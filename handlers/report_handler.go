package handlers

import (
	"code-shield/models"
	"code-shield/services/reports"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// GetReportSummaryHandler 获取任务报告轻量概览
func GetReportSummaryHandler(c *gin.Context) {
	idStr := c.Param("id")
	taskID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid task report ID"})
		return
	}

	summary, err := reports.GetReportSummary(uint(taskID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, summary)
}

// GetReportFindingsHandler 获取按需分页过滤的问题清单
func GetReportFindingsHandler(c *gin.Context) {
	idStr := c.Param("id")
	taskID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid task report ID"})
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "50"))
	if pageSize == 0 {
		pageSize, _ = strconv.Atoi(c.DefaultQuery("page_size", "50"))
	}

	query := reports.FindingsQuery{
		Page:       page,
		PageSize:   pageSize,
		Severity:   c.Query("severity"),
		Status:     c.Query("status"),
		Category:   c.Query("category"),
		Keyword:    c.Query("keyword"),
		SortField:  c.Query("sort_field"),
		SortOrder:  c.Query("sort_order"),
		AssigneeID: c.Query("assignee_id"),
	}

	findingsPage, err := reports.GetReportFindings(uint(taskID), query)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, findingsPage)
}

// GetReportDiagnosticsHandler 获取任务运行轨迹与诊断
func GetReportDiagnosticsHandler(c *gin.Context) {
	idStr := c.Param("id")
	taskID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid task report ID"})
		return
	}

	diag, err := reports.GetReportDiagnostics(uint(taskID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, diag)
}

// GetReportAggregateHandler 一站式全量聚合接口
func GetReportAggregateHandler(c *gin.Context) {
	idStr := c.Param("id")
	taskID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid task report ID"})
		return
	}

	agg, err := reports.GetReportAggregate(uint(taskID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, agg)
}

// ExportReportHandler 统一多格式导出分发入口
func ExportReportHandler(c *gin.Context) {
	idStr := c.Param("id")
	taskID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid task report ID"})
		return
	}

	report, err := reports.GetTaskReportEntity(uint(taskID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task report not found"})
		return
	}

	format := strings.ToLower(strings.TrimSpace(c.DefaultQuery("format", "excel")))
	scope := strings.ToLower(strings.TrimSpace(c.DefaultQuery("scope", "findings")))

	var exporter reports.ReportExporter
	var categoryName string

	switch format {
	case "excel", "xlsx":
		exporter = &reports.ExcelExporter{IsCSV: false}
		categoryName = "findings"
	case "csv":
		exporter = &reports.ExcelExporter{IsCSV: true}
		categoryName = "findings"
	case "json":
		if scope == "reconcile" {
			exporter = &reports.ReconciliationExporter{}
		} else {
			exporter = &reports.JSONExporter{}
		}
		categoryName = scope
	case "md", "markdown":
		exporter = &reports.MarkdownExporter{}
		categoryName = "summary"
	case "zip", "archive":
		exporter = &reports.ArchiveExporter{}
		categoryName = "all-in-one"
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unsupported export format: " + format})
		return
	}

	filename := reports.BuildExportFilename(report, categoryName, exporter.FileExtension())
	encodedFilename := url.PathEscape(filename)

	c.Header("Content-Type", exporter.ContentType())
	// RFC 5987 标准 Content-Disposition (ASCII 兜底 + UTF-8 真实多语言文件名)
	fallbackFilename := "report" + exporter.FileExtension()
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"; filename*=UTF-8''%s", fallbackFilename, encodedFilename))

	opts := reports.ExportOptions{
		Format: format,
		Scope:  scope,
	}

	if err := exporter.Export(report, c.Writer, opts); err != nil {
		// 响应头已发送时不能再次 JSON 写入，记日志
		fmt.Printf("[ExportReportHandler] Failed to export report %d: %v\n", taskID, err)
		return
	}
}

