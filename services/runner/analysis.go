package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"code-shield/models"
	"code-shield/services/coverage"
	"code-shield/services/dispatcher"
	"code-shield/services/engines"
	"code-shield/services/governance"
	"code-shield/services/invoker"
)

// AnalysisOutput 大模型在静态分析阶段输出的 JSON 结构体
type AnalysisOutput struct {
	Findings []struct {
		Severity    string      `json:"severity"`
		Category    string      `json:"category"`
		FilePath    string      `json:"file_path"`
		LineNumber  interface{} `json:"line_number"`
		CodeSnippet string      `json:"code_snippet"`
		Title       string      `json:"title"`
		Detail      string      `json:"detail"`
		Suggestion  string      `json:"suggestion"`
	} `json:"findings"`
	Summary string `json:"summary"`
}

// ExecuteAI 组装 Prompt 并调用配置的 AI 算力驱动执行
func ExecuteAI(ctx *TaskContext, fileList []string, customPromptSuffix string, promptFilePath string, outputPath string) error {
	UpdateTaskStatus(ctx.Report.ID, models.StatusAnalyzing)

	absPrompt := models.AppConfig.GetAbsPath(promptFilePath)

	promptMsg := fmt.Sprintf("请执行%s任务", ctx.TaskType.DisplayName)
	if customPromptSuffix != "" {
		promptMsg += "：" + customPromptSuffix
	}

	router := dispatcher.GetTierRouter()
	queueStartedAt := time.Now()
	acq, err := router.AcquireTier(ctx.Ctx, "tier1_hunter", "")
	acquiredAt := time.Now()
	invocation := &InvocationObservation{
		StartedAt:    queueStartedAt,
		ArtifactPath: outputPath,
		QueueWaitMs:  acquiredAt.Sub(queueStartedAt).Milliseconds(),
	}
	ctx.LastInvocation = invocation
	if err != nil {
		return fmt.Errorf("failed to acquire tier1_hunter compute resource: %w", err)
	}
	defer acq.Release()

	backend := acq.Backend
	modelName := acq.ModelName
	invocation.Backend = backend
	invocation.ModelName = modelName

	// 源码探索阶段防护：严禁 Thin LLM (native)
	if backend == "" || backend == "native" {
		log.Printf("[Analysis] WARNING: tier1_hunter resolved to non-thick agent %q, fallback to agy\n", backend)
		backend = "agy"
		modelName = ""
	}

	aiInv := GetAIInvoker(backend)
	log.Printf("[Analysis] Invoking AI via %s (Model: %s, ReportID: %d, Output: %s)\n",
		aiInv.Name(), modelName, ctx.Report.ID, outputPath)

	timeoutMin := ctx.TaskType.Timeout
	if timeoutMin <= 0 {
		tierCfg := models.AppConfig.GetTierConfig("tier1_hunter")
		if tierCfg.TimeoutSeconds > 0 {
			timeoutMin = (tierCfg.TimeoutSeconds + 59) / 60
		}
	}

	stage := "Tier 1: 初筛猎手"
	subTask := "单仓全量分析"
	if ctx.Report.ChunkName != "" {
		subTask = fmt.Sprintf("分片分析: %s (%d 个文件)", ctx.Report.ChunkName, len(fileList))
	} else if len(fileList) > 0 {
		subTask = fmt.Sprintf("全量分析 (%d 个文件)", len(fileList))
	}

	metrics := &invoker.InvocationMetrics{}
	invokeErr := aiInv.Invoke(invoker.AIRequest{
		ParentContext:  ctx.Ctx,
		WorkDir:        ctx.CodesPath,
		PromptFile:     absPrompt,
		PromptMsg:      promptMsg,
		InputFiles:     fileList,
		OutputPath:     outputPath,
		TimeoutMin:     timeoutMin,
		ModelName:      modelName,
		ResponseFormat: "json",
		Temperature:    models.AppConfig.DeterministicTemperature(),
		WorkContext: &invoker.LLMWorkContext{
			ReportID: ctx.Report.ID,
			RepoName: ctx.Repo.Name,
			TaskType: ctx.TaskType.DisplayName,
			Stage:    stage,
			SubTask:  subTask,
			TierName: "tier1_hunter",
		},
		Metrics: metrics,
	})
	ctx.DriverFailovers += metrics.DriverFailovers
	invocation.FinishedAt = time.Now()
	invocation.DurationMs = invocation.FinishedAt.Sub(invocation.StartedAt).Milliseconds()
	invocation.Attempts = metrics.DriverAttempts
	invocation.Continuations = metrics.Continuations
	invocation.DriverFailovers = metrics.DriverFailovers
	invocation.PromptHash = hashObservabilityFile(absPrompt)
	invocation.ArtifactHash = hashObservabilityFile(outputPath)
	if invokeErr != nil {
		return invokeErr
	}
	return nil
}

