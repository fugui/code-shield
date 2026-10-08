package handlers

import (
	commonAudit "code-common/backend/audit"
	"code-shield/models"
	"code-shield/services"
	"code-shield/services/engines"
	"code-shield/services/engines/profile"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// logTaskTypeFileErr 记录任务类型文件 I/O 错误（清理/落盘类错误不静默忽略）
func logTaskTypeFileErr(action, path string, err error) {
	if err != nil {
		log.Printf("[TaskType] %s failed for %s: %v\n", action, path, err)
	}
}

func decorateScanProfileHash(taskType *models.TaskType) {
	_, profileHash, err := profile.Parse(json.RawMessage(taskType.EngineConfig))
	if err == nil {
		taskType.ScanProfileHash = profileHash
	}
}

// GetDomainFamilies returns all supported domain families and their default defense dimensions
func GetDomainFamilies(c *gin.Context) {
	c.JSON(http.StatusOK, models.GetAllDomainFamilies())
}

// GetTaskTypes returns all task types
func GetTaskTypes(c *gin.Context) {
	var taskTypes []models.TaskType
	query := models.DB.Order("id asc")

	activeOnly := c.Query("active_only")
	if activeOnly == "true" {
		query = query.Where("is_active = ?", true)
	}

	query.Find(&taskTypes)
	for i := range taskTypes {
		decorateScanProfileHash(&taskTypes[i])
	}
	c.JSON(http.StatusOK, taskTypes)
}

// GetTaskType returns a single task type
func GetTaskType(c *gin.Context) {
	id := c.Param("id")
	var taskType models.TaskType
	if err := models.DB.First(&taskType, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task type not found"})
		return
	}
	decorateScanProfileHash(&taskType)
	c.JSON(http.StatusOK, taskType)
}

// CreateTaskType creates a new task type with auto-generated default files
func CreateTaskType(c *gin.Context) {
	var req models.TaskType
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if !engines.EngineExists(req.EngineMode) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 engine_mode: 引擎未注册"})
		return
	}
	if _, _, err := profile.Parse(json.RawMessage(req.EngineConfig)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 engine_config: " + err.Error()})
		return
	}
	canonicalProfile, profileHash, _ := profile.Parse(json.RawMessage(req.EngineConfig))
	canonicalProfileJSON, _ := json.Marshal(canonicalProfile)
	req.EngineConfig = datatypes.JSON(canonicalProfileJSON)

	if req.NotifyTemplate == "" {
		req.NotifyTemplate = "【Code-Shield】{{.RepoName}} {{.TaskDisplayName}}报告"
	}
	if req.DomainFamily == "" {
		req.DomainFamily = models.DomainFamilyComprehensive
	}

	if req.IsCampaign {
		if req.GovernanceMode == "" {
			req.GovernanceMode = models.GovernanceModeFullLedger
		}
		if req.GovernanceMode != models.GovernanceModeFullLedger && req.GovernanceMode != models.GovernanceModeChangeFocus && req.GovernanceMode != models.GovernanceModeEntityAssessment {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的治理模式: 仅支持 full_ledger、change_focus 或 entity_assessment"})
			return
		}
		if req.CampaignPath != "" {
			var count int64
			models.DB.Model(&models.TaskType{}).Where("is_campaign = ? AND campaign_path = ?", true, req.CampaignPath).Count(&count)
			if count > 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("专项分析路由路径 '%s' 已被其他任务类型占用", req.CampaignPath)})
				return
			}
		}
	}

	if err := models.DB.Create(&req).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create task type: " + err.Error()})
		return
	}

	// Create default files on disk using conventional paths
	absTaskDir := models.AppConfig.GetAbsPath(req.TaskDir())
	logTaskTypeFileErr("create task dir", absTaskDir, os.MkdirAll(absTaskDir, 0755))

	defaultAnalysisPrompt := "# " + req.DisplayName + " 分析指令\n\n" +
		"你是一个软件开发经验非常丰富的顶级技术专家与安全审计专家。请对当前代码仓进行 " + req.DisplayName + " 专项分析任务。\n\n" +
		"## 要求\n\n" +
		"1. 深入分析代码中与 " + req.DisplayName + " 相关的潜在安全漏洞与质量缺陷。\n" +
		"2. 排除测试代码，仅对业务核心代码进行分析。\n" +
		"3. 仅报告确实存在或极大概率触发缺陷的代码，拒绝虚报。\n" +
		"4. 必须精准指出问题发生的行号（使用字符串表示，支持单行如 \"42\" 或范围如 \"42-50\"），截取 3-10 行核心代码段。\n\n" +
		"## 输出格式约束\n\n" +
		"必须直接输出纯 JSON 字符串。绝对不得包含 ```json ... ``` 等 Markdown 代码块标记，并且 findings 字段必须符合规范。\n\n" +
		"```json\n" +
		"{\n" +
		"  \"findings\": [\n" +
		"    {\n" +
		"      \"severity\": \"致命|严重|一般|建议\",\n" +
		"      \"category\": \"问题分类-具体子问题\",\n" +
		"      \"file_path\": \"src/example.cpp\",\n" +
		"      \"line_number\": \"42-45\",\n" +
		"      \"code_snippet\": \"原始核心代码片段（3-10行）\",\n" +
		"      \"title\": \"问题简述（一句话概括）\",\n" +
		"      \"detail\": \"详细描述风险触发路径与缺陷逻辑\",\n" +
		"      \"suggestion\": \"具体的代码修复建议与最佳实践\"\n" +
		"    }\n" +
		"  ],\n" +
		"  \"summary\": \"200-300字的整体评估摘要，描述主要隐患及其风险影响\"\n" +
		"}\n" +
		"```\n"
	defaultSynthesisPrompt := "# " + req.DisplayName + " 综合报告生成指令\n\n" +
		"你将收到一份 JSON 格式的分析发现清单（基于 @analysis_prompt.md 提示词进行的分析）。请基于这些发现，生成一份指导开发者进行安全修复的综合评估报告。\n\n" +
		"## 输出格式要求\n\n" +
		"请使用简体中文，以 Markdown 格式输出，内容包括\"概要\"、\"问题清单\"、\"总结与建议\"三部分组成。\n\n" +
		"### 一、概要\n" +
		"- 介绍本次审查的目的与主要方法。\n" +
		"- 问题总数： 3 个\n\n" +
		"> ⚠️ **概要部分的最后一行，格式约束（机器解析，不得违反）**： 必须严格使用 `问题总数： N 个` 的格式，关键字和标点均不得更改， N 为非负整数，无则填 `0`。\n\n" +
		"### 二、问题清单\n" +
		"- 按照严重程度从高到低对风险列表进行整理排版。\n" +
		"- 给出文件定位、风险分类、严重级别、原始代码片段以及具体可靠的修复代码方案。\n\n" +
		"### 三、总结与建议\n" +
		"- 一个简洁的问题缺陷总结和预防改进指导。\n"
	logTaskTypeFileErr("write analysis prompt", req.AnalysisPromptFile(),
		os.WriteFile(models.AppConfig.GetAbsPath(req.AnalysisPromptFile()), []byte(defaultAnalysisPrompt), 0644))
	logTaskTypeFileErr("write synthesis prompt", req.SynthesisPromptFile(),
		os.WriteFile(models.AppConfig.GetAbsPath(req.SynthesisPromptFile()), []byte(defaultSynthesisPrompt), 0644))

	// 写入 meta.json
	if metaBytes, err := json.MarshalIndent(req, "", "  "); err != nil {
		logTaskTypeFileErr("marshal meta.json", absTaskDir, err)
	} else {
		logTaskTypeFileErr("write meta.json", filepath.Join(absTaskDir, "meta.json"),
			os.WriteFile(filepath.Join(absTaskDir, "meta.json"), metaBytes, 0644))
	}

	// 主动失效专项分析内存缓存
	InvalidateCampaignCache(req.CampaignPath, req.Name)

	commonAudit.SetAuditContext(c, "task_type", "create", models.AuditLevelP0,
		fmt.Sprintf("创建了任务类型: %s (%s)", req.DisplayName, req.Name),
		"task_type", fmt.Sprintf("%d", req.ID), req.DisplayName,
		nil, req)

	req.ScanProfileHash = profileHash
	c.JSON(http.StatusCreated, req)
}

