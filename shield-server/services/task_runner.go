package services

import (
	"bytes"
	"code-shield/models"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// TaskResult is the standardized output from a postprocess script
type TaskResult struct {
	Score   int            `json:"score"`
	Summary string         `json:"summary"`
	Metrics map[string]int `json:"metrics"`
}

// taskContext holds all necessary data for a single task execution run
type taskContext struct {
	report     models.TaskReport
	taskType   models.TaskType
	repo       models.Repository
	codesPath  string
	reportPath string
	jsonPath   string
	autoNotify bool
}

func RunTaskSync(reportID uint, repoURL string, taskTypeID uint, autoNotify bool) error {
	ctx := &taskContext{autoNotify: autoNotify}

	// 1. Initialize and load data
	if err := ctx.load(reportID, taskTypeID); err != nil {
		return err
	}

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

	// 4. Prepare output paths for final report
	ctx.prepareOutputPaths()

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
		cmd = exec.Command("git", "-C", ctx.codesPath, "pull")
	} else {
		log.Printf("[TaskRunner] Running git clone %s %s\n", repoURL, ctx.codesPath)
		cmd = exec.Command("git", "clone", repoURL, ctx.codesPath)
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

	cmd := exec.Command(absScript, ctx.codesPath)
	output, err := cmd.CombinedOutput()
	outputStr := strings.TrimSpace(string(output))

	if err != nil {
		if cmd.ProcessState.ExitCode() == 1 {
			log.Printf("[TaskRunner] Precondition skip: %s\n", outputStr)
			models.DB.Model(&models.TaskReport{}).Where("id = ?", ctx.report.ID).Updates(map[string]interface{}{
				"status":     models.StatusSkipped,
				"ai_summary": outputStr,
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
	currentDate := time.Now().Format("2006-01-02")
	reportsDir := filepath.Join(models.AppConfig.Storage.Root, "reports", ctx.taskType.Name, currentDate)
	os.MkdirAll(reportsDir, 0755)

	safeRepoName := strings.ReplaceAll(ctx.repo.Name, "/", "-")
	ctx.reportPath = filepath.Join(reportsDir, fmt.Sprintf("report-%d-%s.md", ctx.report.ID, safeRepoName))
	ctx.jsonPath = filepath.Join(reportsDir, fmt.Sprintf("summary-%d-%s.json", ctx.report.ID, safeRepoName))
}

// executeAI constructs the prompt and delegates to the configured AI CLI backend.
// outputPath controls where AI writes its content (reportPath for Markdown, jsonPath for JSON analysis).
func (ctx *taskContext) executeAI(fileList []string, customPromptSuffix string, promptFilePath string, outputPath string) error {
	updateTaskStatus(ctx.report.ID, models.StatusAnalyzing)

	absPrompt := models.AppConfig.GetAbsPath(promptFilePath)

	// 构造 prompt 消息
	promptMsg := fmt.Sprintf("请执行%s任务", ctx.taskType.DisplayName)
	if ctx.report.ChunkName != "" {
		promptMsg = fmt.Sprintf("你正在分析模块 [%s]，请执行%s任务", ctx.report.ChunkName, ctx.taskType.DisplayName)
	}
	if customPromptSuffix != "" {
		promptMsg += "。" + customPromptSuffix
	}

	// 根据配置选择 AI CLI 后端
	invoker := GetAIInvoker(models.AppConfig.AI.Backend)
	log.Printf("[TaskRunner] Invoking AI via %s (ReportID: %d, Output: %s)\n", invoker.Name(), ctx.report.ID, outputPath)

	return invoker.Invoke(AIRequest{
		WorkDir:    ctx.codesPath,
		PromptFile: absPrompt,
		PromptMsg:  promptMsg,
		InputFiles: fileList,
		OutputPath: outputPath,
		TimeoutMin: ctx.taskType.Timeout,
	})
}

// AnalysisOutput represents the JSON structure output by AI during the analysis phase
type AnalysisOutput struct {
	Findings []struct {
		Severity    string `json:"severity"`
		Category    string `json:"category"`
		FilePath    string `json:"file_path"`
		LineNumber  int    `json:"line_number"`
		CodeSnippet string `json:"code_snippet"`
		Title       string `json:"title"`
		Detail      string `json:"detail"`
		Suggestion  string `json:"suggestion"`
	} `json:"findings"`
	Summary string `json:"summary"`
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

	return []byte(s)
}

// executeAnalysis runs the analysis phase: AI outputs structured JSON findings
func (ctx *taskContext) executeAnalysis(fileList []string) ([]models.AnalysisFinding, error) {
	if err := ctx.executeAI(fileList, "请以纯 JSON 格式输出分析结果，不要输出 Markdown", ctx.taskType.AnalysisPromptFile(), ctx.jsonPath); err != nil {
		return nil, err
	}

	// Parse the AI JSON output from the report file
	rawJSON, err := os.ReadFile(ctx.jsonPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read analysis output: %w", err)
	}

	// Clean the AI output: strip markdown code block markers (```json ... ```)
	cleanedJSON := cleanJSONFromAI(rawJSON)

	var output AnalysisOutput
	if err := json.Unmarshal(cleanedJSON, &output); err != nil {
		log.Printf("[TaskRunner] Failed to parse analysis JSON: %v, attempting AI repair\n", err)
		log.Printf("[TaskRunner] Raw output (first 500 chars): %s\n", string(cleanedJSON[:min(len(cleanedJSON), 500)]))

		// Attempt AI-powered JSON repair
		repairedJSON, repairErr := RepairJSON(ctx.codesPath, ctx.jsonPath)
		if repairErr != nil {
			log.Printf("[TaskRunner] AI JSON repair failed: %v\n", repairErr)
			return nil, nil
		}
		if err := json.Unmarshal(repairedJSON, &output); err != nil {
			log.Printf("[TaskRunner] Repaired JSON still invalid: %v\n", err)
			return nil, nil
		}
		log.Println("[TaskRunner] AI JSON repair successful")
		// Overwrite the json file with the repaired version for downstream use
		os.WriteFile(ctx.jsonPath, repairedJSON, 0644)
	}

	// Convert to model objects and persist
	var findings []models.AnalysisFinding
	for _, f := range output.Findings {
		finding := models.AnalysisFinding{
			TaskReportID: ctx.report.ID,
			TaskTypeID:   ctx.taskType.ID,
			RepoID:       ctx.repo.ID,
			Severity:     f.Severity,
			Category:     f.Category,
			FilePath:     f.FilePath,
			LineNumber:   f.LineNumber,
			CodeSnippet:  f.CodeSnippet,
			Title:        f.Title,
			Detail:       f.Detail,
			Suggestion:   f.Suggestion,
		}
		findings = append(findings, finding)
	}

	// Batch insert into database
	if len(findings) > 0 {
		if err := models.DB.Create(&findings).Error; err != nil {
			log.Printf("[TaskRunner] Failed to save analysis findings: %v\n", err)
		}
	}

	log.Printf("[TaskRunner] Analysis phase complete: %d findings for ReportID %d\n", len(findings), ctx.report.ID)
	return findings, nil
}


// executeSynthesis runs the synthesis phase: AI generates final Markdown report from JSON findings
func (ctx *taskContext) executeSynthesis(allFindings []models.AnalysisFinding) error {

	// Serialize all findings to a JSON input file
	findingsJSON, _ := json.MarshalIndent(allFindings, "", "  ")
	synthesisInputPath := filepath.Join(filepath.Dir(ctx.reportPath), fmt.Sprintf("synthesis-input-%d.json", ctx.report.ID))
	if err := os.WriteFile(synthesisInputPath, findingsJSON, 0644); err != nil {
		return fmt.Errorf("failed to write synthesis input: %w", err)
	}
	defer os.Remove(synthesisInputPath)

	log.Printf("[TaskRunner] Starting synthesis with %d findings for ReportID %d\n", len(allFindings), ctx.report.ID)

	// Call AI with synthesis prompt, passing the JSON file as input
	return ctx.executeAI([]string{synthesisInputPath}, "请基于以上 JSON 分析发现，生成综合 Markdown 报告", ctx.taskType.SynthesisPromptFile(), ctx.reportPath)
}

// runPostProcess parses the AI output using the task-specific postprocess script
func (ctx *taskContext) runPostProcess() TaskResult {
	updateTaskStatus(ctx.report.ID, models.StatusPostProcessing)
	var result TaskResult

	absPostScript := models.AppConfig.GetAbsPath(ctx.taskType.PostprocessScript())
	if _, err := os.Stat(absPostScript); os.IsNotExist(err) {
		return result
	}
	log.Printf("[TaskRunner] Running postprocess: %s\n", absPostScript)

	// Ensure the script is executable
	os.Chmod(absPostScript, 0755)

	cmd := exec.Command(absPostScript, ctx.reportPath)
	if output, err := cmd.Output(); err == nil {
		if err := json.Unmarshal(output, &result); err != nil {
			log.Printf("[TaskRunner] Postprocess JSON error: %v\n", err)
		}
	} else {
		log.Printf("[TaskRunner] Postprocess script failed: %v\n", err)
	}
	return result
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

	if ctx.autoNotify && result.Score >= ctx.taskType.NotifyThreshold {
		if content, err := os.ReadFile(ctx.reportPath); err == nil {
			NotifyTaskResult(ctx.repo, ctx.taskType, result, string(content), "")
		}
	}

	return err
}

func (ctx *taskContext) markFailed(errMsg string) {
	updateTaskStatus(ctx.report.ID, models.StatusFailed)
}

// NotifyTaskResult sends a notification for a completed task
func NotifyTaskResult(repo models.Repository, taskType models.TaskType, result TaskResult, markdownContent string, specificRecipientEmail string) {
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

	payload := map[string]interface{}{
		"task_id":            fmt.Sprintf("task-%d-%d", repo.ID, time.Now().Unix()),
		"task_type":          taskType.Name,
		"task_display_name":  taskType.DisplayName,
		"repo_name":          repo.Name,
		"branch":             repo.Branch,
		"recipients": map[string]interface{}{ "to": toEmails, "cc": ccEmails },
		"subject":          subject,
		"summary":          result.Summary,
		"markdown_content": markdownContent,
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
	models.DB.Model(&models.TaskReport{}).Where("id = ?", reportID).Update("status", status)
}
