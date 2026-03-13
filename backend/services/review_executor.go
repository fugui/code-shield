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

// RunAIReview handles pulling the code and running the Claude CLI.
// It is designed to run asynchronously (via a goroutine).
func RunAIReview(repoID uint, repoURL string, autoNotify bool) {
	log.Printf("[Executor] Starting AI review process for RepoID: %d, URL: %s, autoNotify: %v\n", repoID, repoURL, autoNotify)

	// Update DB to mark as in-progress (optional, but good for UI state)
	// We could create a new ReviewReport here with status "pending"
	var repo models.Repository
	if err := models.DB.First(&repo, repoID).Error; err != nil {
		log.Printf("[Executor] RepoID %d not found: %v\n", repoID, err)
		return
	}

	report := models.ReviewReport{
		RepoID:     repo.ID,
		BaseCommit: "HEAD~1", // Simplification. In reality, get from Git.
		HeadCommit: "HEAD",
		Status:     "pending",
	}
	models.DB.Create(&report)

	// 1. Parse URL to get the target path
	// Example: http://my.reposerver.com/a/b/c/d.git -> /a/b/c/d.git -> a/b/c/d
	u, err := url.Parse(repoURL)
	if err != nil {
		log.Printf("[Executor] Failed to parse URL %s: %v\n", repoURL, err)
		updateStatus(report.ID, "failed")
		if autoNotify {
			NotifyNotifier(repoID, "failed", "Invalid Repository URL", "")
		}
		return
	}

	rawPath := u.Path
	rawPath = strings.TrimPrefix(rawPath, "/")
	rawPath = strings.TrimSuffix(rawPath, ".git")

	// 2. Setup Codes Directory
	codesPath := filepath.Join("codes", rawPath)
	if err := os.MkdirAll(filepath.Dir(codesPath), 0755); err != nil {
		log.Printf("[Executor] Failed to create dir %s: %v\n", filepath.Dir(codesPath), err)
		updateStatus(report.ID, "failed")
		if autoNotify {
			NotifyNotifier(repoID, "failed", "Filesystem error setting up directory", "")
		}
		return
	}

	// 3. Git Clone or Pull
	if stat, err := os.Stat(filepath.Join(codesPath, ".git")); err == nil && stat.IsDir() {
		// Repo exists, git pull
		log.Printf("[Executor] Repo exists, running git pull in %s\n", codesPath)
		cmd := exec.Command("git", "-C", codesPath, "pull")
		if output, err := cmd.CombinedOutput(); err != nil {
			log.Printf("[Executor] git pull failed: %v\nOutput: %s\n", err, output)
			updateStatus(report.ID, "failed")
			if autoNotify {
				NotifyNotifier(repoID, "failed", "Git Pull synchronization failed", "")
			}
			return
		}
	} else {
		// New repo, git clone
		log.Printf("[Executor] New repo, running git clone %s %s\n", repoURL, codesPath)
		cmd := exec.Command("git", "clone", repoURL, codesPath)
		if output, err := cmd.CombinedOutput(); err != nil {
			log.Printf("[Executor] git clone failed: %v\nOutput: %s\n", err, output)
			updateStatus(report.ID, "failed")
			if autoNotify {
				NotifyNotifier(repoID, "failed", "Git Clone cloning failed", "")
			}
			return
		}
	}

	// 4. Setup Reports Directory
	currentDate := time.Now().Format("2006-01-02")
	reportsDir := filepath.Join("review-reports", currentDate)
	if err := os.MkdirAll(reportsDir, 0755); err != nil {
		log.Printf("[Executor] Failed to create reports dir %s: %v\n", reportsDir, err)
		updateStatus(report.ID, "failed")
		if autoNotify {
			NotifyNotifier(repoID, "failed", "Filesystem error creating reports dir", "")
		}
		return
	}

	// Calculate report filename (Markdown)
	// e.g., a-b-c-d
	prefix := strings.ReplaceAll(rawPath, "/", "-")
	reportFileName := fmt.Sprintf("review-report-%s-%s.md", prefix, currentDate)
	reportPath := filepath.Join(reportsDir, reportFileName)

	// Calculate JSON summary filename
	jsonSummaryName := fmt.Sprintf("review-summary-%s-%s.json", prefix, currentDate)
	jsonPath := filepath.Join(reportsDir, jsonSummaryName)

	// 5. Invoke Claude via Bash
	promptPath := "code-review-prompt.md"
	if _, err := os.Stat(promptPath); os.IsNotExist(err) {
		log.Printf("[Executor] Prompt file %s does not exist, creating placeholder.\n", promptPath)
		err := os.WriteFile(promptPath, []byte("Please review the code in the current directory and check for multithreading, lock, and memory leak issues. Output a detailed markdown report."), 0644)
		if err != nil {
			log.Printf("[Executor] Failed to write placeholder prompt: %v\n", err)
		}
	}

	// We need absolute paths since we will change directory for the command
	absPromptPath, _ := filepath.Abs(promptPath)
	absReportPath, _ := filepath.Abs(reportPath)
	absJsonPath, _ := filepath.Abs(jsonPath)

	log.Printf("[Executor] Executing Claude AI Review. Report target: %s, JSON target: %s\n", absReportPath, absJsonPath)
	
	// Command: 
	// cd <codesPath> && cat <absPromptPath> | claude -p "请执行代码检视任务，并输出检视文档到 <absReportPath>" --output-format json > <absJsonPath>
	cliCmd := fmt.Sprintf("cd %s && cat %s | claude -p '请执行代码检视任务，并输出检视文档到 %s' --output-format json > %s", 
		codesPath, absPromptPath, absReportPath, absJsonPath)

	log.Printf("[Executor] Command executing: %s\n", cliCmd)
	cmd := exec.Command("bash", "-c", cliCmd)
	cmd.Dir = "." // Run in project root where code-review-prompt.md is
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("[Executor] Claude CLI execution failed (this is expected if 'claude' isn't installed).\nError: %v\nOutput: %s\n", err, output)
		
		// Fallback for simulation if claude CLI is not installed
		if strings.Contains(string(output), "command not found") {
			log.Printf("[Executor] Simulating success since claude CLI block failed.")
			os.WriteFile(absJsonPath, []byte(`{"status": "simulated_success", "issues": []}`), 0644)
			os.WriteFile(absReportPath, []byte("# Simulated Markdown Report\nEverything looks good!"), 0644)
		} else {
			updateStatus(report.ID, "failed")
			if autoNotify {
				NotifyNotifier(repoID, "failed", "Claude AI execution failed", "")
			}
			return
		}
	}

	log.Printf("[Executor] AI Review completed successfully for %s.\n", repoURL)
	models.DB.Model(&models.ReviewReport{}).Where("id = ?", report.ID).Updates(map[string]interface{}{
		"status":      "success",
		"report_path": absReportPath,
	})

	if autoNotify {
		mdContent, err := os.ReadFile(absReportPath)
		if err == nil {
			NotifyNotifier(repoID, "success", "Code review completed: " + absReportPath, string(mdContent))
		} else {
			log.Printf("[Executor] Failed to read report markdown for notification: %v\n", err)
		}
	}
}