// ExecuteAnalysis 带重试机制执行分析阶段，最多重试 3 次
func ExecuteAnalysis(ctx *TaskContext, fileList []string) ([]models.AnalysisFinding, error) {
	var lastErr error
	var stats analysisAttemptStats
	retryConfig := models.AppConfig.GetAnalysisRetryConfig()
	maxRetries := retryConfig.MaxRetries
	retryableClasses := make(map[invoker.ErrorClass]struct{}, len(retryConfig.RetryableErrors))
	for _, class := range retryConfig.RetryableErrors {
		retryableClasses[invoker.ErrorClass(strings.ToLower(strings.TrimSpace(class)))] = struct{}{}
	}
	for attempt := 0; attempt <= maxRetries; attempt++ {
		ctx.Attempts = attempt + 1

		if attempt > 0 {
			class := invoker.ClassifyError(lastErr)
			if _, ok := retryableClasses[class]; !ok {
				wrappedErr := wrapClassifiedAnalysisError(class, lastErr)
				recordChunkExecution(ctx, fileList, attempt, time.Now(), ctx.LastInvocation, true, wrappedErr)
				return nil, wrappedErr
			}

			backoff := analysisRetryBackoff(
				time.Duration(retryConfig.RetryBackoffMs)*time.Millisecond,
				attempt,
				time.Duration(retryConfig.MaxBackoffSeconds)*time.Second,
			)
			log.Printf("[Analysis] executeAnalysis failed (attempt %d/%d, class=%s) for ReportID %d, retrying in %v: %v\n",
				attempt, maxRetries, class, ctx.Report.ID, backoff, lastErr)

			select {
			case <-ctx.Ctx.Done():
				canceledErr := invoker.WrapClassifiedError(
					invoker.ErrorClassCanceled,
					ctx.Ctx.Err(),
					"analysis retry canceled before backoff completed",
				)
				recordChunkExecution(ctx, fileList, attempt, time.Now(), ctx.LastInvocation, true, canceledErr)
				return nil, canceledErr
			case <-time.After(backoff):
			}

			CleanAnalysisTempFiles(ctx.JsonPath)
		}

		attemptStartedAt := time.Now()
		findings, err := executeAnalysisOnceWithMetrics(ctx, fileList, &stats)
		if err == nil {
			if len(findings) > 0 {
				ctx.Findings = append(ctx.Findings, findings...)
			}

			chunkInfo := ""
			if ctx.Report.ChunkName != "" {
				chunkInfo = fmt.Sprintf(" [Chunk: %s]", ctx.Report.ChunkName)
			}
			log.Printf("[Analysis] Analysis phase complete: %d findings for ReportID %d%s\n", len(findings), ctx.Report.ID, chunkInfo)
			recordChunkExecution(ctx, fileList, attempt+1, attemptStartedAt, ctx.LastInvocation, true, nil)
			return findings, nil
		}
		lastErr = err
		recordChunkExecution(ctx, fileList, attempt+1, attemptStartedAt, ctx.LastInvocation, false, err)
	}
	wrappedErr := wrapClassifiedAnalysisError(
		invoker.ClassifyError(lastErr),
		fmt.Errorf("analysis failed after %d retries: %w", maxRetries, lastErr),
	)
	recordChunkExecution(ctx, fileList, maxRetries+1, time.Now(), ctx.LastInvocation, true, wrappedErr)
	return nil, wrappedErr
}

type analysisAttemptStats struct {
	contractRepairs int
}

