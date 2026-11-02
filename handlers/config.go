package handlers

import (
	"bytes"
	commonAudit "code-common/backend/audit"
	commonAuth "code-common/backend/auth"
	"code-shield/models"
	"code-shield/services"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
		"adaptive_config":   info.AdaptiveConfig,
		"adaptive_status":   info.AdaptiveStatus,
	})
}

func UpdateConfig(c *gin.Context) {
	ensureConfigExists()
	var req struct {
		AutoNotify       *bool    `json:"auto_notify"`
		ConcurrencyScale *float64 `json:"concurrency_scale"`
		DurationHours    *float64 `json:"duration_hours"`
		QueuePaused      *bool    `json:"queue_paused"`
		ThrottleMode     *string  `json:"throttle_mode"`
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
		"throttle_mode":     services.Dispatcher.GetThrottleInfo().ThrottleMode,
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

	if (req.ThrottleMode != nil && *req.ThrottleMode == "adaptive") || (req.ConcurrencyScale != nil && *req.ConcurrencyScale < 0) {
		services.Dispatcher.RestoreManualScale()
	} else if req.ConcurrencyScale != nil {
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
		"throttle_mode":     info.ThrottleMode,
	}

	auditDesc := fmt.Sprintf("修改了系统限流/通知配置 (限流模式: %s, 限流倍率: %.2f)", info.ThrottleMode, info.EffectiveScale)
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
		"adaptive_config":   info.AdaptiveConfig,
		"adaptive_status":   info.AdaptiveStatus,
	})
}

// GetFrontendConfig exposes only non-sensitive UI integration settings.
func GetFrontendConfig(c *gin.Context) {
	if err := models.AppConfig.AIFix.Validate(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "AI fix configuration is invalid"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ai_fix": gin.H{
			"fix_url": models.AppConfig.AIFix.FixURL,
		},
	})
}

func maskAPIKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return "********"
	}
	prefix := ""
	if strings.HasPrefix(key, "sk-") {
		prefix = "sk-"
	}
	suffix := key[len(key)-4:]
	return prefix + "****" + suffix
}

func maskComputeSecrets(cfg models.DynamicComputeConfig) models.DynamicComputeConfig {
	masked := cfg
	if len(cfg.LLM.Resources) > 0 {
		masked.LLM.Resources = make([]models.ComputeResourceConfig, len(cfg.LLM.Resources))
		for i, res := range cfg.LLM.Resources {
			resCopy := res
			if len(res.Endpoints) > 0 {
				resCopy.Endpoints = make([]models.ResourceEndpointConfig, len(res.Endpoints))
				for j, ep := range res.Endpoints {
					epCopy := ep
					epCopy.APIKey = maskAPIKey(ep.APIKey)
					resCopy.Endpoints[j] = epCopy
				}
			}
			masked.LLM.Resources[i] = resCopy
		}
	}
	return masked
}

// GetComputeConfig 获取当前生效的动态算力配置快照及元数据
func GetComputeConfig(c *gin.Context) {
	var record models.SystemDynamicConfig
	if err := models.DB.Where("category = ?", "compute").First(&record).Error; err != nil {
		current := models.GetComputeConfig()
		c.JSON(http.StatusOK, gin.H{
			"version":      1,
			"updated_at":   time.Now(),
			"updated_by":   "system_seed",
			"llm":          current.LLM,
			"tiers":        current.Tiers,
			"worker_count": current.WorkerCount,
		})
		return
	}

	var data models.DynamicComputeConfig
	if err := json.Unmarshal(record.Data, &data); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "解析算力配置失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"version":      record.Version,
		"updated_at":   record.UpdatedAt,
		"updated_by":   record.UpdatedBy,
		"llm":          data.LLM,
		"tiers":        data.Tiers,
		"worker_count": data.WorkerCount,
	})
}

