package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"code-shield/models"
	"code-shield/services/defectlifecycle"

	"github.com/gin-gonic/gin"
)

func GetReportReconciliationHandler(c *gin.Context) {
	reportID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid task report ID"})
		return
	}
	result, err := defectlifecycle.GetReportReconciliation(models.DB, uint(reportID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func GetObservationCandidatesHandler(c *gin.Context) {
	reportID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid task report ID"})
		return
	}
	candidates, err := defectlifecycle.GetObservationCandidates(models.DB, uint(reportID), c.Param("group_uid"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": candidates})
}

func ListDefectsHandler(c *gin.Context) {
	query := defectlifecycle.DefectQuery{
		Page:     parseIntDefault(c.Query("page"), 1),
		PageSize: parseIntDefault(c.Query("pageSize"), 50),
	}
	if value, err := strconv.ParseUint(c.Query("repo_id"), 10, 64); err == nil {
		query.RepoID = uint(value)
	}
	if value, err := strconv.ParseUint(c.Query("task_type_id"), 10, 64); err == nil {
		query.TaskTypeID = uint(value)
	}
	if statuses := c.Query("status"); statuses != "" {
		query.Statuses = strings.Split(statuses, ",")
	}
	page, err := defectlifecycle.ListDefects(models.DB, query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, page)
}

func GetDefectDetailHandler(c *gin.Context) {
	defectID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid defect ID"})
		return
	}
	defect, observations, events, aliases, err := defectlifecycle.GetDefectDetail(models.DB, uint(defectID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"defect": defect, "observations": observations, "events": events, "aliases": aliases,
	})
}

func UpdateDefectAssignmentHandler(c *gin.Context) {
	defectID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid defect ID"})
		return
	}
	actorID, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	var request struct {
		AssigneeID *uint `json:"assignee_id"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := defectlifecycle.AssignDefect(models.DB, uint(defectID), request.AssigneeID, actorID.(uint)); err != nil {
		if errors.Is(err, defectlifecycle.ErrDefectChanged) {
			c.JSON(http.StatusConflict, gin.H{"error": "defect changed; refresh and retry"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("defect %d assignment updated", defectID)})
}

func governanceInput(c *gin.Context) defectlifecycle.GovernanceInput {
	actorID, _ := c.Get("userID")
	input := defectlifecycle.GovernanceInput{RepoRoot: c.GetString("repo_root")}
	if value, ok := actorID.(uint); ok {
		input.ActorID = value
	}
	return input
}

type probableRequest struct {
	RepoRoot string `json:"repo_root"`
	Reason   string `json:"reason"`
}

func ConfirmProbableHandler(c *gin.Context) {
	reportID, observationGroupUID, err := probablePath(c)
	if err != nil {
		return
	}
	var request probableRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input := governanceInput(c)
	input.RepoRoot, input.Reason = request.RepoRoot, request.Reason
	defect, err := defectlifecycle.ConfirmProbable(models.DB, reportID, observationGroupUID, input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, defect)
}

type bindProbableRequest struct {
	RepoRoot string `json:"repo_root"`
	DefectID uint   `json:"defect_id" binding:"required"`
	Reason   string `json:"reason"`
}

func BindProbableHandler(c *gin.Context) {
	reportID, observationGroupUID, err := probablePath(c)
	if err != nil {
		return
	}
	var request bindProbableRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input := governanceInput(c)
	input.RepoRoot, input.Reason = request.RepoRoot, request.Reason
	defect, err := defectlifecycle.BindProbable(models.DB, reportID, observationGroupUID, request.DefectID, input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, defect)
}

func probablePath(c *gin.Context) (uint, string, error) {
	reportValue, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid report ID"})
		return 0, "", err
	}
	return uint(reportValue), c.Param("group_uid"), nil
}

func CreateHumanAliasHandler(c *gin.Context) {
	defectID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid defect ID"})
		return
	}
	var request struct {
		Alias  string `json:"alias" binding:"required"`
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input := governanceInput(c)
	input.Reason = request.Reason
	if err := defectlifecycle.CreateHumanAlias(models.DB, uint(defectID), request.Alias, input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "human alias created"})
}

type mergeDefectRequest struct {
	TargetDefectID uint   `json:"target_defect_id" binding:"required"`
	Reason         string `json:"reason"`
}

func MergeDefectHandler(c *gin.Context) {
	sourceID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid defect ID"})
		return
	}
	var request mergeDefectRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input := governanceInput(c)
	input.Reason = request.Reason
	defect, err := defectlifecycle.MergeDefects(models.DB, uint(sourceID), request.TargetDefectID, input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, defect)
}

type splitDefectRequest struct {
	ReportID uint   `json:"report_id" binding:"required"`
	GroupUID string `json:"observation_group_uid" binding:"required"`
	RepoRoot string `json:"repo_root"`
	Reason   string `json:"reason"`
}

func SplitDefectHandler(c *gin.Context) {
	defectID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid defect ID"})
		return
	}
	var request splitDefectRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	input := governanceInput(c)
	input.RepoRoot, input.Reason = request.RepoRoot, request.Reason
	defect, err := defectlifecycle.SplitDefect(models.DB, uint(defectID), request.ReportID, request.GroupUID, input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, defect)
}

func parseIntDefault(value string, fallback int) int {
	if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
		return parsed
	}
	return fallback
}
