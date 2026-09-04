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
func (ctx *taskContext) executeHooks(findings []models.AnalysisFinding) {
	// 1. 如果任务类型启用了专项分析 (IsCampaign)，自动触发通用归并引擎
	if ctx.taskType.IsCampaign {
		log.Printf("[TaskHooks] Running generic campaign hook for %q (GovernanceMode: %s, Report ID: %d)",
			ctx.taskType.Name, ctx.taskType.GovernanceMode, ctx.report.ID)
		if err := handleGenericCampaignHook(ctx, findings); err != nil {
			log.Printf("[TaskHooks] Generic campaign hook for %q failed: %v", ctx.taskType.Name, err)
		}
	}

	// 2. 执行已注册的自定义 Hooks
	taskHooksMu.RLock()
	hooks, ok := taskHooks[ctx.taskType.Name]
	taskHooksMu.RUnlock()
	if !ok {
		return
	}
	log.Printf("[TaskHooks] Running %d custom hooks for task type %q (Report ID: %d)", len(hooks), ctx.taskType.Name, ctx.report.ID)
	for i, hook := range hooks {
		if err := hook(ctx, findings); err != nil {
			log.Printf("[TaskHooks] Hook %d for %q failed: %v", i, ctx.taskType.Name, err)
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
		Ctx:             ctx.ctx,
		TaskType:        ctx.taskType,
		Repo:            ctx.repo,
		Report:          ctx.report,
		CodesPath:       ctx.codesPath,
		HasFailedChunks: ctx.hasFailedChunks,
		Invoker:         inv,
	}

	return governance.HandleGenericCampaign(campCtx, findings)
}

// SanitizeFindingTitle 规范化缺陷标题，委托给 governance 子包
func SanitizeFindingTitle(title string) string {
	return governance.SanitizeFindingTitle(title)
}
