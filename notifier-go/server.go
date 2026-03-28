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

	draft := DraftEntity{
		TaskID:    payload.TaskID,
		To:        toEmails,
		CC:        ccEmails,
		Subject:   payload.Subject,
		HtmlBody:  summaryHtml,
		PdfPath:   pdfPath,
		Status:    "草稿",
		CreatedAt: time.Now(),
	}

	id, err := InsertDraft(draft)
	if err != nil {
		LogMessage(fmt.Sprintf("Failed to save draft to DB: %v", err))
		return
	}
	draft.ID = id
	
	LogMessage(fmt.Sprintf("Draft saved to DB (ID: %d)", id))
	
	RefreshDraftsUI()

	if GetAutoSend() {
		LogMessage(fmt.Sprintf("Auto-send enabled. Attempting to send Draft ID: %d", id))
		go SendDraft(draft)
	}
}
