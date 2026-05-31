package services

import (
	"bytes"
	"code-shield/models"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// TaskResult is the standardized output from a postprocess script
type TaskResult struct {
	Score   int            `json:"score"`
	Summary string         `json:"summary"`
	Metrics map[string]int `json:"metrics"`
}

// ChunkDetails records execution details of a single chunk (or single repo run).
type ChunkDetails struct {
	ChunkName       string    `json:"chunk_name"`
	Files           []string  `json:"files,omitempty"`
	Status          string    `json:"status"` // "success" or "failed"
	Attempts        int       `json:"attempts"`
	Retries         int       `json:"retries"`
	StartTime       time.Time `json:"start_time"`
	EndTime         time.Time `json:"end_time"`
	DurationSeconds float64   `json:"duration_seconds"`
	ErrorMessage    string    `json:"error_message,omitempty"`
}

// AnalysisSummary holds metrics and details for the Analysis Phase.
type AnalysisSummary struct {
	Status          string         `json:"status"` // "success" or "failed"
	StartTime       time.Time      `json:"start_time"`
	EndTime         time.Time      `json:"end_time"`
	DurationSeconds float64        `json:"duration_seconds"`
	TotalChunks     int            `json:"total_chunks"`
	SuccessChunks   int            `json:"success_chunks"`
	FailedChunks    int            `json:"failed_chunks"`
	TotalFindings   int            `json:"total_findings"`
	Chunks          []ChunkDetails `json:"chunks"`
}

// SynthesisSummary holds metrics and details for the Synthesis Phase.
type SynthesisSummary struct {
	Status          string    `json:"status"` // "success" or "failed"
	Attempts        int       `json:"attempts"`
	StartTime       time.Time `json:"start_time"`
	EndTime         time.Time `json:"end_time"`
	DurationSeconds float64   `json:"duration_seconds"`
	ErrorMessage    string    `json:"error_message,omitempty"`
}

// TaskSummaryReport defines the unified execution summary for the entire task.
type TaskSummaryReport struct {
	TaskID          uint             `json:"task_id"`
	RepoName        string           `json:"repo_name"`
	TaskType        string           `json:"task_type"`
	EngineMode      string           `json:"engine_mode"`
	Status          string           `json:"status"` // "success" or "failed"
	StartTime       time.Time        `json:"start_time"`
	EndTime         time.Time        `json:"end_time"`
	DurationSeconds float64          `json:"duration_seconds"`
	Analysis        AnalysisSummary  `json:"analysis"`
	Synthesis       SynthesisSummary `json:"synthesis"`
}

// taskContext holds all necessary data for a single task execution run
type taskContext struct {
	ctx        context.Context
	cancel     context.CancelFunc
	report     models.TaskReport
	taskType   models.TaskType
	repo       models.Repository
	codesPath  string
	reportPath string
	jsonPath   string
	autoNotify bool
	runParams  models.RunParams // 合并后的运行参数
	Attempts   int              // 实际尝试次数
	Summary    TaskSummaryReport
	findings   []models.AnalysisFinding
}

var (
	activeTasksMu sync.Mutex
	activeTasks   = make(map[uint]*taskContext) // reportID -> taskContext
)

func CancelRunningTask(reportID uint) bool {
	activeTasksMu.Lock()
	defer activeTasksMu.Unlock()
	if ctx, ok := activeTasks[reportID]; ok {
		log.Printf("[TaskRunner] Cancelling active task for ReportID %d\n", reportID)
		ctx.cancel()
		return true
	}
	return false
}

// CancelAllRunningTasks cancels all running tasks currently registered in activeTasks
func CancelAllRunningTasks() {
	activeTasksMu.Lock()
	defer activeTasksMu.Unlock()
	log.Printf("[TaskRunner] Cancelling all %d active tasks\n", len(activeTasks))
	for id, ctx := range activeTasks {
		log.Printf("[TaskRunner] Cancelling task for ReportID %d\n", id)
		ctx.cancel()
	}
}

// resolveRunParams 将外部传入的 RunParams 与 TaskType 默认值合并。
// 优先级：外部 RunParams > TaskType 默认值 > 全局配置
func (ctx *taskContext) resolveRunParams(input models.RunParams) {
	ctx.runParams = input
	if ctx.runParams.AIBackend == nil && ctx.taskType.AIBackend != "" {
		ctx.runParams.AIBackend = &ctx.taskType.AIBackend
	}
	if ctx.runParams.TargetScope == nil {
		ctx.runParams.TargetScope = &ctx.taskType.TargetScope
	}
}

func RunTaskSync(reportID uint, repoURL string, taskTypeID uint, autoNotify bool, runParams models.RunParams) error {
	ctx := &taskContext{autoNotify: autoNotify}

	// 1. Initialize and load data
	if err := ctx.load(reportID, taskTypeID); err != nil {
		return err
	}

	ctx.Summary = TaskSummaryReport{
		TaskID:     reportID,
		RepoName:   ctx.repo.Name,
		TaskType:   ctx.taskType.Name,
		EngineMode: ctx.taskType.EngineMode,
		StartTime:  time.Now(),
	}

	taskCtx, cancel := context.WithCancel(context.Background())
	ctx.ctx = taskCtx
	ctx.cancel = cancel

	activeTasksMu.Lock()
	activeTasks[reportID] = ctx
	activeTasksMu.Unlock()
	defer func() {
		activeTasksMu.Lock()
		delete(activeTasks, reportID)
		activeTasksMu.Unlock()
		cancel()
	}()

	// Resolve run params: input overrides → TaskType defaults → global config
	ctx.resolveRunParams(runParams)

	// Prepare output paths for final report early to ensure output.txt is available for all failure logging
	ctx.prepareOutputPaths()

	log.Printf("[TaskRunner] Starting task for ReportID: %d, URL: %s, TaskType: %s (Mode: %s)\n", 
		ctx.report.ID, repoURL, ctx.taskType.Name, ctx.taskType.EngineMode)

	// 2. Prepare workspace and sync code
	if err := ctx.prepareAndSync(repoURL); err != nil {
		ctx.markFailed(err.Error())
		return err
	}

	// 3. Run Precondition check
	if skipped, err := ctx.checkPrecondition(); err != nil {
		ctx.markFailed(err.Error())
		return err
	} else if skipped {
		return nil
	}

	// 5. Dispatch to Engine
	engine := GetEngine(ctx.taskType.EngineMode)
	if err := engine.Run(ctx); err != nil {
		ctx.markFailed(err.Error())
		return err
	}

	// 6. Post-process: run postprocess script on final report
	result := ctx.runPostProcess()

	// 7. Finalize: save result to DB and trigger notification
	return ctx.finalize(result)
}




// load fetches necessary models from the database
func (ctx *taskContext) load(reportID, taskTypeID uint) error {
	if err := models.DB.Preload("Repo").First(&ctx.report, reportID).Error; err != nil {
		return fmt.Errorf("report %d not found", reportID)
	}
	if err := models.DB.First(&ctx.taskType, taskTypeID).Error; err != nil {
		return fmt.Errorf("task type %d not found", taskTypeID)
	}
	ctx.repo = ctx.report.Repo
	return nil
}

// prepareAndSync handles URL parsing and git operations
func (ctx *taskContext) prepareAndSync(repoURL string) error {
	u, err := url.Parse(repoURL)
	if err != nil {
		return fmt.Errorf("invalid repository URL: %w", err)
	}

	rawPath := strings.TrimSuffix(strings.TrimPrefix(u.Path, "/"), ".git")
	ctx.codesPath = filepath.Join(models.AppConfig.Storage.Root, "codes", rawPath)

	if err := os.MkdirAll(filepath.Dir(ctx.codesPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	updateTaskStatus(ctx.report.ID, models.StatusCloning)
	
	var cmd *exec.Cmd
	if stat, err := os.Stat(filepath.Join(ctx.codesPath, ".git")); err == nil && stat.IsDir() {
		log.Printf("[TaskRunner] Running git pull in %s\n", ctx.codesPath)
		cmd = exec.CommandContext(ctx.ctx, "git", "-C", ctx.codesPath, "pull")
	} else {
		log.Printf("[TaskRunner] Running git clone %s %s\n", repoURL, ctx.codesPath)
		cmd = exec.CommandContext(ctx.ctx, "git", "clone", repoURL, ctx.codesPath)
	}

	if output, err := cmd.CombinedOutput(); err != nil {
		models.DB.Model(&models.TaskReport{}).Where("id = ?", ctx.report.ID).Update("clone_status", "failed")
		return fmt.Errorf("git operation failed: %s", string(output))
	}

	models.DB.Model(&models.TaskReport{}).Where("id = ?", ctx.report.ID).Update("clone_status", "success")
	return nil
}

// checkPrecondition executes the task-specific script to decide whether to proceed
func (ctx *taskContext) checkPrecondition() (bool, error) {
	absScript := models.AppConfig.GetAbsPath(ctx.taskType.PreconditionScript())
	if _, err := os.Stat(absScript); os.IsNotExist(err) {
		return false, nil
	}
	
	log.Printf("[TaskRunner] Running precondition: %s\n", absScript)

	// Ensure the script is executable
	os.Chmod(absScript, 0755)

	cmd := exec.CommandContext(ctx.ctx, absScript, ctx.codesPath)
	output, err := cmd.CombinedOutput()
	outputStr := strings.TrimSpace(string(output))

	if err != nil {
		if cmd.ProcessState.ExitCode() == 1 {
			log.Printf("[TaskRunner] Precondition skip: %s\n", outputStr)
			models.DB.Model(&models.TaskReport{}).Where("id = ?", ctx.report.ID).Updates(map[string]interface{}{
				"status":     models.StatusSkipped,
				"ai_summary": outputStr,
				"created_at": time.Now(),
			})
			return true, nil
		}
		return false, fmt.Errorf("precondition failed: %s", outputStr)
	}
	return false, nil
}

// prepareOutputPaths creates the output directory and sets reportPath/jsonPath on the main report context.
// Note: ChunkedEngine constructs paths for chunk sub-reports independently.
func (ctx *taskContext) prepareOutputPaths() {
	if ctx.report.ReportPath != "" {
		reportsDir := filepath.Dir(ctx.report.ReportPath)
		os.MkdirAll(reportsDir, 0755)
		ctx.reportPath = ctx.report.ReportPath
		safeRepoName := strings.ReplaceAll(ctx.repo.Name, "/", "-")
		ctx.jsonPath = filepath.Join(reportsDir, fmt.Sprintf("report-%d-summary-%s.json", ctx.report.ID, safeRepoName))
		return
	}

	currentDate := time.Now().Format("2006-01-02")
	if !ctx.report.CreatedAt.IsZero() {
		currentDate = ctx.report.CreatedAt.Format("2006-01-02")
	}
	reportsDir := filepath.Join(models.AppConfig.Storage.Root, "reports", ctx.taskType.Name, currentDate)
	os.MkdirAll(reportsDir, 0755)

	safeRepoName := strings.ReplaceAll(ctx.repo.Name, "/", "-")
	ctx.reportPath = filepath.Join(reportsDir, fmt.Sprintf("report-%d-report-%s.md", ctx.report.ID, safeRepoName))
	ctx.jsonPath = filepath.Join(reportsDir, fmt.Sprintf("report-%d-summary-%s.json", ctx.report.ID, safeRepoName))
}

// executeAI constructs the prompt and delegates to the configured AI CLI backend.
// outputPath controls where AI writes its content (reportPath for Markdown, jsonPath for JSON analysis).
func (ctx *taskContext) executeAI(fileList []string, customPromptSuffix string, promptFilePath string, outputPath string) error {
	updateTaskStatus(ctx.report.ID, models.StatusAnalyzing)

	absPrompt := models.AppConfig.GetAbsPath(promptFilePath)

	// 构造 prompt 消息
	promptMsg := fmt.Sprintf("请执行%s任务", ctx.taskType.DisplayName)
	if customPromptSuffix != "" {
		promptMsg += "：" + customPromptSuffix
	}

	// 根据配置选择 AI CLI 后端（RunParams > TaskType > 全局配置）
	backend := models.AppConfig.AI.Backend
	if ctx.runParams.AIBackend != nil && *ctx.runParams.AIBackend != "" {
		backend = *ctx.runParams.AIBackend
	}
	invoker := GetAIInvoker(backend)
	log.Printf("[TaskRunner] Invoking AI via %s (ReportID: %d, Output: %s)\n", invoker.Name(), ctx.report.ID, outputPath)

	return invoker.Invoke(AIRequest{
		ParentContext: ctx.ctx,
		WorkDir:       ctx.codesPath,
		PromptFile:    absPrompt,
		PromptMsg:     promptMsg,
		InputFiles:    fileList,
		OutputPath:    outputPath,
		TimeoutMin:    ctx.taskType.Timeout,
	})
}

// AnalysisOutput represents the JSON structure output by AI during the analysis phase
type AnalysisOutput struct {
	Findings []struct {
		Severity    string `json:"severity"`
		Category    string `json:"category"`
		FilePath    string `json:"file_path"`
		LineNumber  interface{} `json:"line_number"`
		CodeSnippet string `json:"code_snippet"`
		Title       string `json:"title"`
		Detail      string `json:"detail"`
		Suggestion  string `json:"suggestion"`
	} `json:"findings"`
	Summary string `json:"summary"`
}

// toLineStr 将 AI 输出的 line_number（可能是数字或字符串）统一转为 string
func toLineStr(v interface{}) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case float64: // JSON 数字默认解码为 float64
		return fmt.Sprintf("%d", int(val))
	default:
		return fmt.Sprintf("%v", val)
	}
}

// cleanJSONFromAI strips markdown code block markers and extracts JSON from AI output.
// AI models frequently wrap JSON in ```json ... ``` markers despite being told not to.
func cleanJSONFromAI(raw []byte) []byte {
	s := strings.TrimSpace(string(raw))

	// Strip leading ```json or ``` and trailing ```
	if strings.HasPrefix(s, "```") {
		// Remove the opening marker line
		if idx := strings.Index(s, "\n"); idx != -1 {
			s = s[idx+1:]
		}
		// Remove the trailing ```
		if strings.HasSuffix(s, "```") {
			s = s[:len(s)-3]
		}
		s = strings.TrimSpace(s)
	}

	// Try to find a JSON object if there's surrounding text
	if !strings.HasPrefix(s, "{") {
		if start := strings.Index(s, "{"); start != -1 {
			// Find the matching closing brace from the end
			if end := strings.LastIndex(s, "}"); end > start {
				s = s[start : end+1]
			}
		}
	}

	// Fix unescaped quotes inside JSON string values
	s = fixUnescapedQuotes(s)

	return []byte(s)
}

// fixUnescapedQuotes 修复 AI 生成的 JSON 中字符串值内未转义的引号。
// 例如: {"title": "Use "proper" method"} → {"title": "Use \"proper\" method"}
// 使用状态机逐字符扫描，判断引号是结构性分隔符还是值内容。
func fixUnescapedQuotes(s string) string {
	// 先尝试标准解析，如果成功则无需修复
	if json.Valid([]byte(s)) {
		return s
	}

	var buf strings.Builder
	buf.Grow(len(s) + 64)

	inString := false // 是否在字符串值内部
	i := 0

	for i < len(s) {
		ch := s[i]

		// 处理已转义的字符（\x），直接保留
		if inString && ch == '\\' && i+1 < len(s) {
			buf.WriteByte(ch)
			buf.WriteByte(s[i+1])
			i += 2
			continue
		}

		if ch == '"' {
			if !inString {
				// 进入字符串
				inString = true
				buf.WriteByte(ch)
				i++
				continue
			}

			// 当前在字符串内部，遇到引号 → 判断这是闭合引号还是未转义的内部引号
			// 向后跳过空白字符，检查下一个非空字符是否为 JSON 结构字符
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
				j++
			}

			isStructural := false
			if j >= len(s) {
				// 已到末尾，视为闭合引号
				isStructural = true
			} else {
				next := s[j]
				// 闭合引号后面应该紧跟 JSON 结构字符: , : } ]
				// 或者紧跟换行后的结构字符
				isStructural = next == ',' || next == ':' || next == '}' || next == ']'
			}

			if isStructural {
				// 这是闭合引号，结束字符串
				inString = false
				buf.WriteByte(ch)
			} else {
				// 这是未转义的内部引号，补上反斜杠
				buf.WriteByte('\\')
				buf.WriteByte(ch)
			}
			i++
			continue
		}

		buf.WriteByte(ch)
		i++
	}

	return buf.String()
}

