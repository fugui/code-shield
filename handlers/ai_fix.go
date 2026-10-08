package handlers

import (
	"crypto/subtle"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"code-shield/models"
	reportService "code-shield/services/reports"

	"github.com/gin-gonic/gin"
)

const aiFixSchemaVersion = "1.0"

var scpCloneURLPattern = regexp.MustCompile(`^[^/@]+@[^:/]+:`)

type aiFixRepository struct {
	ID            uint   `json:"id"`
	Name          string `json:"name"`
	CloneURL      string `json:"clone_url"`
	WebURL        string `json:"web_url"`
	DefaultBranch string `json:"default_branch"`
}

type aiFixScan struct {
	TaskReportID      uint       `json:"task_report_id"`
	TaskTypeID        uint       `json:"task_type_id"`
	TaskTypeName      string     `json:"task_type_name"`
	GovernanceMode    string     `json:"governance_mode"`
	BaseCommit        string     `json:"base_commit"`
	HeadCommit        *string    `json:"head_commit"`
	CoverageState     string     `json:"coverage_state"`
	LedgerCommittedAt *time.Time `json:"ledger_committed_at"`
	ReportURL         string     `json:"report_url"`
}

type aiFixDefect struct {
	ID                   uint   `json:"id"`
	RepoID               uint   `json:"repo_id"`
	TaskTypeID           uint   `json:"task_type_id"`
	Status               string `json:"status"`
	StatusReason         string `json:"status_reason"`
	IdentityKind         string `json:"identity_kind"`
	CanonicalFingerprint string `json:"canonical_fingerprint"`
	NormPath             string `json:"norm_path"`
	ScopeKey             string `json:"scope_key"`
	SymbolPath           string `json:"symbol_path"`
	StmtShape            string `json:"stmt_shape"`
	CleanToken           string `json:"clean_token"`
	PrevShape            string `json:"prev_shape"`
	NextShape            string `json:"next_shape"`
	OccurrenceIndex      int    `json:"occurrence_index"`
	DefectClassMajor     string `json:"defect_class_major"`
	Severity             string `json:"severity"`
	LineStart            *int   `json:"line_start"`
	LineEnd              *int   `json:"line_end"`
	FirstReportID        uint   `json:"first_report_id"`
	LastSeenReportID     uint   `json:"last_seen_report_id"`
	LastMatchedReportID  uint   `json:"last_matched_report_id"`
	MissedCount          int    `json:"missed_count"`
	DormantRounds        int    `json:"dormant_rounds"`
	HumanLocked          bool   `json:"human_locked"`
	HumanDecision        string `json:"human_decision"`
	MergedIntoID         *uint  `json:"merged_into_id"`
}

type aiFixFinding struct {
	ID                  uint      `json:"id"`
	TaskReportID        uint      `json:"task_report_id"`
	ObservationGroupUID string    `json:"observation_group_uid"`
	Title               string    `json:"title"`
	Severity            string    `json:"severity"`
	SeverityDisplay     string    `json:"severity_display"`
	Category            string    `json:"category"`
	CategoryCode        string    `json:"category_code"`
	FilePath            string    `json:"file_path"`
	LineNumber          string    `json:"line_number"`
	ScopeSymbol         string    `json:"scope_symbol"`
	Detail              string    `json:"detail"`
	CodeSnippet         string    `json:"code_snippet"`
	Suggestion          string    `json:"suggestion"`
	HunterClaim         string    `json:"hunter_claim"`
	ChallengerArg       string    `json:"challenger_arg"`
	JudgeVerdict        string    `json:"judge_verdict"`
	AnchorConfidence    string    `json:"anchor_confidence"`
	CreatedAt           time.Time `json:"created_at"`
}

type aiFixSourceFinding struct {
	ID                  uint   `json:"id"`
	TaskReportID        uint   `json:"task_report_id"`
	ObservationGroupUID string `json:"observation_group_uid"`
}

type aiFixContext struct {
	SchemaVersion  string               `json:"schema_version"`
	GeneratedAt    string               `json:"generated_at"`
	Defect         aiFixDefect          `json:"defect"`
	Repository     aiFixRepository      `json:"repository"`
	Scan           aiFixScan            `json:"scan"`
	PrimaryFinding aiFixFinding         `json:"primary_finding"`
	SourceFindings []aiFixSourceFinding `json:"source_findings"`
	Links          map[string]string    `json:"links"`
}

type aiFixError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type aiFixErrorEnvelope struct {
	Error   aiFixError `json:"error"`
	TraceID string     `json:"trace_id"`
}

type aiFixRateLimiter struct {
	mu      sync.Mutex
	entries map[string][]time.Time
}

var aiFixRequests = &aiFixRateLimiter{entries: make(map[string][]time.Time)}

func writeAIFixError(c *gin.Context, status int, code string, message string) {
	traceID := sanitizeAIFixTraceID(c.GetHeader("X-Trace-ID"))
	c.JSON(status, aiFixErrorEnvelope{
		Error:   aiFixError{Code: code, Message: message},
		TraceID: traceID,
	})
}

