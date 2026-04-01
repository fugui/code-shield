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

	log.Printf("[TaskRunner] Starting task for ReportID: %d, URL: %s, TaskType: %s\n", 
		ctx.report.ID, repoURL, ctx.taskType.Name)

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
		return ErrSkipped
	}

	// 4. Execute AI Engine
	if err := ctx.executeAI(); err != nil {
		ctx.markFailed(err.Error())
		return err
	}

	// 5. Post-process and Finalize
	result := ctx.runPostProcess()
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
	ctx.codesPath = filepath.Join(models.AppConfig.Workspace.Home, "codes", rawPath)

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
	if ctx.taskType.PreconditionScript == "" {
		return false, nil
	}

	updateTaskStatus(ctx.report.ID, models.StatusPreProcessing)
	absScript := models.AppConfig.GetAbsPath(ctx.taskType.PreconditionScript)
	
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

// executeAI invokes the external AI CLI
func (ctx *taskContext) executeAI() error {
	updateTaskStatus(ctx.report.ID, models.StatusAnalyzing)

	// Prepare output paths
	currentDate := time.Now().Format("2006-01-02")
	reportsDir := filepath.Join(models.AppConfig.Workspace.Home, "reports", ctx.taskType.Name, currentDate)
	os.MkdirAll(reportsDir, 0755)

	safeRepoName := strings.ReplaceAll(ctx.repo.Name, "/", "-")
	ctx.reportPath = filepath.Join(reportsDir, fmt.Sprintf("%s-report-%s.md", ctx.taskType.Name, safeRepoName))
	ctx.jsonPath = filepath.Join(reportsDir, fmt.Sprintf("%s-summary-%s.json", ctx.taskType.Name, safeRepoName))

	absPrompt := models.AppConfig.GetAbsPath(ctx.taskType.PromptFile)
	if _, err := os.Stat(absPrompt); os.IsNotExist(err) {
		return fmt.Errorf("prompt file not found: %s", absPrompt)
	}

	cliCmd := fmt.Sprintf("cd %s && cat %s | claude -p '请执行%s任务，并输出文档到 %s' --output-format json > %s",
		ctx.codesPath, absPrompt, ctx.taskType.DisplayName, ctx.reportPath, ctx.jsonPath)

	timeout := time.Duration(ctx.taskType.Timeout) * time.Minute
	if timeout <= 0 { timeout = 30 * time.Minute }

	ctxRun, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Printf("[TaskRunner] Executing AI: %s\n", cliCmd)
	cmd := exec.CommandContext(ctxRun, "bash", "-c", cliCmd)
	if output, err := cmd.CombinedOutput(); err != nil {
		if ctxRun.Err() == context.DeadlineExceeded {
			return fmt.Errorf("AI execution timed out after %v", timeout)
		}
		// Fallback for demo/simulation if claude is missing
		if strings.Contains(string(output), "command not found") {
			log.Println("[TaskRunner] Simulating success (claude CLI not found)")
			os.WriteFile(ctx.jsonPath, []byte(`{"status": "simulated"}`), 0644)
			os.WriteFile(ctx.reportPath, []byte("# Simulated Report\nAI engine not found."), 0644)
			return nil
		}
		return fmt.Errorf("AI execution failed: %s", string(output))
	}

	return nil
}

// runPostProcess parses the AI output using the task-specific postprocess script
func (ctx *taskContext) runPostProcess() TaskResult {
	updateTaskStatus(ctx.report.ID, models.StatusPostProcessing)
	var result TaskResult

	if ctx.taskType.PostprocessScript == "" {
		return result
	}

	absPostScript := models.AppConfig.GetAbsPath(ctx.taskType.PostprocessScript)
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
			NotifyTaskResult(ctx.repo, ctx.taskType, result, string(content))
		}
	}

	return err
}

func (ctx *taskContext) markFailed(errMsg string) {
	updateTaskStatus(ctx.report.ID, models.StatusFailed)
}

// NotifyTaskResult sends a notification for a completed task
func NotifyTaskResult(repo models.Repository, taskType models.TaskType, result TaskResult, markdownContent string) {
	models.DB.Preload("Owner").Preload("Team.Leader").First(&repo, repo.ID)

	toEmails := []string{}
	if repo.Owner.Email != "" { toEmails = append(toEmails, repo.Owner.Email) }
	
	ccEmails := []string{}
	if repo.Team.Leader.Email != "" && repo.Team.Leader.Email != repo.Owner.Email {
		ccEmails = append(ccEmails, repo.Team.Leader.Email)
	}

	subject := fmt.Sprintf("【%s】%s %s报告（评分: %d）",
		taskType.DisplayName, repo.Name, taskType.DisplayName, result.Score)

	payload := map[string]interface{}{
		"task_id":   fmt.Sprintf("task-%d-%d", repo.ID, time.Now().Unix()),
		"task_type": taskType.Name,
		"repo_name": repo.Name,
		"branch":    repo.Branch,
		"recipients": map[string]interface{}{ "to": toEmails, "cc": ccEmails },
		"subject":          subject,
		"summary":          result.Summary,
		"markdown_content": markdownContent,
	}

	targetURL := models.AppConfig.Notifier.URL
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