// executeAnalysis runs the analysis phase: AI outputs structured JSON findings, retrying up to 3 times on failure.
func (ctx *taskContext) executeAnalysis(fileList []string) ([]models.AnalysisFinding, error) {
	var lastErr error
	maxRetries := 3
	for attempt := 0; attempt <= maxRetries; attempt++ {
		ctx.Attempts = attempt + 1
		if attempt > 0 {
			log.Printf("[TaskRunner] executeAnalysis failed (attempt %d/%d) for ReportID %d, retrying in %ds: %v\n",
				attempt, maxRetries, ctx.report.ID, attempt*2, lastErr)
			time.Sleep(time.Duration(attempt*2) * time.Second)

			// 重试前清理上次遗留的临时文件，避免脏数据累积
			cleanAnalysisTempFiles(ctx.jsonPath)
		}

		findings, err := ctx.executeAnalysisOnce(fileList)
		if err == nil {
			if len(findings) > 0 {
				ctx.findings = append(ctx.findings, findings...)
			}

			chunkInfo := ""
			if ctx.report.ChunkName != "" {
				chunkInfo = fmt.Sprintf(" [Chunk: %s]", ctx.report.ChunkName)
			}
			log.Printf("[TaskRunner] Analysis phase complete: %d findings for ReportID %d%s\n", len(findings), ctx.report.ID, chunkInfo)
			return findings, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("analysis failed after %d retries: %w", maxRetries, lastErr)
}

// cleanAnalysisTempFiles 清理一次 analysis attempt 产生的所有临时文件和 AI 误创建的目录。
// jsonPath 是主 JSON 输出路径，据此推导出关联的辅助文件。
func cleanAnalysisTempFiles(jsonPath string) {
	// AI CLI 输出日志
	os.Remove(jsonPath + ".output.txt")
	// AI 调试日志
	os.Remove(jsonPath + ".debug.log")
	// RepairJSON 产生的修复中间文件
	ext := filepath.Ext(jsonPath)
	basePath := strings.TrimSuffix(jsonPath, ext)
	os.Remove(basePath + ".fixed" + ext)
	// 上次的 JSON 输出本身（避免新 attempt 读到旧数据）
	os.Remove(jsonPath)
	os.Remove(jsonPath + ".raw")
	// AI 工具有时会创建同名目录（如 chunk-fle-4/ 代替 chunk-fle-4.json），一并清除
	os.RemoveAll(basePath)
}

// recoverAIOutput 处理 AI 工具（opencode/claude）将输出文件写入同名目录而非直接创建文件的情况。
// 例如：AI 被要求写入 chunk-fle-4.json，但实际创建了 chunk-fle-4/chunk-fle-4.json。
// 此函数检测这种情况，将文件移动 to 正确位置，并清理 AI 创建的垃圾目录。
func recoverAIOutput(expectedPath string) {
	// 如果预期文件已经存在，无需恢复
	if info, err := os.Stat(expectedPath); err == nil && !info.IsDir() {
		return
	}

	// 检查是否存在同名目录（去掉扩展名）
	ext := filepath.Ext(expectedPath)
	baseName := filepath.Base(strings.TrimSuffix(expectedPath, ext))
	dirPath := strings.TrimSuffix(expectedPath, ext)

	info, err := os.Stat(dirPath)
	if err != nil || !info.IsDir() {
		return
	}

	// 在目录中查找同名 JSON 文件
	candidatePath := filepath.Join(dirPath, baseName+ext)
	if _, err := os.Stat(candidatePath); err == nil {
		log.Printf("[TaskRunner] Recovering AI output: moving %s → %s\n", candidatePath, expectedPath)
		// 读取内容并写到正确位置（避免跨设备 rename 失败）
		if content, readErr := os.ReadFile(candidatePath); readErr == nil {
			if writeErr := os.WriteFile(expectedPath, content, 0644); writeErr == nil {
				os.RemoveAll(dirPath)
				return
			}
		}
	}

	// 候选位置不存在时，搜索目录内的第一个 JSON 文件作为兜底
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ext {
			fallbackPath := filepath.Join(dirPath, entry.Name())
			log.Printf("[TaskRunner] Recovering AI output (fallback): moving %s → %s\n", fallbackPath, expectedPath)
			if content, readErr := os.ReadFile(fallbackPath); readErr == nil {
				if writeErr := os.WriteFile(expectedPath, content, 0644); writeErr == nil {
					os.RemoveAll(dirPath)
					return
				}
			}
			break
		}
	}

	// 目录内没有可用文件（空目录），直接清除
	log.Printf("[TaskRunner] Cleaning up empty AI-created directory: %s\n", dirPath)
	os.RemoveAll(dirPath)
}

// executeAnalysisOnce runs a single attempt of the analysis phase.
// 注意：此函数只负责调用 AI、解析 JSON 并返回 findings 结构体，不写入数据库。
// 数据库持久化由调用方 executeAnalysis 在确认最终成功后统一执行。
func (ctx *taskContext) executeAnalysisOnce(fileList []string) ([]models.AnalysisFinding, error) {
	rawPath := ctx.jsonPath + ".raw"
	if err := ctx.executeAI(fileList, "请以纯 JSON 格式（强调：不要输出 Markdown）输出分析结果", ctx.taskType.AnalysisPromptFile(), rawPath); err != nil {
		return nil, err
	}

	// AI 工具有时会将输出写入同名目录而非文件（如 chunk-fle-4/ 代替 chunk-fle-4.json），
	// 在此自动检测并恢复到正确路径。
	recoverAIOutput(rawPath)

	// Parse the AI JSON output from the report file
	rawJSON, err := os.ReadFile(rawPath)
	if err != nil {
		// Fallback: If rawPath does not exist, try reading from AI stdout log (.output.txt)
		stdoutPath := rawPath + ".output.txt"
		if _, statErr := os.Stat(stdoutPath); statErr == nil {
			log.Printf("[TaskRunner] Fallback: rawPath (%s) not found, reading from AI stdout log: %s\n", rawPath, stdoutPath)
			rawJSON, err = os.ReadFile(stdoutPath)
			if err == nil {
				// Write the raw output to rawPath for consistency and downstream repair use
				os.WriteFile(rawPath, rawJSON, 0644)
			}
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read analysis output: %w", err)
		}
	}

	// Clean the AI output: strip markdown code block markers (```json ... ```)
	cleanedJSON := cleanJSONFromAI(rawJSON)

	var output AnalysisOutput
	if err := json.Unmarshal(cleanedJSON, &output); err != nil {
		log.Printf("[Error] Failed to parse analysis JSON: %v, attempting AI repair\n", err)
		log.Printf("[Error] Raw output (first 500 chars): %s\n", string(cleanedJSON[:min(len(cleanedJSON), 500)]))

		// Attempt AI-powered JSON repair
		backend := ""
		if ctx.runParams.AIBackend != nil {
			backend = *ctx.runParams.AIBackend
		}
		repairedJSON, repairErr := RepairJSON(ctx.codesPath, rawPath, backend)
		if repairErr != nil {
			log.Printf("[Error] AI JSON repair failed: %v\n", repairErr)
			return nil, fmt.Errorf("AI JSON repair failed: %w", repairErr)
		}
		if err := json.Unmarshal(repairedJSON, &output); err != nil {
			log.Printf("[Error] Repaired JSON still invalid: %v\n", err)
			return nil, fmt.Errorf("repaired JSON still invalid: %w", err)
		}
		log.Println("[TaskRunner] AI JSON repair successful")
		// Overwrite the raw file with the repaired version for downstream use
		os.WriteFile(rawPath, repairedJSON, 0644)
	}

	// Convert to model objects (不写入数据库，由调用方统一处理)
	var findings []models.AnalysisFinding
	for _, f := range output.Findings {
		finding := models.AnalysisFinding{
			TaskReportID: ctx.report.ID,
			TaskTypeID:   ctx.taskType.ID,
			RepoID:       ctx.repo.ID,
			Severity:     f.Severity,
			Category:     f.Category,
			FilePath:     f.FilePath,
			LineNumber:   toLineStr(f.LineNumber),
			CodeSnippet:  f.CodeSnippet,
			Title:        f.Title,
			Detail:       f.Detail,
			Suggestion:   f.Suggestion,
		}
		findings = append(findings, finding)
	}

	return findings, nil
}

// loadFindingsFromChunkFile reads a chunk's raw JSON file and parses it into AnalysisFinding objects.
func (ctx *taskContext) loadFindingsFromChunkFile(jsonPath string) ([]models.AnalysisFinding, error) {
	rawPath := jsonPath + ".raw"
	rawJSON, err := os.ReadFile(rawPath)
	if err != nil {
		return nil, err
	}

	cleanedJSON := cleanJSONFromAI(rawJSON)

	var output AnalysisOutput
	if err := json.Unmarshal(cleanedJSON, &output); err != nil {
		return nil, err
	}

	var findings []models.AnalysisFinding
	for _, f := range output.Findings {
		finding := models.AnalysisFinding{
			TaskReportID: ctx.report.ID,
			TaskTypeID:   ctx.taskType.ID,
			RepoID:       ctx.repo.ID,
			Severity:     f.Severity,
			Category:     f.Category,
			FilePath:     f.FilePath,
			LineNumber:   toLineStr(f.LineNumber),
			CodeSnippet:  f.CodeSnippet,
			Title:        f.Title,
			Detail:       f.Detail,
			Suggestion:   f.Suggestion,
		}
		findings = append(findings, finding)
	}

	return findings, nil
}


// executeSynthesis runs the synthesis phase: AI generates final Markdown report from JSON findings, retrying up to 3 times on failure.
func (ctx *taskContext) executeSynthesis(allFindings []models.AnalysisFinding) error {
	// 1. Serialize all findings (complete list) to synthesisInputPath for Outlook attachment and database records.
	findingsJSON, _ := json.MarshalIndent(allFindings, "", "  ")
	safeRepoName := strings.ReplaceAll(ctx.repo.Name, "/", "-")
	synthesisInputPath := filepath.Join(filepath.Dir(ctx.reportPath), fmt.Sprintf("report-%d-synthesis-%s.json", ctx.report.ID, safeRepoName))
	if err := os.WriteFile(synthesisInputPath, findingsJSON, 0644); err != nil {
		return fmt.Errorf("failed to write synthesis input: %w", err)
	}

	// 2. Count all findings by severity for 100% accurate prompt injection
	counts := map[string]int{
		"阻塞":        0,
		"blocking":  0,
		"严重":        0,
		"critical":  0,
		"主要":        0,
		"major":     0,
		"提示":        0,
		"info":      0,
		"建议":        0,
		"suggestion": 0,
		"合格":        0,
		"pass":      0,
		"高风险":       0,
		"high":      0,
		"high_risk": 0,
		"中风险":       0,
		"medium":    0,
		"medium_risk": 0,
		"低风险":       0,
		"low":       0,
		"low_risk":  0,
	}
	for _, f := range allFindings {
		counts[strings.ToLower(f.Severity)]++
	}

	blockingCount := counts["阻塞"] + counts["blocking"]
	criticalCount := counts["严重"] + counts["critical"]
	majorCount := counts["主要"] + counts["major"]
	infoCount := counts["提示"] + counts["info"]
	suggestionCount := counts["建议"] + counts["suggestion"]

	highCount := counts["高风险"] + counts["high"] + counts["high_risk"]
	mediumCount := counts["中风险"] + counts["medium"] + counts["medium_risk"]
	lowCount := counts["低风险"] + counts["low"] + counts["low_risk"]

	// Inject 100% accurate pre-calculated severity summary to the AI prompt
	suffixPrompt := ""
	isCodeReview := strings.Contains(strings.ToLower(ctx.taskType.Name), "review")
	if isCodeReview {
		suffixPrompt = fmt.Sprintf("【重要硬性指标约束（必须严格遵守）】：为了确保报告的统计数据100%%精确，请不要根据输入的 JSON 数量进行统计，而**必须**将以下精确的统计结果原封不动地输出在报告的『一、检视结果概要』章节中：\n```\n## 检视结果概要\n\n阻塞：%d，严重：%d，主要：%d，提示：%d，建议：%d\n```",
			blockingCount, criticalCount, majorCount, infoCount, suggestionCount)
	} else {
		suffixPrompt = fmt.Sprintf("【重要硬性指标约束（必须严格遵守）】：为了确保报告的统计数据100%%精确，请不要根据输入的 JSON 数量进行统计，而**必须**将以下精确的统计结果原封不动地输出在报告的『一、检视结果概要』章节中：\n```\n## 检视结果概要\n\n高风险：%d，中风险：%d，低风险：%d\n```",
			highCount, mediumCount, lowCount)
	}

	// 3. Build a simplified list of findings for the AI if count exceeds the threshold to avoid context and output limit issues.
	var aiFindings []models.AnalysisFinding

	// Weight mapping for sorting severities (higher weight = higher priority)
	severityWeight := map[string]int{
		"阻塞":        5,
		"blocking":  5,
		"严重":        4,
		"critical":  4,
		"主要":        3,
		"major":     3,
		"提示":        2,
		"info":      2,
		"建议":        1,
		"suggestion": 1,
		"合格":        0,
		"pass":      0,
		"高风险":       5,
		"high":      5,
		"high_risk": 5,
		"中风险":       3,
		"medium":    3,
		"medium_risk": 3,
		"low":       1,
		"低风险":       1,
		"low_risk":  1,
	}

	// Sort findings by severity weight descending
	sortedFindings := make([]models.AnalysisFinding, len(allFindings))
	copy(sortedFindings, allFindings)
	sort.Slice(sortedFindings, func(i, j int) bool {
		wI := severityWeight[strings.ToLower(sortedFindings[i].Severity)]
		wJ := severityWeight[strings.ToLower(sortedFindings[j].Severity)]
		if wI != wJ {
			return wI > wJ
		}
		// Sort alphabetically by file path on equal severity
		return sortedFindings[i].FilePath < sortedFindings[j].FilePath
	})

	const maxFullDetailThreshold = 25
	if len(sortedFindings) <= maxFullDetailThreshold {
		aiFindings = sortedFindings
	} else {
		log.Printf("[TaskRunner] Findings count %d exceeds %d. Generating simplified findings payload for AI synthesis...\n", len(sortedFindings), maxFullDetailThreshold)
		for i, f := range sortedFindings {
			if i < maxFullDetailThreshold {
				// Keep full details for top 25 high-priority findings
				aiFindings = append(aiFindings, f)
			} else {
				// Stripping large fields (code snippets and details) for lower-priority findings to compress tokens
				f.CodeSnippet = ""
				f.Detail = "详细内容请查阅随附的完整版 JSON 发现清单附件。"
				f.Suggestion = "请查阅完整清单文件获取本项的具体修改建议。"
				aiFindings = append(aiFindings, f)
			}
		}
		suffixPrompt += fmt.Sprintf("\n\n此外，本次分析发现的问题总数较多（共 %d 处）。为了精炼报告，我们在输入的 JSON 中对排在第 %d 位以后的次要或低风险发现进行了简化（隐去了代码片段和详细分析）。在生成『三、发现的问题』章节时，请仅对前 %d 个高风险问题进行详细罗列展示；对于其余的简化问题，请按照文件、分类或影响进行归聚归集（例如：『* 其它低危问题：共 X 处，位于 http_client.go:12 等地方，影响代码健壮性...』），或者简要列举它们的类别及位置，切勿逐个平铺列出，以避免因内容过长而被大模型强制截断。",
			len(sortedFindings), maxFullDetailThreshold+1, maxFullDetailThreshold)
	}

	// 4. Serialize simplified findings to a temporary file for AI input
	aiFindingsJSON, _ := json.MarshalIndent(aiFindings, "", "  ")
	synthesisAIInputPath := filepath.Join(filepath.Dir(ctx.reportPath), fmt.Sprintf("report-%d-synthesis-%s-for-ai.json", ctx.report.ID, safeRepoName))
	if err := os.WriteFile(synthesisAIInputPath, aiFindingsJSON, 0644); err != nil {
		return fmt.Errorf("failed to write AI synthesis input: %w", err)
	}
	defer os.Remove(synthesisAIInputPath)

	synthStart := time.Now()
	var lastErr error
	maxRetries := 3
	for attempt := 0; attempt <= maxRetries; attempt++ {
		ctx.Summary.Synthesis.Attempts = attempt + 1
		if attempt > 0 {
			log.Printf("[TaskRunner] executeSynthesis failed (attempt %d/%d) for ReportID %d, retrying in %ds: %v\n",
				attempt, maxRetries, ctx.report.ID, attempt*2, lastErr)
			time.Sleep(time.Duration(attempt*2) * time.Second)

			// Clean up previous temporary and dirty artifacts
			cleanSynthesisTempFiles(ctx.reportPath)
		}

		err := ctx.executeSynthesisOnce(synthesisAIInputPath, suffixPrompt)
		if err == nil {
			log.Printf("[TaskRunner] Synthesis phase complete for ReportID %d\n", ctx.report.ID)
			ctx.Summary.Synthesis.Status = "success"
			ctx.Summary.Synthesis.StartTime = synthStart
			ctx.Summary.Synthesis.EndTime = time.Now()
			ctx.Summary.Synthesis.DurationSeconds = ctx.Summary.Synthesis.EndTime.Sub(synthStart).Seconds()
			return nil
		}
		lastErr = err
	}

	ctx.Summary.Synthesis.Status = "failed"
	ctx.Summary.Synthesis.StartTime = synthStart
	ctx.Summary.Synthesis.EndTime = time.Now()
	ctx.Summary.Synthesis.DurationSeconds = ctx.Summary.Synthesis.EndTime.Sub(synthStart).Seconds()
	ctx.Summary.Synthesis.ErrorMessage = lastErr.Error()
	return fmt.Errorf("synthesis failed after %d retries: %w", maxRetries, lastErr)
}

func (ctx *taskContext) executeSynthesisOnce(synthesisInputPath string, suffixPrompt string) error {
	promptMsg := "请基于以下 JSON 分析发现，生成综合 Markdown 报告"
	if suffixPrompt != "" {
		promptMsg += "\n\n" + suffixPrompt
	}
	// Call AI with synthesis prompt, passing the JSON file as input
	if err := ctx.executeAI([]string{synthesisInputPath}, promptMsg, ctx.taskType.SynthesisPromptFile(), ctx.reportPath); err != nil {
		return err
	}

	// 校验最终报告是否成功生成且不为空
	info, err := os.Stat(ctx.reportPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("report file %s was not generated", ctx.reportPath)
		}
		return fmt.Errorf("failed to check report file: %w", err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("generated report file is empty")
	}

	return nil
}

// cleanSynthesisTempFiles cleans up temporary files generated during a failed synthesis attempt.
func cleanSynthesisTempFiles(reportPath string) {
	os.Remove(reportPath)
	os.Remove(reportPath + ".output.txt")
	os.Remove(reportPath + ".debug.log")
}

// runPostProcess parses the AI output using the task-specific postprocess script
func (ctx *taskContext) runPostProcess() TaskResult {
	updateTaskStatus(ctx.report.ID, models.StatusPostProcessing)
	var result TaskResult

	// 1. Load all findings for this report from the database to calculate score and metrics
	findings := ctx.findings
	severityWeight := map[string]int{
		"阻塞":        5,
		"blocking":  5,
		"严重":        4,
		"critical":  4,
		"主要":        3,
		"major":     3,
		"提示":        2,
		"info":      2,
		"建议":        1,
		"suggestion": 1,
		"合格":        0,
		"pass":      0,
		"高风险":       5,
		"high":      5,
		"high_risk": 5,
		"中风险":       3,
		"medium":    3,
		"medium_risk": 3,
		"low":       1,
		"低风险":       1,
		"low_risk":  1,
	}

	metrics := map[string]int{}
	score := 0
	for _, f := range findings {
		sev := strings.ToLower(f.Severity)
		weight := severityWeight[sev]
		score += weight

		key := sev
		switch sev {
		case "阻塞", "blocking":
			key = "blocking"
		case "严重", "critical":
			key = "critical"
		case "主要", "major":
			key = "major"
		case "提示", "info":
			key = "hint"
		case "建议", "suggestion":
			key = "suggestion"
		case "高风险", "high", "high_risk":
			key = "high_risk"
		case "中风险", "medium", "medium_risk":
			key = "medium_risk"
		case "低风险", "low", "low_risk":
			key = "low_risk"
		}
		metrics[key]++
	}
	result.Score = score
	result.Metrics = metrics

	// 2. Construct clean summary directly from findings severity metrics
	var summaryParts []string
	if result.Metrics["blocking"] > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("阻塞：%d个", result.Metrics["blocking"]))
	}
	if result.Metrics["critical"] > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("严重：%d个", result.Metrics["critical"]))
	}
	if result.Metrics["major"] > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("主要：%d个", result.Metrics["major"]))
	}
	if result.Metrics["hint"] > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("提示：%d个", result.Metrics["hint"]))
	}
	if result.Metrics["suggestion"] > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("建议：%d个", result.Metrics["suggestion"]))
	}
	if result.Metrics["high_risk"] > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("高风险：%d个", result.Metrics["high_risk"]))
	}
	if result.Metrics["medium_risk"] > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("中风险：%d个", result.Metrics["medium_risk"]))
	}
	if result.Metrics["low_risk"] > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("低风险：%d个", result.Metrics["low_risk"]))
	}

	if len(summaryParts) == 0 {
		result.Summary = "未发现任何问题，代码质量极佳！"
	} else {
		result.Summary = strings.Join(summaryParts, "，")
	}

	log.Printf("[TaskRunner] Postprocess calculated in Go - Score: %d, Summary len: %d, Metrics: %v\n", score, len(result.Summary), metrics)
	return result
}