func sanitizeAIFixTraceID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return ""
	}
	for _, char := range value {
		if char < 0x20 || char == 0x7f {
			return ""
		}
	}
	return value
}

func authorizeAIFix(c *gin.Context) bool {
	expected := models.AppConfig.AIFix.ContextToken
	authorization := c.GetHeader("Authorization")
	const prefix = "Bearer "
	if expected == "" || len(authorization) <= len(prefix) ||
		!strings.EqualFold(authorization[:len(prefix)], prefix) {
		writeAIFixError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication is required")
		return false
	}
	token := authorization[len(prefix):]
	if subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
		writeAIFixError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication is required")
		return false
	}
	return true
}

func (limiter *aiFixRateLimiter) allow(key string, limit int, window time.Duration) bool {
	now := time.Now()
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	timestamps := limiter.entries[key]
	valid := timestamps[:0]
	for _, timestamp := range timestamps {
		if now.Sub(timestamp) < window {
			valid = append(valid, timestamp)
		}
	}
	if len(valid) >= limit {
		limiter.entries[key] = valid
		return false
	}
	limiter.entries[key] = append(valid, now)
	return true
}

func sanitizeCloneURL(preferred string, fallback string) (string, error) {
	raw := strings.TrimSpace(preferred)
	if raw == "" {
		raw = strings.TrimSpace(fallback)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty clone URL")
	}
	if !strings.Contains(raw, "://") && scpCloneURLPattern.MatchString(raw) {
		return raw, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid clone URL")
	}
	parsed.User = nil
	query := parsed.Query()
	for _, key := range []string{"access_token", "private_token", "token", "api_key"} {
		query.Del(key)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func buildAIFixFinding(finding models.AnalysisFinding) aiFixFinding {
	return aiFixFinding{
		ID:                  finding.ID,
		TaskReportID:        finding.TaskReportID,
		ObservationGroupUID: finding.ObservationGroupUID,
		Title:               finding.Title,
		Severity:            finding.Severity,
		SeverityDisplay:     reportService.GetSeverityChinese(finding.Severity),
		Category:            finding.Category,
		CategoryCode:        finding.CategoryCode,
		FilePath:            finding.FilePath,
		LineNumber:          finding.LineNumber,
		ScopeSymbol:         finding.ScopeSymbol,
		Detail:              finding.Detail,
		CodeSnippet:         finding.CodeSnippet,
		Suggestion:          finding.Suggestion,
		HunterClaim:         finding.HunterClaim,
		ChallengerArg:       finding.ChallengerArg,
		JudgeVerdict:        finding.JudgeVerdict,
		AnchorConfidence:    finding.AnchorConfidence,
		CreatedAt:           finding.CreatedAt,
	}
}

func GetAIFixDefectContext(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if !authorizeAIFix(c) {
		return
	}

	traceID := sanitizeAIFixTraceID(c.GetHeader("X-Trace-ID"))
	if !aiFixRequests.allow("service", 60, time.Minute) {
		writeAIFixError(c, http.StatusTooManyRequests, "RATE_LIMITED", "Too many requests")
		log.Printf("[AI-Fix] defect_id=%s trace_id=%q status=%d", c.Param("defect_id"), traceID, http.StatusTooManyRequests)
		return
	}

	status := http.StatusOK
	defer func() {
		log.Printf("[AI-Fix] defect_id=%s trace_id=%q status=%d", c.Param("defect_id"), traceID, status)
	}()
	defectID, err := strconv.ParseUint(c.Param("defect_id"), 10, 64)
	if err != nil || defectID == 0 || strconv.FormatUint(defectID, 10) != c.Param("defect_id") {
		status = http.StatusBadRequest
		writeAIFixError(c, status, "INVALID_DEFECT_ID", "defect_id must be a positive decimal integer")
		return
	}

	var defect models.Defect
	if err := models.DB.First(&defect, defectID).Error; err != nil {
		status = http.StatusNotFound
		writeAIFixError(c, status, "DEFECT_NOT_FOUND", "Defect not found")
		return
	}
	switch defect.Status {
	case "MERGED":
		status = http.StatusConflict
		writeAIFixError(c, status, "DEFECT_MERGED", "Defect has been merged")
		return
	case "ACTIVE", "COVERAGE_GAP", "VERIFIED_PENDING", "DORMANT":
	default:
		status = http.StatusConflict
		writeAIFixError(c, status, "DEFECT_NOT_FIXABLE", "Defect status does not support AI repair")
		return
	}

	reportID := defect.LastMatchedReportID
	if reportID == 0 {
		reportID = defect.LastSeenReportID
	}
	var report models.TaskReport
	if err := models.DB.Preload("Repo").Preload("TaskType").First(&report, reportID).Error; err != nil {
		status = http.StatusNotFound
		writeAIFixError(c, status, "SOURCE_CONTEXT_NOT_FOUND", "Defect source context was not found")
		return
	}
	if report.RepoID != defect.RepoID || report.TaskTypeID != defect.TaskTypeID {
		status = http.StatusNotFound
		writeAIFixError(c, status, "SOURCE_CONTEXT_NOT_FOUND", "Defect source context was not found")
		return
	}

	var observation models.DefectObservation
	if err := models.DB.Where("report_id = ? AND defect_id = ?", report.ID, defect.ID).
		First(&observation).Error; err != nil {
		status = http.StatusNotFound
		writeAIFixError(c, status, "SOURCE_CONTEXT_NOT_FOUND", "Defect observation was not found")
		return
	}

	var findings []models.AnalysisFinding
	if err := models.DB.Where("task_report_id = ? AND observation_group_uid = ?", report.ID, observation.ObservationGroupUID).
		Order("id ASC").Find(&findings).Error; err != nil || len(findings) == 0 {
		status = http.StatusNotFound
		writeAIFixError(c, status, "SOURCE_CONTEXT_NOT_FOUND", "Defect finding was not found")
		return
	}

	if !report.Repo.IsActive {
		status = http.StatusUnprocessableEntity
		writeAIFixError(c, status, "REPOSITORY_CONTEXT_INVALID", "Repository is not active")
		return
	}
	cloneURL, err := sanitizeCloneURL(report.Repo.HTTPURL, report.Repo.URL)
	if err != nil {
		status = http.StatusUnprocessableEntity
		writeAIFixError(c, status, "REPOSITORY_CONTEXT_INVALID", "Repository clone URL is invalid")
		return
	}
	sourceFindings := make([]aiFixSourceFinding, 0, len(findings))
	for _, finding := range findings {
		sourceFindings = append(sourceFindings, aiFixSourceFinding{
			ID:                  finding.ID,
			TaskReportID:        finding.TaskReportID,
			ObservationGroupUID: finding.ObservationGroupUID,
		})
	}
	headCommit := report.HeadCommit
	reportURL := strings.TrimRight(models.AppConfig.AIFix.ReportBaseURL, "/") +
		fmt.Sprintf("/shield/reports?taskId=%d&tab=findings&findingId=%d#finding-%d", report.ID, findings[0].ID, findings[0].ID)
	governanceMode := models.ResolveGovernanceMode(report.GovernanceMode)

	response := aiFixContext{
		SchemaVersion: aiFixSchemaVersion,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		Defect:        newAIFixDefect(defect),
		Repository: aiFixRepository{
			ID:            report.Repo.ID,
			Name:          report.Repo.Name,
			CloneURL:      cloneURL,
			WebURL:        cloneURL,
			DefaultBranch: report.Repo.Branch,
		},
		Scan: aiFixScan{
			TaskReportID:      report.ID,
			TaskTypeID:        report.TaskTypeID,
			TaskTypeName:      firstNonEmpty(report.TaskType.DisplayName, report.TaskType.Name),
			GovernanceMode:    governanceMode,
			BaseCommit:        report.BaseCommit,
			HeadCommit:        &headCommit,
			CoverageState:     report.CoverageState,
			LedgerCommittedAt: report.LedgerCommittedAt,
			ReportURL:         reportURL,
		},
		PrimaryFinding: buildAIFixFinding(findings[0]),
		SourceFindings: sourceFindings,
		Links: map[string]string{
			"report":         reportURL,
			"finding_anchor": fmt.Sprintf("finding-%d", findings[0].ID),
		},
	}

	c.JSON(status, response)
}

func newAIFixDefect(defect models.Defect) aiFixDefect {
	return aiFixDefect{
		ID:                   defect.ID,
		RepoID:               defect.RepoID,
		TaskTypeID:           defect.TaskTypeID,
		Status:               defect.Status,
		StatusReason:         defect.StatusReason,
		IdentityKind:         defect.IdentityKind,
		CanonicalFingerprint: defect.CanonicalFingerprint,
		NormPath:             defect.NormPath,
		ScopeKey:             defect.ScopeKey,
		SymbolPath:           defect.SymbolPath,
		StmtShape:            defect.StmtShape,
		CleanToken:           defect.CleanToken,
		PrevShape:            defect.PrevShape,
		NextShape:            defect.NextShape,
		OccurrenceIndex:      defect.OccurrenceIndex,
		DefectClassMajor:     defect.DefectClassMajor,
		Severity:             defect.Severity,
		LineStart:            defect.LineStart,
		LineEnd:              defect.LineEnd,
		FirstReportID:        defect.FirstReportID,
		LastSeenReportID:     defect.LastSeenReportID,
		LastMatchedReportID:  defect.LastMatchedReportID,
		MissedCount:          defect.MissedCount,
		DormantRounds:        defect.DormantRounds,
		HumanLocked:          defect.HumanLocked,
		HumanDecision:        defect.HumanDecision,
		MergedIntoID:         defect.MergedIntoID,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