func preserveExistingSecrets(next *models.DynamicComputeConfig, old models.DynamicComputeConfig) {
	if next == nil {
		return
	}
	for i := range next.LLM.Resources {
		nextRes := &next.LLM.Resources[i]
		oldRes := old.LLM.FindResource(nextRes.ID)
		if oldRes == nil {
			continue
		}
		oldEndpoints := make(map[string]string)
		for _, ep := range oldRes.Endpoints {
			if strings.TrimSpace(ep.APIKey) != "" {
				oldEndpoints[ep.Name] = ep.APIKey
			}
		}
		for j := range nextRes.Endpoints {
			nextEp := &nextRes.Endpoints[j]
			trimmed := strings.TrimSpace(nextEp.APIKey)
			if trimmed == "" || strings.Contains(trimmed, "****") {
				if existingKey, ok := oldEndpoints[nextEp.Name]; ok {
					nextEp.APIKey = existingKey
				}
			}
		}
	}
}

// UpdateComputeConfigRequest 算力配置更新请求负载
type UpdateComputeConfigRequest struct {
	Version     int                      `json:"version"`
	LLM         models.LLMConfig         `json:"llm"`
	Tiers       models.DebateTiersConfig `json:"tiers"`
	WorkerCount int                      `json:"worker_count"`
}

// UpdateComputeConfig 更新动态算力配置快照 (含 version CAS 乐观锁防并发覆盖)
func UpdateComputeConfig(c *gin.Context) {
	var req UpdateComputeConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数无效: " + err.Error()})
		return
	}

	nextCompute := models.DynamicComputeConfig{
		LLM:         req.LLM,
		Tiers:       req.Tiers,
		WorkerCount: req.WorkerCount,
	}

	// 1. 查询存量配置，防敏感 API Key 被空值或脱敏掩码意外冲刷覆盖
	var record models.SystemDynamicConfig
	res := models.DB.Where("category = ?", "compute").First(&record)
	var oldCompute models.DynamicComputeConfig
	if res.Error == nil {
		_ = json.Unmarshal(record.Data, &oldCompute)
	} else {
		oldCompute = models.GetComputeConfig()
	}
	preserveExistingSecrets(&nextCompute, oldCompute)

	// 2. 强约束校验（默认资源合法性、Native 端点存在性、各阶段引擎类型约束）
	if err := models.ValidateComputeConfig(&nextCompute, &models.AppConfig); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	models.NormalizeComputeDefaults(&nextCompute)

	// 3. 乐观锁校验 (CAS 防并发静默覆盖)
	now := time.Now()
	operator := "admin"
	if user := commonAuth.GetUserContext(c); user != nil && user.Username != "" {
		operator = user.Username
	}

	if res.Error == nil {
		// 已存在记录，比对提交的 version 与数据库当前 version
		if req.Version > 0 && record.Version != req.Version {
			c.JSON(http.StatusConflict, gin.H{
				"error":           fmt.Sprintf("配置版本冲突：当前版本为 %d（提交版本为 %d），请刷新获取最新配置后再保存", record.Version, req.Version),
				"current_version": record.Version,
			})
			return
		}
	}

	// 4. 序列化并持久化至 DB (SSOT)
	rawJSON, err := json.Marshal(nextCompute)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "序列化配置失败: " + err.Error()})
		return
	}

	if res.Error != nil {
		record = models.SystemDynamicConfig{
			Category:  "compute",
			Data:      datatypes.JSON(rawJSON),
			Version:   1,
			UpdatedBy: operator,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := models.DB.Create(&record).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "保存算力配置失败: " + err.Error()})
			return
		}
	} else {
		record.Data = datatypes.JSON(rawJSON)
		record.Version++
		record.UpdatedAt = now
		record.UpdatedBy = operator
		if err := models.DB.Save(&record).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "保存算力配置失败: " + err.Error()})
			return
		}
	}

	// 4. 原子更新内存快照与热重载后台服务
	models.ApplyDynamicComputeConfig(nextCompute)
	services.Dispatcher.ReloadResources(nextCompute.LLM)
	if nextCompute.WorkerCount > 0 {
		services.ResizeWorkerPool(nextCompute.WorkerCount)
	}

	// 5. 脱敏审计 Diff 日志
	maskedOld := maskComputeSecrets(oldCompute)
	maskedNew := maskComputeSecrets(nextCompute)
	commonAudit.SetAuditContext(c, "compute", "update", models.AuditLevelP1,
		fmt.Sprintf("更新了系统算力配置 (Worker: %d, 节点数: %d, 版本: %d -> %d)",
			nextCompute.WorkerCount, len(nextCompute.LLM.Resources), record.Version-1, record.Version),
		"system_dynamic_config", "compute", "算力调度配置",
		maskedOld, maskedNew)

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"version":    record.Version,
		"updated_at": record.UpdatedAt,
		"updated_by": record.UpdatedBy,
	})
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

	rawURL := strings.TrimSpace(req.BaseURL)
	if rawURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "base_url 不能为空"})
		return
	}

	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "base_url 必须为合法的 HTTP 或 HTTPS URL"})
		return
	}

	targetURL := rawURL
	if !strings.HasSuffix(targetURL, "/chat/completions") {
		targetURL = strings.TrimRight(targetURL, "/") + "/chat/completions"
	}

	modelName := strings.TrimSpace(req.Model)
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

	// 内存防膨胀：使用 LimitReader 仅读取前 1024 字节
	limitedReader := io.LimitReader(resp.Body, 1024)
	respBody, _ := io.ReadAll(limitedReader)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		c.JSON(http.StatusOK, gin.H{
			"success":     true,
			"latency_ms":  latency,
			"status_code": resp.StatusCode,
			"message":     fmt.Sprintf("连通成功 (延迟: %dms, HTTP %d)", latency, resp.StatusCode),
		})
	} else {
		c.JSON(http.StatusOK, gin.H{
			"success":     false,
			"latency_ms":  latency,
			"status_code": resp.StatusCode,
			"message":     fmt.Sprintf("端点返回错误 (HTTP %d): %s", resp.StatusCode, string(respBody)),
		})
	}
}

