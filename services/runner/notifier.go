package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"code-shield/models"
)

// NotifyTaskResult 针对已完成的任务向责任人、部门主管及抄送人发送邮件或 Webhook 通告
func NotifyTaskResult(repo models.Repository, taskType models.TaskType, result TaskResult, specificRecipientEmail string, reportID uint, reportPath string) {
	if models.DB != nil {
		models.DB.Preload("Owner").Preload("Department.Leader").First(&repo, repo.ID)
	}

	toEmails := []string{}
	ccEmails := []string{}

	if specificRecipientEmail != "" {
		toEmails = []string{specificRecipientEmail}
	} else {
		if repo.Owner.Email != "" {
			toEmails = append(toEmails, repo.Owner.Email)
		}

		if repo.Department.Leader != nil && repo.Department.Leader.Email != "" && repo.Department.Leader.Email != repo.Owner.Email {
			ccEmails = append(ccEmails, repo.Department.Leader.Email)
		}

		var relatedIDs []string
		if len(repo.RelatedMembers) > 0 {
			_ = json.Unmarshal(repo.RelatedMembers, &relatedIDs)
		}

		if len(relatedIDs) > 0 && models.DB != nil {
			var numericIDs []uint
			for _, idStr := range relatedIDs {
				if num, err := strconv.ParseUint(strings.TrimSpace(idStr), 10, 64); err == nil && num > 0 {
					numericIDs = append(numericIDs, uint(num))
				}
			}

			var users []models.User
			if len(numericIDs) > 0 {
				models.DB.Where("id IN ? OR employee_id IN ? OR email IN ?", numericIDs, relatedIDs, relatedIDs).Find(&users)
			} else {
				models.DB.Where("employee_id IN ? OR email IN ?", relatedIDs, relatedIDs).Find(&users)
			}
			for _, u := range users {
				if u.Email != "" && u.Email != repo.Owner.Email {
					duplicate := false
					for _, cc := range ccEmails {
						if cc == u.Email {
							duplicate = true
							break
						}
					}
					if !duplicate {
						ccEmails = append(ccEmails, u.Email)
					}
				}
			}
		}

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

	subject := fmt.Sprintf("【%s】%s %s报告（风险评分: %d）",
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
		"task_id":           fmt.Sprintf("task-%d-%d", repo.ID, time.Now().Unix()),
		"task_type":         taskType.Name,
		"task_display_name": taskType.DisplayName,
		"repo_name":         repo.Name,
		"branch":            repo.Branch,
		"recipients":        map[string]interface{}{"to": toEmails, "cc": ccEmails},
		"subject":           subject,
		"summary":           result.Summary,
		"markdown_content":  markdownContent,
		"report_url":        reportURL,
	}

	if markdownFilename != "" {
		payload["markdown_filename"] = markdownFilename
	}
	if synthesisJSONFilename != "" {
		payload["synthesis_json_filename"] = synthesisJSONFilename
		payload["synthesis_json_content"] = synthesisJSONContent
	}

	targetURL := models.AppConfig.Notification.Webhook
	if targetURL == "" {
		return
	}

	payloadBytes, _ := json.Marshal(payload)
	resp, err := http.Post(targetURL, "application/json", bytes.NewBuffer(payloadBytes))
	if err == nil {
		defer resp.Body.Close()
		log.Printf("[Notifier] Sent for RepoID %d (Status: %d)\n", repo.ID, resp.StatusCode)
	}
}
