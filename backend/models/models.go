package models

import (
	"time"
)

type Member struct {
	ID         string    `gorm:"primaryKey;column:id" json:"id"` // 员工的字符串ID
	Name       string    `gorm:"not null" json:"name"`           // 姓名
	Email      string    `gorm:"default:''" json:"email"`          // 邮箱地址
	Department string    `gorm:"default:''" json:"department"`     // 部门名称
	CreatedAt  time.Time `json:"created_at"`
}

type User struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Username  string    `gorm:"uniqueIndex;not null" json:"username"`
	Password  string    `gorm:"not null" json:"-"` // Omit password in JSON
	IsActive  bool      `gorm:"default:true" json:"is_active"`
	IsAdmin   bool      `gorm:"default:false" json:"is_admin"`
	CreatedAt time.Time `json:"created_at"`
}

type Team struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"uniqueIndex;not null" json:"name"`
	LeaderID  string    `json:"leader_id"`
	Leader    Member    `gorm:"foreignKey:LeaderID" json:"leader"`
	CreatedAt time.Time `json:"created_at"`
}

type Repository struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	TeamID         uint      `json:"team_id"`
	Team           Team      `gorm:"foreignKey:TeamID" json:"team"`
	Name           string    `gorm:"uniqueIndex;not null" json:"name"`
	URL            string    `gorm:"not null" json:"url"`
	OwnerID        string    `json:"owner_id"`
	Owner          Member    `gorm:"foreignKey:OwnerID" json:"owner"`
	Branch         string    `gorm:"default:main" json:"branch"`
	ServiceGroup   string    `gorm:"size:30" json:"service_group"`
	IsActive       bool      `gorm:"default:true" json:"is_active"`
	LastCommitHash string    `json:"last_commit_hash"`
	CreatedAt      time.Time `json:"created_at"`
}

type ReviewReport struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	RepoID     uint       `json:"repo_id"`
	Repo       Repository `gorm:"foreignKey:RepoID" json:"repo"`
	BaseCommit string     `gorm:"not null" json:"base_commit"`
	HeadCommit string     `gorm:"not null" json:"head_commit"`
	Status     string     `gorm:"default:pending" json:"status"` // pending, success, failed
	AISummary  string     `json:"ai_summary"`
	ReportPath string     `json:"report_path"`
	CreatedAt  time.Time  `json:"created_at"`
}

type KeyIssue struct {
	ID         uint         `gorm:"primaryKey" json:"id"`
	RepoID     uint         `json:"repo_id"`
	ReportID   uint         `json:"report_id"`
	Repo       Repository   `gorm:"foreignKey:RepoID" json:"repo"`
	Report     ReviewReport `gorm:"foreignKey:ReportID" json:"report"`
	IssueType  string       `gorm:"not null" json:"issue_type"` // multithreading, lock, memory_leak, library
	Title      string       `gorm:"not null" json:"title"`
	FilePath   string       `json:"file_path"`
	LineNumber int          `json:"line_number"`
	Status     string       `gorm:"default:open" json:"status"` // open, in_progress, resolved
	AssigneeID string       `json:"assignee_id"`
	Assignee   Member       `gorm:"foreignKey:AssigneeID" json:"assignee"`
	CreatedAt  time.Time    `json:"created_at"`
}

type SystemConfig struct {
	ID         uint `gorm:"primaryKey" json:"id"` // Always 1
	AutoNotify bool `gorm:"default:false" json:"auto_notify"`
}