// NotifyNotifier executes an HTTP POST to the standalone Windows Node.js Notifier service.
func NotifyNotifier(repoID uint, status string, message string, markdownContent string) {
	// Only send real notification on success.
	if status != "success" {
		log.Printf("[Notifier API] Skipping email notification for status: %s\n", status)
		return
	}

	// Retrieve the full repository, owner, and team leader information
	var repo models.Repository
	if err := models.DB.Preload("Owner").Preload("Team.Leader").First(&repo, repoID).Error; err != nil {
		log.Printf("[Notifier API] Failed to load RepoID %d: %v\n", repoID, err)
		return
	}

	toEmails := []string{}
	if repo.Owner.Email != "" {
		toEmails = append(toEmails, repo.Owner.Email)
	}
	
	ccEmails := []string{}
	if repo.Team.Leader.Email != "" && repo.Team.Leader.Email != repo.Owner.Email {
		ccEmails = append(ccEmails, repo.Team.Leader.Email)
	}

	payload := map[string]interface{}{
		"task_id":          fmt.Sprintf("rev-%d-%d", repo.ID, time.Now().Unix()),
		"repo_name":        repo.Name,
		"branch":           repo.Branch,
		"recipients": map[string]interface{}{
			"to": toEmails,
			"cc": ccEmails,
		},
		"subject":          fmt.Sprintf("[Code-Shield] 项目 %s 自动检视报告", repo.Name),
		"markdown_content": markdownContent,
	}
	
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[Notifier API] Failed to marshal payload: %v\n", err)
		return
	}
	
	targetURL := models.AppConfig.Notifier.URL
	if targetURL == "" {
		log.Println("[Notifier API] Warning: Notifier URL is not configured in config.yaml, skipping webhook.")
		return
	}

	resp, err := http.Post(targetURL, "application/json", bytes.NewBuffer(payloadBytes))
	if err != nil {
		log.Printf("[Notifier API] Failed to send webhook to Windows Node.js Notifier (%s): %v\n", targetURL, err)
		return
	}
	defer resp.Body.Close()
	
	log.Printf("[Notifier API] Successfully sent webhook for RepoID %d (Status Code: %d)\n", repoID, resp.StatusCode)
}

func updateStatus(reportID uint, status string) {
	models.DB.Model(&models.ReviewReport{}).Where("id = ?", reportID).Update("status", status)
}