// GetReportReconciliationHandler 获取指定任务报告的跨轮对账详情（对齐前端 ScanReconciliationInfo 契约并计算漏斗统计）
func GetReportReconciliationHandler(c *gin.Context) {
	idStr := c.Param("id")
	taskID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid task report ID"})
		return
	}

	var recon models.ScanReconciliation
	if err := models.DB.Where("current_report_id = ?", taskID).Order("id desc").First(&recon).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "No reconciliation session found for this report"})
		return
	}

	var links []models.ReconciliationLink
	_ = models.DB.Where("recon_id = ?", recon.ID).Find(&links).Error

	// 回退：若 DB 中未检索到 links，尝试从落盘的 recon-*.json 读取
	if len(links) == 0 {
		var currReport models.TaskReport
		if err := models.DB.First(&currReport, taskID).Error; err == nil {
			reportDir := currReport.GetReportDir()
			matches, _ := filepath.Glob(filepath.Join(reportDir, fmt.Sprintf("recon-%d-vs-*.json", taskID)))
			if len(matches) > 0 {
				if b, readErr := os.ReadFile(matches[0]); readErr == nil {
					var diffPayload struct {
						Links []struct {
							BaseFP         string  `json:"base_fp"`
							CurrentFP      string  `json:"current_fp"`
							BaseItemUID    string  `json:"base_item_uid"`
							CurrentItemUID string  `json:"current_item_uid"`
							Relation       string  `json:"relation"`
							MatchedTier    int     `json:"matched_tier"`
							Confidence     float64 `json:"confidence"`
							File           string  `json:"file"`
							Reason         string  `json:"reason"`
							SeverityRange  string  `json:"severity_range"`
						} `json:"links"`
					}
					if err := json.Unmarshal(b, &diffPayload); err == nil && len(diffPayload.Links) > 0 {
						for _, dl := range diffPayload.Links {
							links = append(links, models.ReconciliationLink{
								ReconID:        recon.ID,
								BaseFP:         dl.BaseFP,
								CurrentFP:      dl.CurrentFP,
								BaseItemUID:    dl.BaseItemUID,
								CurrentItemUID: dl.CurrentItemUID,
								Relation:       dl.Relation,
								MatchedTier:    dl.MatchedTier,
								Confidence:     dl.Confidence,
								Reason:         dl.Reason,
								SeverityRange:  dl.SeverityRange,
							})
						}
					}
				}
			}
		}
	}

	funnelStats := map[string]int{
		"R1_STRONG_PHYSICAL_FP":    0,
		"R2_DETERMINISTIC_FEATURE": 0,
		"R3_GEOMETRIC_SEMANTIC":    0,
		"R4_MULTI_VIEW_MERGE":      0,
		"R5_RESIDUAL_ALIGNMENT":    0,
		"R6_TEMPLATE_FAMILY":       0,
	}

	reconciliationLinks := make([]gin.H, len(links))
	matchedCount := 0
	for i, l := range links {
		ruleName := ""
		switch l.MatchedTier {
		case 1:
			ruleName = "R1 强物理指纹"
			funnelStats["R1_STRONG_PHYSICAL_FP"]++
		case 2:
			ruleName = "R2 确定性特征几何"
			funnelStats["R2_DETERMINISTIC_FEATURE"]++
		case 3:
			ruleName = "R3 几何平移与语义"
			funnelStats["R3_GEOMETRIC_SEMANTIC"]++
		case 4:
			ruleName = "R4 多视角诊断合并"
			funnelStats["R4_MULTI_VIEW_MERGE"]++
		case 5:
			ruleName = "R5 单文件残差对齐"
			funnelStats["R5_RESIDUAL_ALIGNMENT"]++
		case 6:
			ruleName = "R6 跨文件模板族"
			funnelStats["R6_TEMPLATE_FAMILY"]++
		default:
			if l.Relation == "SAME_MULTI_VIEW" {
				ruleName = "R4 多视角诊断合并"
				funnelStats["R4_MULTI_VIEW_MERGE"]++
			} else if l.Relation == "TEMPLATE" {
				ruleName = "R6 跨文件模板族"
				funnelStats["R6_TEMPLATE_FAMILY"]++
			} else {
				ruleName = "规则匹配"
			}
		}

		if l.Relation != "NEW" && l.Relation != "VANISHED" {
			matchedCount++
		}

		reconciliationLinks[i] = gin.H{
			"id":              l.ID,
			"base_record_id":  l.BaseItemUID,
			"curr_finding_id": l.CurrentItemUID,
			"match_rule":      ruleName,
			"relation":        l.Relation,
			"severity_range":  l.SeverityRange,
			"confidence":      l.Confidence,
			"rationale":       l.Reason,
			"confirmed":       l.Confirmed,
		}
	}

	if recon.MultiViewMerged > 0 && funnelStats["R4_MULTI_VIEW_MERGE"] == 0 {
		funnelStats["R4_MULTI_VIEW_MERGE"] = recon.MultiViewMerged
	}
	if recon.TemplateFamilyCount > 0 && funnelStats["R6_TEMPLATE_FAMILY"] == 0 {
		funnelStats["R6_TEMPLATE_FAMILY"] = recon.TemplateFamilyCount
	}

	if matchedCount == 0 {
		matchedCount = recon.ExistedCount + recon.MultiViewMerged
	}

	totalCurrent := recon.ActiveWorkingCount
	if totalCurrent == 0 {
		totalCurrent = recon.NewCount + recon.ExistedCount
	}

	totalBaseline := recon.ExistedCount + recon.VanishedCoverageGap + recon.VanishedFixCandidate
	resolvedCount := recon.VanishedFixCandidate + recon.ResolvedByChangeCount

	c.JSON(http.StatusOK, gin.H{
		"id":                   recon.ID,
		"repo_id":              recon.RepoID,
		"task_report_id":       recon.CurrentReportID,
		"baseline_report_id":   recon.BaseReportID,
		"governance_mode":      recon.GovernanceMode,
		"total_current":        totalCurrent,
		"total_baseline":       totalBaseline,
		"matched_count":        matchedCount,
		"new_count":            recon.NewCount,
		"existed_count":        recon.ExistedCount,
		"resolved_count":       resolvedCount,
		"gap_filled_count":     recon.VanishedCoverageGap,
		"archived_count":       recon.DormantArchivedCount,
		"multi_view_count":     recon.MultiViewMerged,
		"family_count":         recon.TemplateFamilyCount,
		"funnel_stats":         funnelStats,
		"reconciliation_links": reconciliationLinks,
		"created_at":           recon.CreatedAt.Format(time.RFC3339),
		// 保留原结构兼容
		"session": recon,
		"links":   links,
	})
}
