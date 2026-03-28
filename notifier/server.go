package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type NotifyPayload struct {
	TaskID          string `json:"task_id"`
	RepoName        string `json:"repo_name"`
	Branch          string `json:"branch"`
	Recipients      struct {
		To []string `json:"to"`
		CC []string `json:"cc"`
	} `json:"recipients"`
	Subject         string `json:"subject"`
	MarkdownContent string `json:"markdown_content"`
}

type NotifyResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

func StartHTTPServer() {
	http.HandleFunc("/api/notify", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(NotifyResponse{Success: true})
	})

	http.HandleFunc("/api/notify/email", handleEmailNotify)

	port := ":8081"
	LogMessage("HTTP Server started on 0.0.0.0" + port)
	err := http.ListenAndServe(port, nil)
	if err != nil {
		LogMessage(fmt.Sprintf("HTTP Server failed: %v", err))
	}
}

func handleEmailNotify(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(NotifyResponse{Success: false, Error: "Method not allowed"})
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(NotifyResponse{Success: false, Error: "Failed to read body"})
		return
	}
	defer r.Body.Close()

	var payload NotifyPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(NotifyResponse{Success: false, Error: "Invalid JSON payload"})
		return
	}

	if payload.MarkdownContent == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(NotifyResponse{Success: false, Error: "Missing markdown_content format"})
		return
	}

	LogMessage(fmt.Sprintf("Received email request for task: %s", payload.TaskID))

	go processEmailPayload(payload)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(NotifyResponse{Success: true, Message: "Payload received. Processing in background."})
}

func processEmailPayload(payload NotifyPayload) {
	LogMessage("Generating PDF for task: " + payload.TaskID)
	pdfPath, err := GeneratePDF(payload.MarkdownContent, payload.TaskID)
	if err != nil {
		LogMessage(fmt.Sprintf("Failed to generate PDF: %v", err))
		return
	}
	LogMessage("PDF generated: " + pdfPath)

	toEmails := strings.Join(payload.Recipients.To, ";")
	ccEmails := strings.Join(payload.Recipients.CC, ";")
	
	summaryText := ExtractSummary(payload.MarkdownContent)
	summaryHtml, err := RenderMarkdownToHTML(summaryText)
	if err != nil {
		LogMessage(fmt.Sprintf("Failed to render HTML summary: %v", err))
		summaryHtml = "<p>" + summaryText + "</p>"
	}

	// Log to GUI View
	timeStr := time.Now().Format("01-02 15:04:05")
	LogMessage(fmt.Sprintf("Ready to interact with Outlook for task: %s", payload.TaskID))
	
	err = CreateAndHandleEmail(toEmails, ccEmails, payload.Subject, summaryHtml, pdfPath, GetAutoSend())
	if err != nil {
		LogMessage(fmt.Sprintf("Failed to Create/Send email via Outlook: %v", err))
		AddDraftLogToView("失败", toEmails, payload.Subject, timeStr)
		return
	}

	if GetAutoSend() {
		LogMessage(fmt.Sprintf("Auto-send is ON. Email sent via Outlook! (Task: %s)", payload.TaskID))
		AddDraftLogToView("已发送", toEmails, payload.Subject, timeStr)
	} else {
		LogMessage(fmt.Sprintf("Email saved to Outlook Drafts folder. (Task: %s)", payload.TaskID))
		AddDraftLogToView("保存草稿", toEmails, payload.Subject, timeStr)
	}
}
