package services

import (
	"log"
	"sync"

	"code-shield/models"
	"code-shield/services/governance"
)

// TaskHook is a callback function run when a task finishes successfully
type TaskHook func(ctx *taskContext, findings []models.AnalysisFinding) error

var (
	taskHooksMu sync.RWMutex
	taskHooks   = make(map[string][]TaskHook)
)

// RegisterTaskHook registers a postprocess hook for a specific task type name
func RegisterTaskHook(taskTypeName string, hook TaskHook) {
	taskHooksMu.Lock()
	defer taskHooksMu.Unlock()
	taskHooks[taskTypeName] = append(taskHooks[taskTypeName], hook)
}

// executeHooks runs all hooks registered for the current task type
func executeHooks(ctx *taskContext, findings []models.AnalysisFinding) {
	// 1. 如果任务类型启用了专项分析 (IsCampaign)，自动触发通用归并引擎
	if ctx.TaskType.IsCampaign {
		log.Printf("[TaskHooks] Running generic campaign hook for %q (GovernanceMode: %s, Report ID: %d)",
			ctx.TaskType.Name, ctx.TaskType.GovernanceMode, ctx.Report.ID)
		if err := handleGenericCampaignHook(ctx, findings); err != nil {
			log.Printf("[TaskHooks] Generic campaign hook for %q failed: %v", ctx.TaskType.Name, err)
		}
	}

	// 2. 执行已注册的自定义 Hooks
	taskHooksMu.RLock()
	hooks, ok := taskHooks[ctx.TaskType.Name]
	taskHooksMu.RUnlock()
	if !ok {
		return
	}
	log.Printf("[TaskHooks] Running %d custom hooks for task type %q (Report ID: %d)", len(hooks), ctx.TaskType.Name, ctx.Report.ID)
	for i, hook := range hooks {
		if err := hook(ctx, findings); err != nil {
			log.Printf("[TaskHooks] Hook %d for %q failed: %v", i, ctx.TaskType.Name, err)
		}
	}
}

// handleGenericCampaignHook 将任务执行上下文适配并转接至治理领域的 HandleGenericCampaign
func handleGenericCampaignHook(ctx *taskContext, findings []models.AnalysisFinding) error {
	backend := models.AppConfig.AI.ToolBackends.FindingMatch
	if backend == "" {
		backend = "native"
	}
	if !IsValidAIBackend(backend) {
		backend = models.AppConfig.AI.Backend
	}
	if backend == "" {
		backend = "claude"
	}

	inv := GetAIInvoker(backend)

	campCtx := &governance.CampaignContext{
		Ctx:             ctx.Ctx,
		TaskType:        ctx.TaskType,
		Repo:            ctx.Repo,
		Report:          ctx.Report,
		CodesPath:       ctx.CodesPath,
		HasFailedChunks: ctx.HasFailedChunks,
		Invoker:         inv,
	}

	return governance.HandleGenericCampaign(campCtx, findings)
}

// SanitizeFindingTitle 规范化缺陷标题，委托给 governance 子包
func SanitizeFindingTitle(title string) string {
	return governance.SanitizeFindingTitle(title)
}