func analysisRetryBackoff(base time.Duration, attempt int, maxBackoff time.Duration) time.Duration {
	if base <= 0 {
		base = 2 * time.Second
	}
	if maxBackoff <= 0 {
		maxBackoff = 30 * time.Second
	}
	if attempt <= 0 {
		return base
	}

	// The historical base keeps 2s/4s/8s exponential backoff. Shift is capped
	// to prevent overflow after many configured retries.
	shift := attempt - 1
	if shift > 5 {
		shift = 5
	}
	backoff := base * (1 << uint(shift))
	if backoff > maxBackoff {
		backoff = maxBackoff
	}
	return backoff
}

func wrapClassifiedAnalysisError(class invoker.ErrorClass, err error) error {
	if err == nil {
		return nil
	}
	if class == invoker.ErrorClassNone || class == "" {
		class = invoker.ErrorClassUnknown
	}
	return invoker.WrapClassifiedError(class, err, "analysis failed")
}

// ExecuteChunkAnalysis runs one chunk with an isolated TaskContext. The caller
// must supply a unique output path; sibling chunks must never share raw output
// artifacts or mutable attempt counters.
func ExecuteChunkAnalysis(ctx *TaskContext, req engines.ChunkExecutionRequest) (engines.ChunkExecutionResult, error) {
	chunkCtx := *ctx
	chunkCtx.Report = ctx.Report
	chunkCtx.Report.ChunkName = req.Name
	chunkCtx.Findings = nil
	chunkCtx.Attempts = 0
	chunkCtx.ChunkUID = coverage.StableChunkID(chunkPolicyID(ctx), req.Files)

	if req.OutputPath != "" {
		if dir := filepath.Dir(req.OutputPath); dir != "" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return engines.ChunkExecutionResult{}, fmt.Errorf("create chunk output dir: %w", err)
			}
		}
		chunkCtx.JsonPath = req.OutputPath
	}

	findings, err := ExecuteAnalysis(&chunkCtx, req.Files)
	class := invoker.ErrorClassNone
	if err != nil {
		class = invoker.ClassifyError(err)
	}
	result := engines.ChunkExecutionResult{
		Findings:        findings,
		Attempts:        chunkCtx.Attempts,
		Retries:         maxInt(chunkCtx.Attempts-1, 0),
		ContractRepairs: chunkCtx.ContractRepairs,
		DriverFailovers: chunkCtx.DriverFailovers,
		OutputPath:      chunkCtx.JsonPath,
		ErrorClass:      string(class),
		ChunkUID:        chunkCtx.ChunkUID,
		PrimaryUnitID:   primaryUnitID(req.Files),
	}
	if result.Attempts < 1 {
		result.Attempts = 1
	}
	return result, err
}

func chunkPolicyID(ctx *TaskContext) string {
	if ctx.ChunkPolicyID != "" {
		return ctx.ChunkPolicyID
	}
	return coverage.ChunkPolicyID(ctx.TaskType.Name, ctx.TaskType.EngineConfig)
}

func primaryUnitID(files []string) string {
	if len(files) != 1 || strings.TrimSpace(files[0]) == "" {
		return ""
	}
	digest := sha256.Sum256([]byte("primary-unit|" + files[0]))
	return hex.EncodeToString(digest[:])[:16]
}