// UpdateTaskType updates an existing task type
func UpdateTaskType(c *gin.Context) {
	id := c.Param("id")
	var taskType models.TaskType
	if err := models.DB.First(&taskType, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task type not found"})
		return
	}

	oldTaskType := taskType

	var req struct {
		DisplayName       *string          `json:"display_name"`
		Description       *string          `json:"description"`
		EngineMode        *string          `json:"engine_mode"`
		EngineConfig      *json.RawMessage `json:"engine_config"`
		AssessmentConfig  *json.RawMessage `json:"assessment_config"`
		TargetScope       *string          `json:"target_scope"`
		NotifyTemplate    *string          `json:"notify_template"`
		NotifyThreshold   *int             `json:"notify_threshold"`
		NotifyCc          *json.RawMessage `json:"notify_cc"`
		Timeout           *int             `json:"timeout"`
		IsActive          *bool            `json:"is_active"`
		IsCampaign        *bool            `json:"is_campaign"`
		CampaignPath      *string          `json:"campaign_path"`
		GovernanceMode    *string          `json:"governance_mode"`
		CampaignIcon      *string          `json:"campaign_icon"`
		CampaignConfig    *json.RawMessage `json:"campaign_config"`
		DomainFamily      *string          `json:"domain_family"`
		DefenseDimensions *json.RawMessage `json:"defense_dimensions"`
		DomainLabel       *string          `json:"domain_label"`
		TargetSemantics   *json.RawMessage `json:"target_semantics"`
		DisplaySemantics  *json.RawMessage `json:"display_semantics"`
		Categories        *json.RawMessage `json:"categories"`
		UpdatedAt         *time.Time       `json:"updated_at"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.EngineMode != nil && !engines.EngineExists(*req.EngineMode) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 engine_mode: 引擎未注册"})
		return
	}
	var profileHash string
	if req.EngineConfig != nil {
		canonicalProfile, parsedHash, parseErr := profile.Parse(*req.EngineConfig)
		if parseErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 engine_config: " + parseErr.Error()})
			return
		}
		canonicalProfileJSON, _ := json.Marshal(canonicalProfile)
		canonicalRaw := json.RawMessage(canonicalProfileJSON)
		req.EngineConfig = &canonicalRaw
		profileHash = parsedHash
	}

	updates := map[string]interface{}{}
	if req.DisplayName != nil {
		updates["display_name"] = *req.DisplayName
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}
	if req.EngineMode != nil {
		updates["engine_mode"] = *req.EngineMode
		updates["current_revision_id"] = nil
	}
	if req.EngineConfig != nil {
		updates["engine_config"] = string(*req.EngineConfig)
		updates["current_revision_id"] = nil
	}
	if req.AssessmentConfig != nil {
		updates["assessment_config"] = string(*req.AssessmentConfig)
		updates["assessment_config_hash"] = models.HashAssessmentConfig(datatypes.JSON(*req.AssessmentConfig))
		updates["current_revision_id"] = nil
	}
	if req.TargetScope != nil {
		updates["target_scope"] = *req.TargetScope
	}
	if req.NotifyTemplate != nil {
		updates["notify_template"] = *req.NotifyTemplate
	}
	if req.NotifyThreshold != nil {
		updates["notify_threshold"] = *req.NotifyThreshold
	}
	if req.NotifyCc != nil {
		updates["notify_cc"] = string(*req.NotifyCc)
	}
	if req.Timeout != nil {
		updates["timeout"] = *req.Timeout
	}
	if req.IsActive != nil {
		updates["is_active"] = *req.IsActive
	}
	if req.IsCampaign != nil {
		updates["is_campaign"] = *req.IsCampaign
	}
	if req.CampaignPath != nil {
		updates["campaign_path"] = *req.CampaignPath
	}
	if req.GovernanceMode != nil {
		updates["governance_mode"] = *req.GovernanceMode
		updates["current_revision_id"] = nil
	}
	if req.CampaignIcon != nil {
		updates["campaign_icon"] = *req.CampaignIcon
	}
	if req.CampaignConfig != nil {
		updates["campaign_config"] = string(*req.CampaignConfig)
	}
	if req.DomainFamily != nil {
		updates["domain_family"] = *req.DomainFamily
		updates["current_revision_id"] = nil
	}
	if req.DefenseDimensions != nil {
		updates["defense_dimensions"] = string(*req.DefenseDimensions)
		updates["current_revision_id"] = nil
	}
	if req.DomainLabel != nil {
		updates["domain_label"] = *req.DomainLabel
		updates["current_revision_id"] = nil
	}
	if req.TargetSemantics != nil {
		updates["target_semantics"] = string(*req.TargetSemantics)
		updates["current_revision_id"] = nil
	}
	if req.DisplaySemantics != nil {
		updates["display_semantics"] = string(*req.DisplaySemantics)
		updates["current_revision_id"] = nil
	}
	if req.Categories != nil {
		updates["categories"] = string(*req.Categories)
		updates["current_revision_id"] = nil
	}

	if req.UpdatedAt != nil && !taskType.UpdatedAt.Equal(*req.UpdatedAt) {
		c.JSON(http.StatusConflict, gin.H{"error": "任务类型已被其他修改更新，请刷新后重试"})
		return
	}

	targetIsCampaign := taskType.IsCampaign
	if req.IsCampaign != nil {
		targetIsCampaign = *req.IsCampaign
	}
	targetCampaignPath := taskType.CampaignPath
	if req.CampaignPath != nil {
		targetCampaignPath = *req.CampaignPath
	}
	targetGovernanceMode := taskType.GovernanceMode
	if req.GovernanceMode != nil {
		targetGovernanceMode = *req.GovernanceMode
	}

	if targetIsCampaign {
		if targetGovernanceMode != models.GovernanceModeFullLedger && targetGovernanceMode != models.GovernanceModeChangeFocus && targetGovernanceMode != models.GovernanceModeEntityAssessment {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的治理模式: 仅支持 full_ledger、change_focus 或 entity_assessment"})
			return
		}
		if targetCampaignPath != "" {
			var count int64
			models.DB.Model(&models.TaskType{}).Where("id != ? AND is_campaign = ? AND campaign_path = ?", taskType.ID, true, targetCampaignPath).Count(&count)
			if count > 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("专项分析路由路径 '%s' 已被其他任务类型占用", targetCampaignPath)})
				return
			}
		}
	}

	if err := models.DB.Model(&taskType).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update task type: " + err.Error()})
		return
	}

	models.DB.First(&taskType, id)
	taskType.ScanProfileHash = profileHash

	// 主动失效专项分析内存缓存
	InvalidateCampaignCache(oldTaskType.CampaignPath, oldTaskType.Name, taskType.CampaignPath, taskType.Name)

	// 将最新元数据重写回磁盘的 meta.json
	absTaskDir := models.AppConfig.GetAbsPath(taskType.TaskDir())
	logTaskTypeFileErr("create task dir", absTaskDir, os.MkdirAll(absTaskDir, 0755))
	if metaBytes, err := json.MarshalIndent(taskType, "", "  "); err != nil {
		logTaskTypeFileErr("marshal meta.json", absTaskDir, err)
	} else {
		logTaskTypeFileErr("write meta.json", filepath.Join(absTaskDir, "meta.json"),
			os.WriteFile(filepath.Join(absTaskDir, "meta.json"), metaBytes, 0644))
	}

	commonAudit.SetAuditContext(c, "task_type", "update", models.AuditLevelP0,
		fmt.Sprintf("修改了任务类型: %s (%s)", taskType.DisplayName, taskType.Name),
		"task_type", fmt.Sprintf("%d", taskType.ID), taskType.DisplayName,
		oldTaskType, taskType)

	c.JSON(http.StatusOK, taskType)
}

// DeleteTaskType deletes a task type and cleans up associated reports, schedules, and memory records
func DeleteTaskType(c *gin.Context) {
	id := c.Param("id")
	var taskType models.TaskType
	if err := models.DB.First(&taskType, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task type not found"})
		return
	}

	// 开启事务级联清理所有关联实体
	err := models.DB.Transaction(func(tx *gorm.DB) error {
		// 1. 获取关联的全部 TaskReport ID
		var reportIDs []uint
		if err := tx.Model(&models.TaskReport{}).Where("task_type_id = ?", taskType.ID).Pluck("id", &reportIDs).Error; err != nil {
			return err
		}

		if len(reportIDs) > 0 {
			// 清理关联的 AnalysisFinding
			if err := tx.Where("task_report_id IN ?", reportIDs).Delete(&models.AnalysisFinding{}).Error; err != nil {
				return err
			}
			// 清理关联的 TaskDebateLog
			if err := tx.Where("task_report_id IN ?", reportIDs).Delete(&models.TaskDebateLog{}).Error; err != nil {
				return err
			}
			// 清理关联的 KeyIssue
			if err := tx.Where("task_report_id IN ?", reportIDs).Delete(&models.KeyIssue{}).Error; err != nil {
				return err
			}
			// 删除 TaskReport
			if err := tx.Where("id IN ?", reportIDs).Delete(&models.TaskReport{}).Error; err != nil {
				return err
			}
		}

		// 2. 清理关联的定时任务与触发日志
		if err := tx.Where("task_type_id = ?", taskType.ID).Delete(&models.ScheduleConfig{}).Error; err != nil {
			return err
		}
		if err := tx.Where("task_type_id = ?", taskType.ID).Delete(&models.TaskTriggerLog{}).Error; err != nil {
			return err
		}
		if err := tx.Where("task_type_id = ?", taskType.ID).Delete(&models.TaskExecutionLog{}).Error; err != nil {
			return err
		}

		// 3. 清理专项分析历史数据
		if err := tx.Where("task_type_id = ?", taskType.ID).Delete(&models.CampaignFinding{}).Error; err != nil {
			return err
		}

		// 4. 清理人机负样本规则
		if err := tx.Where("task_type_id = ?", taskType.ID).Delete(&models.RepoFeedbackRule{}).Error; err != nil {
			return err
		}

		// 5. 清理不可变修订
		if err := tx.Where("task_type_id = ?", taskType.ID).Delete(&models.TaskTypeRevision{}).Error; err != nil {
			return err
		}

		// 6. 删除 TaskType 自身
		if err := tx.Delete(&taskType).Error; err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete task type: " + err.Error()})
		return
	}

	// 主动失效专项分析内存缓存
	InvalidateCampaignCache(taskType.CampaignPath, taskType.Name)

	// 物理删除磁盘文件夹，防止重启后再次被扫出装载
	absTaskDir := models.AppConfig.GetAbsPath(taskType.TaskDir())
	logTaskTypeFileErr("remove task dir", absTaskDir, os.RemoveAll(absTaskDir))

	commonAudit.SetAuditContext(c, "task_type", "delete", models.AuditLevelP0,
		fmt.Sprintf("删除了任务类型: %s (%s)", taskType.DisplayName, taskType.Name),
		"task_type", fmt.Sprintf("%d", taskType.ID), taskType.DisplayName,
		taskType, nil)

	c.JSON(http.StatusOK, gin.H{"message": "Task type deleted"})
}

// GetTaskTypeFiles returns the content of the 4 conventional files
func GetTaskTypeFiles(c *gin.Context) {
	id := c.Param("id")
	var taskType models.TaskType
	if err := models.DB.First(&taskType, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task type not found"})
		return
	}

	readFile := func(path string) string {
		absPath := models.AppConfig.GetAbsPath(path)
		content, err := os.ReadFile(absPath)
		if err != nil {
			return ""
		}
		return string(content)
	}

	c.JSON(http.StatusOK, gin.H{
		"analysis_prompt":  readFile(taskType.AnalysisPromptFile()),
		"synthesis_prompt": readFile(taskType.SynthesisPromptFile()),
		"postprocess":      readFile(taskType.PostprocessScript()),
	})
}

// UpdateTaskTypeFile writes content to a specific file of a task type
func UpdateTaskTypeFile(c *gin.Context) {
	id := c.Param("id")
	fileType := c.Param("file_type")

	var taskType models.TaskType
	if err := models.DB.First(&taskType, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task type not found"})
		return
	}

	var req struct {
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var filePath string
	switch fileType {
	case "analysis_prompt":
		filePath = taskType.AnalysisPromptFile()
	case "synthesis_prompt":
		filePath = taskType.SynthesisPromptFile()
	case "postprocess":
		filePath = taskType.PostprocessScript()
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid file type, must be: analysis_prompt, synthesis_prompt, or postprocess"})
		return
	}

	absPath := models.AppConfig.GetAbsPath(filePath)
	oldContentBytes, _ := os.ReadFile(absPath)
	oldContent := string(oldContentBytes)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create directory"})
		return
	}

	perm := os.FileMode(0644)
	if fileType == "postprocess" {
		perm = 0755 // scripts need execute permission
	}

	if err := os.WriteFile(absPath, []byte(req.Content), perm); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to write file"})
		return
	}

	commonAudit.SetAuditContext(c, "task_type", "update_file", models.AuditLevelP0,
		fmt.Sprintf("更新了任务类型 [%s] 的提示词文件: %s", taskType.DisplayName, fileType),
		"task_type", fmt.Sprintf("%d", taskType.ID), taskType.DisplayName,
		map[string]string{"file_type": fileType, "content": oldContent},
		map[string]string{"file_type": fileType, "content": req.Content})

	c.JSON(http.StatusOK, gin.H{"message": "文件已保存"})
}

// TriggerAllReposForTaskType triggers the task type for all repositories
func TriggerAllReposForTaskType(c *gin.Context) {
	id := c.Param("id")
	var taskType models.TaskType
	if err := models.DB.First(&taskType, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task type not found"})
		return
	}

	var repos []models.Repository
	if err := models.DB.Find(&repos).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch repositories"})
		return
	}

	opID, opName, clientIP := getOperatorInfo(c)
	batchNo := fmt.Sprintf("TRG-%s-ALL", time.Now().Format("20060102150405"))

	triggerLog := models.TaskTriggerLog{
		TriggerBatch:  batchNo,
		TriggerType:   "manual_batch",
		OperatorID:    opID,
		OperatorName:  opName,
		TaskTypeID:    taskType.ID,
		TargetMode:    "all",
		TargetSummary: fmt.Sprintf("全部代码仓 (共 %d 个)", len(repos)),
		TotalRepos:    len(repos),
		ClientIP:      clientIP,
		CreatedAt:     time.Now(),
	}
	models.DB.Create(&triggerLog)

	var tLogID *uint
	if triggerLog.ID > 0 {
		tLogID = &triggerLog.ID
	}

	count := 0
	for _, repo := range repos {
		services.EnqueueTaskWithTriggerLog(nil, tLogID, repo.ID, repo.URL, taskType.ID, false, "manual", models.RunParams{})
		count++
	}

	commonAudit.SetAuditContext(c, "scan", "trigger", models.AuditLevelP1,
		fmt.Sprintf("触发了任务类型 [%s] 全仓扫描 (覆盖 %d 仓)", taskType.DisplayName, count),
		"task_trigger_log", fmt.Sprintf("%d", triggerLog.ID), triggerLog.TriggerBatch,
		nil, triggerLog)

	c.JSON(http.StatusAccepted, gin.H{
		"message": fmt.Sprintf("已成功触发 %d 个代码仓的 %s 任务", count, taskType.DisplayName),
	})
}