func (ctx *taskContext) writeSummaryReport() {
	if ctx.jsonPath == "" {
		return
	}
	ctx.Summary.EndTime = time.Now()
	ctx.Summary.DurationSeconds = ctx.Summary.EndTime.Sub(ctx.Summary.StartTime).Seconds()

	if ctx.Summary.Status == "" {
		if ctx.Summary.Analysis.Status == "failed" || ctx.Summary.Synthesis.Status == "failed" {
			ctx.Summary.Status = "failed"
		} else {
			ctx.Summary.Status = "success"
		}
	}

	reportData, err := json.MarshalIndent(ctx.Summary, "", "  ")
	if err != nil {
		log.Printf("[TaskRunner] Failed to marshal task summary report: %v\n", err)
		return
	}

	if err := os.WriteFile(ctx.jsonPath, reportData, 0644); err != nil {
		log.Printf("[TaskRunner] Failed to write task summary report to %s: %v\n", ctx.jsonPath, err)
	} else {
		log.Printf("[TaskRunner] Successfully saved unified task summary report to %s\n", ctx.jsonPath)
	}
}

// finalize saves the result to DB and triggers notification
func (ctx *taskContext) finalize(result TaskResult) error {
	metricsJSON, _ := json.Marshal(result.Metrics)
	
	err := models.DB.Model(&models.TaskReport{}).Where("id = ?", ctx.report.ID).Updates(map[string]interface{}{
		"status":      models.StatusSuccess,
		"report_path": ctx.reportPath,
		"ai_summary":  result.Summary,
		"score":       result.Score,
		"metrics":     string(metricsJSON),
		"created_at":  time.Now(),
	}).Error

	ctx.Summary.Status = "success"
	ctx.writeSummaryReport()

	if ctx.autoNotify && result.Score >= ctx.taskType.NotifyThreshold {
		NotifyTaskResult(ctx.repo, ctx.taskType, result, "", ctx.report.ID, ctx.reportPath)
	}

	return err
}

