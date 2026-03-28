package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

var DB *sql.DB

type DraftEntity struct {
	ID        int64
	TaskID    string
	To        string
	CC        string
	Subject   string
	HtmlBody  string
	PdfPath   string
	Status    string // "草稿", "已发送", "等待发送", "失败"
	CreatedAt time.Time
}

func initDB() error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	dbPath := filepath.Join(filepath.Dir(exePath), "notifier.db")
	DB, err = sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}

	createTableSQL := `
	CREATE TABLE IF NOT EXISTS drafts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id TEXT,
		to_emails TEXT,
		cc_emails TEXT,
		subject TEXT,
		html_body TEXT,
		pdf_path TEXT,
		status TEXT,
		created_at DATETIME
	);
	CREATE TABLE IF NOT EXISTS config (
		key TEXT PRIMARY KEY,
		value TEXT
	);
	`
	_, err = DB.Exec(createTableSQL)
	if err != nil {
		return err
	}

	var count int
	DB.QueryRow("SELECT COUNT(*) FROM config WHERE key = 'auto_send'").Scan(&count)
	if count == 0 {
		_, err = DB.Exec("INSERT INTO config (key, value) VALUES ('auto_send', 'false')")
	}

	return err
}

func GetAutoSend() bool {
	var val string
	err := DB.QueryRow("SELECT value FROM config WHERE key = 'auto_send'").Scan(&val)
	if err != nil {
		return false
	}
	return val == "true"
}

func SetAutoSend(enabled bool) error {
	val := "false"
	if enabled {
		val = "true"
	}
	_, err := DB.Exec("UPDATE config SET value = ? WHERE key = 'auto_send'", val)
	return err
}

func InsertDraft(draft DraftEntity) (int64, error) {
	stmt, err := DB.Prepare("INSERT INTO drafts (task_id, to_emails, cc_emails, subject, html_body, pdf_path, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)")
	if err != nil {
		return 0, err
	}
	defer stmt.Close()
	res, err := stmt.Exec(draft.TaskID, draft.To, draft.CC, draft.Subject, draft.HtmlBody, draft.PdfPath, draft.Status, draft.CreatedAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func UpdateDraftStatus(id int64, status string) error {
	_, err := DB.Exec("UPDATE drafts SET status = ? WHERE id = ?", status, id)
	return err
}

func GetAllDrafts() ([]DraftEntity, error) {
	rows, err := DB.Query("SELECT id, task_id, to_emails, cc_emails, subject, html_body, pdf_path, status, created_at FROM drafts ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var drafts []DraftEntity
	for rows.Next() {
		var draft DraftEntity
		err = rows.Scan(&draft.ID, &draft.TaskID, &draft.To, &draft.CC, &draft.Subject, &draft.HtmlBody, &draft.PdfPath, &draft.Status, &draft.CreatedAt)
		if err != nil {
			return nil, err
		}
		drafts = append(drafts, draft)
	}
	return drafts, nil
}

func GetDraftByID(id int64) (DraftEntity, error) {
	var draft DraftEntity
	err := DB.QueryRow("SELECT id, task_id, to_emails, cc_emails, subject, html_body, pdf_path, status, created_at FROM drafts WHERE id = ?", id).
		Scan(&draft.ID, &draft.TaskID, &draft.To, &draft.CC, &draft.Subject, &draft.HtmlBody, &draft.PdfPath, &draft.Status, &draft.CreatedAt)
	return draft, err
}

func DeleteDraft(id int64) error {
	_, err := DB.Exec("DELETE FROM drafts WHERE id = ?", id)
	return err
}
