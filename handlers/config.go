package handlers

import (
	commonAudit "code-common/backend/audit"
	"code-shield/models"
	"code-shield/services"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
)

// ensureConfig exists ensures a row with ID=1 exists in SystemConfig
func ensureConfigExists() {
	var config models.SystemConfig
	res := models.DB.First(&config, 1)
	if res.Error != nil {
		config = models.SystemConfig{ID: 1, AutoNotify: false}
		models.DB.Create(&config)
	}
}

func GetConfig(c *gin.Context) {
	ensureConfigExists()
	var config models.SystemConfig
	models.DB.First(&config, 1)

	info := services.Dispatcher.GetThrottleInfo()

	c.JSON(http.StatusOK, gin.H{
		"id":                config.ID,
		"auto_notify":       config.AutoNotify,
		"queue_paused":      config.QueuePaused,
		"concurrency_scale": info.EffectiveScale,
		"throttle_mode":     info.ThrottleMode,
		"scale_expires_at":  info.ScaleExpiresAt,
		"manual_scale":      info.ManualScale,
		"is_manual":         info.IsManual,
		"is_work_hours":     info.IsWorkHours,
		"work_hours_config": info.WorkHoursConfig,
	})
}

func UpdateConfig(c *gin.Context) {
	ensureConfigExists()
	var req struct {
		AutoNotify       *bool    `json:"auto_notify"`
		ConcurrencyScale *float64 `json:"concurrency_scale"`
		DurationHours    *float64 `json:"duration_hours"`
		QueuePaused      *bool    `json:"queue_paused"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var config models.SystemConfig
	models.DB.First(&config, 1)

	oldBefore := map[string]interface{}{
		"auto_notify":       config.AutoNotify,
		"queue_paused":      config.QueuePaused,
		"concurrency_scale": services.Dispatcher.GetThrottleInfo().EffectiveScale,
	}

	if req.AutoNotify != nil {
		config.AutoNotify = *req.AutoNotify
		if err := models.DB.Save(&config).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "保存系统通知配置失败: " + err.Error()})
			return
		}
	}

	if req.QueuePaused != nil {
		config.QueuePaused = *req.QueuePaused
		if err := models.DB.Save(&config).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "保存队列调度状态失败: " + err.Error()})
			return
		}
		services.SetQueuePaused(*req.QueuePaused)
	}

	if req.ConcurrencyScale != nil {
		var dur time.Duration
		if req.DurationHours != nil && *req.DurationHours > 0 {
			dur = time.Duration(*req.DurationHours * float64(time.Hour))
		}
		services.Dispatcher.SetScale(*req.ConcurrencyScale, dur)
	}

	info := services.Dispatcher.GetThrottleInfo()

	newAfter := map[string]interface{}{
		"auto_notify":       config.AutoNotify,
		"queue_paused":      config.QueuePaused,
		"concurrency_scale": info.EffectiveScale,
	}

	auditDesc := fmt.Sprintf("修改了系统限流/通知配置 (限流倍率: %.2f)", info.EffectiveScale)
	if req.QueuePaused != nil {
		if *req.QueuePaused {
			auditDesc += "；暂停了任务队列派发 (进入排空模式)"
		} else {
			auditDesc += "；恢复了任务队列正常派发"
		}
	}

	commonAudit.SetAuditContext(c, "config", "update", models.AuditLevelP1,
		auditDesc,
		"system_config", "1", "全局扫描限流配置",
		oldBefore, newAfter)

	c.JSON(http.StatusOK, gin.H{
		"id":                config.ID,
		"auto_notify":       config.AutoNotify,
		"queue_paused":      config.QueuePaused,
		"concurrency_scale": info.EffectiveScale,
		"throttle_mode":     info.ThrottleMode,
		"scale_expires_at":  info.ScaleExpiresAt,
		"manual_scale":      info.ManualScale,
		"is_manual":         info.IsManual,
		"is_work_hours":     info.IsWorkHours,
		"work_hours_config": info.WorkHoursConfig,
	})
}

// GetCategoryConfig 获取指定 Category 的动态配置
func GetCategoryConfig(c *gin.Context) {
	cat := c.Param("category")
	var record models.SystemDynamicConfig
	if err := models.DB.Where("category = ?", cat).First(&record).Error; err != nil {
		switch cat {
		case "llm":
			c.JSON(http.StatusOK, models.AppConfig.LLM)
		case "scanner":
			c.JSON(http.StatusOK, models.AppConfig.Scanner)
		case "governance":
			c.JSON(http.StatusOK, models.AppConfig.Governance)
		case "notification":
			c.JSON(http.StatusOK, models.AppConfig.Notification)
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的配置类别: " + cat})
		}
		return
	}

	var data interface{}
	if err := json.Unmarshal(record.Data, &data); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "解析配置失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, data)
}

// UpdateCategoryConfig 更新指定 Category 的动态配置
func UpdateCategoryConfig(c *gin.Context) {
	cat := c.Param("category")
	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取请求失败: " + err.Error()})
		return
	}

	switch cat {
	case "llm":
		var llmCfg models.LLMConfig
		if err := json.Unmarshal(rawBody, &llmCfg); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 LLM 配置 JSON: " + err.Error()})
			return
		}
		models.AppConfig.LLM = llmCfg
		services.Dispatcher.ReloadResources(llmCfg)
	case "scanner":
		var scannerCfg models.ScannerConfig
		if err := json.Unmarshal(rawBody, &scannerCfg); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 Scanner 配置 JSON: " + err.Error()})
			return
		}
		models.AppConfig.Scanner = scannerCfg
		if scannerCfg.WorkerCount > 0 {
			models.AppConfig.Server.WorkerCount = scannerCfg.WorkerCount
		}
		if scannerCfg.MaxQueueSize > 0 {
			models.AppConfig.Server.MaxQueueSize = scannerCfg.MaxQueueSize
		}
	case "governance":
		var govCfg models.GovernancePolicyConfig
		if err := json.Unmarshal(rawBody, &govCfg); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 Governance 配置 JSON: " + err.Error()})
			return
		}
		models.AppConfig.Governance = govCfg
	case "notification":
		var notifCfg models.NotificationConfig
		if err := json.Unmarshal(rawBody, &notifCfg); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 Notification 配置 JSON: " + err.Error()})
			return
		}
		models.AppConfig.Notification = notifCfg
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的配置类别: " + cat})
		return
	}

	models.AppConfig.SyncLegacy()

	var record models.SystemDynamicConfig
	res := models.DB.Where("category = ?", cat).First(&record)
	now := time.Now()
	if res.Error != nil {
		record = models.SystemDynamicConfig{
			Category:  cat,
			Data:      datatypes.JSON(rawBody),
			Version:   1,
			UpdatedBy: "admin",
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := models.DB.Create(&record).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "保存配置失败: " + err.Error()})
			return
		}
	} else {
		record.Data = datatypes.JSON(rawBody)
		record.Version++
		record.UpdatedAt = now
		record.UpdatedBy = "admin"
		if err := models.DB.Save(&record).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "保存配置失败: " + err.Error()})
			return
		}
	}

	commonAudit.SetAuditContext(c, "config", "update_"+cat, models.AuditLevelP1,
		fmt.Sprintf("更新了系统 %s 模块配置", cat),
		"system_dynamic_config", cat, cat+"配置",
		nil, nil)

	c.JSON(http.StatusOK, gin.H{"success": true, "category": cat, "updated_at": record.UpdatedAt})
}

// GetFullConfig 获取全量动态配置
func GetFullConfig(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"llm":          models.AppConfig.LLM,
		"scanner":      models.AppConfig.Scanner,
		"governance":   models.AppConfig.Governance,
		"notification": models.AppConfig.Notification,
	})
}

// UpdateFullConfig 更新全量动态配置
func UpdateFullConfig(c *gin.Context) {
	var req struct {
		LLM          *models.LLMConfig              `json:"llm"`
		Scanner      *models.ScannerConfig          `json:"scanner"`
		Governance   *models.GovernancePolicyConfig `json:"governance"`
		Notification *models.NotificationConfig     `json:"notification"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	now := time.Now()
	saveCategory := func(cat string, v interface{}) error {
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		var record models.SystemDynamicConfig
		if err := models.DB.Where("category = ?", cat).First(&record).Error; err != nil {
			record = models.SystemDynamicConfig{
				Category:  cat,
				Data:      datatypes.JSON(raw),
				Version:   1,
				UpdatedBy: "admin",
				CreatedAt: now,
				UpdatedAt: now,
			}
			return models.DB.Create(&record).Error
		}
		record.Data = datatypes.JSON(raw)
		record.Version++
		record.UpdatedAt = now
		record.UpdatedBy = "admin"
		return models.DB.Save(&record).Error
	}

	if req.LLM != nil {
		models.AppConfig.LLM = *req.LLM
		services.Dispatcher.ReloadResources(*req.LLM)
		_ = saveCategory("llm", *req.LLM)
	}
	if req.Scanner != nil {
		models.AppConfig.Scanner = *req.Scanner
		if req.Scanner.WorkerCount > 0 {
			models.AppConfig.Server.WorkerCount = req.Scanner.WorkerCount
		}
		_ = saveCategory("scanner", *req.Scanner)
	}
	if req.Governance != nil {
		models.AppConfig.Governance = *req.Governance
		_ = saveCategory("governance", *req.Governance)
	}
	if req.Notification != nil {
		models.AppConfig.Notification = *req.Notification
		_ = saveCategory("notification", *req.Notification)
	}

	models.AppConfig.SyncLegacy()

	commonAudit.SetAuditContext(c, "config", "update_full", models.AuditLevelP1,
		"更新了系统全量动态配置",
		"system_dynamic_config", "full", "全量配置",
		nil, nil)

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// PingEndpoint 测试 Native 算力端点连通性与模型响应延迟
func PingEndpoint(c *gin.Context) {
	var req struct {
		BaseURL string `json:"base_url"`
		APIKey  string `json:"api_key"`
		Model   string `json:"model"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	targetURL := req.BaseURL
	if targetURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "base_url 不能为空"})
		return
	}
	if !strings.HasSuffix(targetURL, "/chat/completions") {
		targetURL = strings.TrimRight(targetURL, "/") + "/chat/completions"
	}

	modelName := req.Model
	if modelName == "" {
		modelName = "glm-4-flash"
	}

	payload := map[string]interface{}{
		"model": modelName,
		"messages": []map[string]string{
			{"role": "user", "content": "ping"},
		},
		"max_tokens": 5,
	}
	bodyBytes, _ := json.Marshal(payload)

	httpReq, err := http.NewRequestWithContext(c.Request.Context(), "POST", targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "创建请求失败: " + err.Error()})
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if req.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+req.APIKey)
	}

	client := &http.Client{Timeout: 8 * time.Second}
	startTime := time.Now()
	resp, err := client.Do(httpReq)
	latency := time.Since(startTime).Milliseconds()

	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success":    false,
			"latency_ms": latency,
			"message":    "连接失败: " + err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		c.JSON(http.StatusOK, gin.H{
			"success":     true,
			"latency_ms":  latency,
			"status_code": resp.StatusCode,
			"message":     fmt.Sprintf("连通成功 (延迟: %dms, HTTP %d)", latency, resp.StatusCode),
		})
	} else {
		respBody, _ := io.ReadAll(resp.Body)
		c.JSON(http.StatusOK, gin.H{
			"success":     false,
			"latency_ms":  latency,
			"status_code": resp.StatusCode,
			"message":     fmt.Sprintf("端点返回错误 (HTTP %d): %s", resp.StatusCode, string(respBody)),
		})
	}
}

// ResetCategoryConfig 重置指定模块为 config.yaml 初始种子
func ResetCategoryConfig(c *gin.Context) {
	var req struct {
		Category string `json:"category"`
	}
	_ = c.ShouldBindJSON(&req)

	var seedCfg models.Config
	if err := models.LoadConfig("config.yaml"); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取 config.yaml 失败: " + err.Error()})
		return
	}
	seedCfg = models.AppConfig

	resetOne := func(cat string, v interface{}) {
		models.DB.Where("category = ?", cat).Delete(&models.SystemDynamicConfig{})
		raw, _ := json.Marshal(v)
		now := time.Now()
		models.DB.Create(&models.SystemDynamicConfig{
			Category:  cat,
			Data:      datatypes.JSON(raw),
			Version:   1,
			UpdatedBy: "reset_seed",
			CreatedAt: now,
			UpdatedAt: now,
		})
	}

	if req.Category == "" || req.Category == "all" {
		resetOne("llm", seedCfg.LLM)
		resetOne("scanner", seedCfg.Scanner)
		resetOne("governance", seedCfg.Governance)
		resetOne("notification", seedCfg.Notification)
		services.Dispatcher.ReloadResources(seedCfg.LLM)
	} else {
		switch req.Category {
		case "llm":
			resetOne("llm", seedCfg.LLM)
			services.Dispatcher.ReloadResources(seedCfg.LLM)
		case "scanner":
			resetOne("scanner", seedCfg.Scanner)
		case "governance":
			resetOne("governance", seedCfg.Governance)
		case "notification":
			resetOne("notification", seedCfg.Notification)
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 category: " + req.Category})
			return
		}
	}

	models.InitDynamicConfigs(seedCfg)

	commonAudit.SetAuditContext(c, "config", "reset_seed", models.AuditLevelP1,
		fmt.Sprintf("重置了 %s 配置为初始 YAML 模版", req.Category),
		"system_dynamic_config", req.Category, "配置重置",
		nil, nil)

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "已重置为初始种子配置"})
}