// ResetComputeConfigToSeed 重置动态算力配置为 config.yaml 初始种子
func ResetComputeConfigToSeed(c *gin.Context) {
	seedCfg, err := models.ParseConfigFile("config.yaml")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取 config.yaml 失败: " + err.Error()})
		return
	}

	seedCompute := models.DynamicComputeConfig{
		LLM:         seedCfg.LLM,
		Tiers:       seedCfg.Scanner.Debate.Tiers,
		WorkerCount: seedCfg.Scanner.WorkerCount,
	}
	models.NormalizeComputeDefaults(&seedCompute)

	rawJSON, err := json.Marshal(seedCompute)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "序列化种子配置失败: " + err.Error()})
		return
	}

	operator := "reset_seed"
	if user := commonAuth.GetUserContext(c); user != nil && user.Username != "" {
		operator = user.Username
	}

	now := time.Now()
	var record models.SystemDynamicConfig
	if err := models.DB.Where("category = ?", "compute").First(&record).Error; err != nil {
		record = models.SystemDynamicConfig{
			Category:  "compute",
			Data:      datatypes.JSON(rawJSON),
			Version:   1,
			UpdatedBy: operator,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := models.DB.Create(&record).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "保存重置配置失败: " + err.Error()})
			return
		}
	} else {
		record.Data = datatypes.JSON(rawJSON)
		record.Version++
		record.UpdatedAt = now
		record.UpdatedBy = operator
		if err := models.DB.Save(&record).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "保存重置配置失败: " + err.Error()})
			return
		}
	}

	models.ApplyDynamicComputeConfig(seedCompute)
	services.Dispatcher.ReloadResources(seedCompute.LLM)
	if seedCompute.WorkerCount > 0 {
		services.ResizeWorkerPool(seedCompute.WorkerCount)
	}

	masked := maskComputeSecrets(seedCompute)
	commonAudit.SetAuditContext(c, "compute", "reset_seed", models.AuditLevelP1,
		fmt.Sprintf("重置了算力配置为初始 YAML 模版 (版本: %d)", record.Version),
		"system_dynamic_config", "compute", "算力配置重置",
		nil, masked)

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"message":    "已重置为初始种子配置",
		"version":    record.Version,
		"updated_at": record.UpdatedAt,
		"updated_by": record.UpdatedBy,
	})
}
