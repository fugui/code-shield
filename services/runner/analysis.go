package runner

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"code-shield/models"
	"code-shield/services/dispatcher"
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
	acq, err := router.AcquireTier(ctx.Ctx, "tier1_hunter", "")
	if err != nil {
		return fmt.Errorf("failed to acquire tier1_hunter compute resource: %w", err)
	}
	defer acq.Release()

	backend := acq.Backend
	modelName := acq.ModelName

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

	return aiInv.Invoke(invoker.AIRequest{
		ParentContext:  ctx.Ctx,
		WorkDir:        ctx.CodesPath,
		PromptFile:     absPrompt,
		PromptMsg:      promptMsg,
		InputFiles:     fileList,
		OutputPath:     outputPath,
		TimeoutMin:     timeoutMin,
		ModelName:      modelName,
		ResponseFormat: "json",
		WorkContext: &invoker.LLMWorkContext{
			ReportID: ctx.Report.ID,
			RepoName: ctx.Repo.Name,
			TaskType: ctx.TaskType.DisplayName,
			Stage:    stage,
			SubTask:  subTask,
		},
	})
}

// ExecuteAnalysis 带重试机制执行分析阶段，最多重试 3 次
func ExecuteAnalysis(ctx *TaskContext, fileList []string) ([]models.AnalysisFinding, error) {
	var lastErr error
	maxRetries := 3
	for attempt := 0; attempt <= maxRetries; attempt++ {
		ctx.Attempts = attempt + 1
		if attempt > 0 {
			log.Printf("[Analysis] executeAnalysis failed (attempt %d/%d) for ReportID %d, retrying in %ds: %v\n",
				attempt, maxRetries, ctx.Report.ID, attempt*2, lastErr)
			time.Sleep(time.Duration(attempt*2) * time.Second)

			CleanAnalysisTempFiles(ctx.JsonPath)
		}

		findings, err := ExecuteAnalysisOnce(ctx, fileList)
		if err == nil {
			if len(findings) > 0 {
				ctx.Findings = append(ctx.Findings, findings...)
			}

			chunkInfo := ""
			if ctx.Report.ChunkName != "" {
				chunkInfo = fmt.Sprintf(" [Chunk: %s]", ctx.Report.ChunkName)
			}
			log.Printf("[Analysis] Analysis phase complete: %d findings for ReportID %d%s\n", len(findings), ctx.Report.ID, chunkInfo)
			return findings, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("analysis failed after %d retries: %w", maxRetries, lastErr)
}

// ExecuteAnalysisOnce 执行单次分析尝试：调用 AI、解析 JSON、提取并清洗 findings
func ExecuteAnalysisOnce(ctx *TaskContext, fileList []string) ([]models.AnalysisFinding, error) {
	rawPath := ctx.JsonPath + ".raw"
	if err := ExecuteAI(ctx, fileList, "请以纯 JSON 格式（强调：不要输出 Markdown）输出分析结果", ctx.TaskType.AnalysisPromptFile(), rawPath); err != nil {
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
		finding := models.AnalysisFinding{
			TaskReportID: ctx.Report.ID,
			TaskTypeID:   ctx.TaskType.ID,
			RepoID:       ctx.Repo.ID,
			Severity:     strings.TrimSpace(f.Severity),
			Category:     strings.TrimSpace(f.Category),
			FilePath:     strings.TrimSpace(f.FilePath),
			LineNumber:   ToLineStr(f.LineNumber),
			CodeSnippet:  f.CodeSnippet,
			Title:        governance.SanitizeFindingTitle(f.Title),
			Detail:       f.Detail,
			Suggestion:   f.Suggestion,
		}
		findings = append(findings, finding)
	}

	return findings, nil
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
		finding := models.AnalysisFinding{
			TaskReportID: ctx.Report.ID,
			TaskTypeID:   ctx.TaskType.ID,
			RepoID:       ctx.Repo.ID,
			Severity:     strings.TrimSpace(f.Severity),
			Category:     strings.TrimSpace(f.Category),
			FilePath:     strings.TrimSpace(f.FilePath),
			LineNumber:   ToLineStr(f.LineNumber),
			CodeSnippet:  f.CodeSnippet,
			Title:        governance.SanitizeFindingTitle(f.Title),
			Detail:       f.Detail,
			Suggestion:   f.Suggestion,
		}
		findings = append(findings, finding)
	}

	return findings, nil
}