func (ctx *taskContext) markFailed(errMsg string) {
	updates := map[string]interface{}{
		"status":     models.StatusFailed,
		"ai_summary": fmt.Sprintf("【执行失败】%s", errMsg),
		"created_at": time.Now(),
	}
	if ctx.reportPath != "" {
		updates["report_path"] = ctx.reportPath
	}
	models.DB.Model(&models.TaskReport{}).Where("id = ?", ctx.report.ID).Updates(updates)

	ctx.Summary.Status = "failed"
	if ctx.Summary.Analysis.Status == "" {
		ctx.Summary.Analysis.Status = "failed"
	}
	ctx.writeSummaryReport()

	if ctx.reportPath != "" {
		cliOutputPath := ctx.reportPath + ".output.txt"
		alreadyLogged := false
		if contentBytes, err := os.ReadFile(cliOutputPath); err == nil {
			content := string(contentBytes)
			if strings.Contains(content, "[Code-Shield Error]") && strings.Contains(content, errMsg) {
				alreadyLogged = true
			}
		}

		if !alreadyLogged {
			var f *os.File
			var openErr error
			if _, statErr := os.Stat(cliOutputPath); os.IsNotExist(statErr) {
				f, openErr = os.Create(cliOutputPath)
			} else {
				f, openErr = os.OpenFile(cliOutputPath, os.O_APPEND|os.O_WRONLY, 0644)
			}

			if openErr == nil {
				defer f.Close()
				f.WriteString(fmt.Sprintf("\n\n[Code-Shield Error] AI execution failed: %s\n", errMsg))
			} else {
				log.Printf("[TaskRunner] Failed to write error to output.txt: %v\n", openErr)
			}
		}
	}
}

