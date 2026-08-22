package handlers

import (
	"code-shield/services/reports"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

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
		exporter = &reports.JSONExporter{}
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
	// RFC 5987 标准 Content-Disposition
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"; filename*=UTF-8''%s", filename, encodedFilename))

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