func hashObservabilityFile(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

// ExecuteAnalysisOnce 执行单次分析尝试：调用 AI、解析 JSON、提取并清洗 findings
func ExecuteAnalysisOnce(ctx *TaskContext, fileList []string) ([]models.AnalysisFinding, error) {
	return executeAnalysisOnceWithMetrics(ctx, fileList, nil)
}

func executeAnalysisOnceWithMetrics(ctx *TaskContext, fileList []string, stats *analysisAttemptStats) ([]models.AnalysisFinding, error) {
	rawPath := ctx.JsonPath + ".raw"
	if err := ExecuteAI(ctx, fileList, "请以纯 JSON 格式（强调：不要输出 Markdown）输出分析结果", ctx.promptFile(), rawPath); err != nil {
		return nil, err
	}

	RecoverAIOutput(rawPath)

	rawJSON, err := os.ReadFile(rawPath)
	if err != nil {
		stdoutPath := rawPath + ".output.txt"
		if _, statErr := os.Stat(stdoutPath); statErr == nil {
			log.Printf("[Analysis] Fallback: rawPath (%s) not found, reading from AI stdout log: %s\n", rawPath, stdoutPath)
			rawJSON, err = os.ReadFile(stdoutPath)
			if err == nil {
				_ = os.WriteFile(rawPath, rawJSON, 0644)
			}
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read analysis output: %w", err)
		}
	}

	cleanedJSON := CleanJSONFromAI(rawJSON)

	var output AnalysisOutput
	if err := json.Unmarshal(cleanedJSON, &output); err != nil {
		if stats != nil {
			stats.contractRepairs++
		}
		if ctx != nil {
			ctx.ContractRepairs++
		}
		log.Printf("[Error] Failed to parse analysis JSON: %v, attempting AI repair\n", err)
		repairedJSON, repairErr := RepairJSON(ctx.CodesPath, rawPath, "")
		if repairErr != nil {
			log.Printf("[Error] AI JSON repair failed: %v\n", repairErr)
			return nil, fmt.Errorf("AI JSON repair failed: %w", repairErr)
		}
		if err := json.Unmarshal(repairedJSON, &output); err != nil {
			log.Printf("[Error] Repaired JSON still invalid: %v\n", err)
			return nil, fmt.Errorf("repaired JSON still invalid: %w", err)
		}
		log.Println("[Analysis] AI JSON repair successful")
		_ = os.WriteFile(rawPath, repairedJSON, 0644)
	}

	var findings []models.AnalysisFinding
	for _, f := range output.Findings {
		normalizedCategory := governance.NormalizeCategory(f.Category, ctx.TaskType.GetAllowedCategories())
		finding := models.AnalysisFinding{
			TaskReportID:     ctx.Report.ID,
			TaskTypeID:       ctx.TaskType.ID,
			RepoID:           ctx.Repo.ID,
			Severity:         strings.TrimSpace(f.Severity),
			Category:         normalizedCategory.Category,
			CategoryDetail:   normalizedCategory.Detail,
			TaxonomyGoverned: normalizedCategory.Governed,
			FilePath:         strings.TrimSpace(f.FilePath),
			LineNumber:       ToLineStr(f.LineNumber),
			CodeSnippet:      f.CodeSnippet,
			Title:            governance.SanitizeFindingTitle(f.Title),
			Detail:           f.Detail,
			Suggestion:       f.Suggestion,
		}
		findings = append(findings, finding)
	}

	return findings, nil
}

func (ctx *TaskContext) promptFile() string {
	if ctx.PromptPath != "" {
		return ctx.PromptPath
	}
	return ctx.TaskType.AnalysisPromptFile()
}

// LoadFindingsFromChunkFile 从指定的分片 JSON 结果中读取并解析 findings
func LoadFindingsFromChunkFile(ctx *TaskContext, jsonPath string) ([]models.AnalysisFinding, error) {
	rawPath := jsonPath + ".raw"
	rawJSON, err := os.ReadFile(rawPath)
	if err != nil {
		return nil, err
	}

	cleanedJSON := CleanJSONFromAI(rawJSON)

	var output AnalysisOutput
	if err := json.Unmarshal(cleanedJSON, &output); err != nil {
		return nil, err
	}

	var findings []models.AnalysisFinding
	for _, f := range output.Findings {
		normalizedCategory := governance.NormalizeCategory(f.Category, ctx.TaskType.GetAllowedCategories())
		finding := models.AnalysisFinding{
			TaskReportID:     ctx.Report.ID,
			TaskTypeID:       ctx.TaskType.ID,
			RepoID:           ctx.Repo.ID,
			Severity:         strings.TrimSpace(f.Severity),
			Category:         normalizedCategory.Category,
			CategoryDetail:   normalizedCategory.Detail,
			TaxonomyGoverned: normalizedCategory.Governed,
			FilePath:         strings.TrimSpace(f.FilePath),
			LineNumber:       ToLineStr(f.LineNumber),
			CodeSnippet:      f.CodeSnippet,
			Title:            governance.SanitizeFindingTitle(f.Title),
			Detail:           f.Detail,
			Suggestion:       f.Suggestion,
		}
		findings = append(findings, finding)
	}

	return findings, nil
}