// NotifyTaskResult sends a notification for a completed task
func NotifyTaskResult(repo models.Repository, taskType models.TaskType, result TaskResult, specificRecipientEmail string, reportID uint, reportPath string) {
	models.DB.Preload("Owner").Preload("Team.Leader").First(&repo, repo.ID)

	toEmails := []string{}
	ccEmails := []string{}

	if specificRecipientEmail != "" {
		toEmails = []string{specificRecipientEmail}
	} else {
		if repo.Owner.Email != "" { toEmails = append(toEmails, repo.Owner.Email) }
		
		if repo.Team.Leader.Email != "" && repo.Team.Leader.Email != repo.Owner.Email {
			ccEmails = append(ccEmails, repo.Team.Leader.Email)
		}

		// Unmarshal related members and append their emails
		var relatedIDs []string
		if len(repo.RelatedMembers) > 0 {
			_ = json.Unmarshal(repo.RelatedMembers, &relatedIDs)
		}

		if len(relatedIDs) > 0 {
			var members []models.Member
			models.DB.Where("id IN ?", relatedIDs).Find(&members)
			for _, m := range members {
				if m.Email != "" && m.Email != repo.Owner.Email {
					// Prevent duplicate CC entries
					duplicate := false
					for _, cc := range ccEmails {
						if cc == m.Email {
							duplicate = true
							break
						}
					}
					if !duplicate {
						ccEmails = append(ccEmails, m.Email)
					}
				}
			}
		}

		// Add task-type-level CC recipients
		var taskTypeCcEmails []string
		if len(taskType.NotifyCc) > 0 {
			_ = json.Unmarshal(taskType.NotifyCc, &taskTypeCcEmails)
		}
		for _, email := range taskTypeCcEmails {
			if email == "" {
				continue
			}
			duplicate := false
			for _, existing := range append(toEmails, ccEmails...) {
				if existing == email {
					duplicate = true
					break
				}
			}
			if !duplicate {
				ccEmails = append(ccEmails, email)
			}
		}
	}

	subject := fmt.Sprintf("【%s】%s %s报告（评分: %d）",
		taskType.DisplayName, repo.Name, taskType.DisplayName, result.Score)

	safeRepoName := strings.ReplaceAll(repo.Name, "/", "-")
	markdownFilename := ""
	synthesisJSONFilename := ""
	synthesisJSONContent := ""
	markdownContent := ""
	reportURL := ""
	if reportID > 0 {
		baseURL := strings.TrimSuffix(models.AppConfig.Server.ExternalURL, "/")
		reportURL = fmt.Sprintf("%s/public/reports/%d", baseURL, reportID)
	}

	if reportID > 0 && reportPath != "" {
		if contentBytes, err := os.ReadFile(reportPath); err == nil {
			markdownContent = string(contentBytes)
			markdownFilename = fmt.Sprintf("report-%d-report-%s.md", reportID, safeRepoName)
		} else {
			log.Printf("[Error] NotifyTaskResult: failed to read markdown report file at %s: %v\n", reportPath, err)
		}
		synthesisJSONFilename = fmt.Sprintf("report-%d-synthesis-%s.json", reportID, safeRepoName)
		synthesisPath := filepath.Join(filepath.Dir(reportPath), synthesisJSONFilename)
		if contentBytes, err := os.ReadFile(synthesisPath); err == nil {
			synthesisJSONContent = string(contentBytes)
		} else {
			log.Printf("[Warning] NotifyTaskResult: failed to read synthesis JSON file at %s: %v\n", synthesisPath, err)
		}
	}

	payload := map[string]interface{}{
		"task_id":            fmt.Sprintf("task-%d-%d", repo.ID, time.Now().Unix()),
		"task_type":          taskType.Name,
		"task_display_name":  taskType.DisplayName,
		"repo_name":          repo.Name,
		"branch":             repo.Branch,
		"recipients":         map[string]interface{}{"to": toEmails, "cc": ccEmails},
		"subject":            subject,
		"summary":            result.Summary,
		"markdown_content":   markdownContent,
		"report_url":         reportURL,
	}

	if markdownFilename != "" {
		payload["markdown_filename"] = markdownFilename
	}
	if synthesisJSONFilename != "" {
		payload["synthesis_json_filename"] = synthesisJSONFilename
		payload["synthesis_json_content"]  = synthesisJSONContent
	}

	targetURL := models.AppConfig.Notification.Webhook
	if targetURL == "" { return }

	payloadBytes, _ := json.Marshal(payload)
	resp, err := http.Post(targetURL, "application/json", bytes.NewBuffer(payloadBytes))
	if err == nil {
		defer resp.Body.Close()
		log.Printf("[Notifier] Sent for RepoID %d (Status: %d)\n", repo.ID, resp.StatusCode)
	}
}

func updateTaskStatus(reportID uint, status string) {
	models.DB.Model(&models.TaskReport{}).Where("id = ?", reportID).Updates(map[string]interface{}{
		"status":     status,
		"created_at": time.Now(),
	})
}
