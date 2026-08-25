package main

import (
	"context"
	"embed"
	"flag"
	"log"

	commonAudit "code-common/backend/audit"
	commonAuth "code-common/backend/auth"
	commonServer "code-common/backend/server"
	"code-shield/cron_jobs"
	"code-shield/handlers"
	"code-shield/models"
	"code-shield/services"

	"github.com/gin-gonic/gin"
)

//go:embed all:frontend/dist
var frontendFS embed.FS

// 构建时通过 -ldflags 注入
var (
	Version   = "dev"
	CommitID  = "unknown"
	BuildTime = "unknown"
)

func main() {
	staleAction := flag.String("stale", "recover", "Action for stale (pending/running) tasks found at startup: 'recover' (re-enqueue), 'ignore' (leave as is), 'delete' (delete from database)")
	flag.Parse()

	if *staleAction != "recover" && *staleAction != "ignore" && *staleAction != "delete" {
		log.Fatalf("Invalid -stale option: %q. Allowed values: recover, ignore, delete", *staleAction)
	}

	log.Printf("Code-Shield Server %s (commit: %s, built: %s)\n", Version, CommitID, BuildTime)

	// Load global configuration
	if err := models.LoadConfig("config.yaml"); err != nil {
		log.Fatalf("Failed to load config.yaml: %v", err)
	}

	// Initialize database
	models.InitDB()

	// 初始化系统全局操作审计引擎
	commonAudit.Init(models.DB)

	// 确保至少存在默认管理员账号（用于独立部署模式）
	if err := commonAuth.EnsureSeedAdmin(models.DB, "shield_admin"); err != nil {
		log.Printf("[Server] Warning: Failed to ensure seed admin: %v", err)
	}

	// 初始化多 LLM 调度分配器
	services.InitModelDispatcher()

	// Sync opencode agent files with current prompt files
	services.SyncAllAgents()

	// Start worker pool with configured concurrency
	services.StartWorkerPool(models.AppConfig.Server.WorkerCount)

	// Recover tasks that were pending/running before the last shutdown
	services.RecoverPendingTasks(*staleAction)

	// Start cron jobs
	cron_jobs.StartCronJobs()

	// 启动统一服务器
	err := commonServer.Run(commonServer.Options{
		ServiceName:       "Code-Shield",
		Prefix:            "shield",
		Port:              models.AppConfig.Server.Port,
		GinLog:            models.AppConfig.Server.GinLog,
		ReadTimeout:       models.AppConfig.Server.ReadTimeout,
		ReadHeaderTimeout: models.AppConfig.Server.ReadHeaderTimeout,
		WriteTimeout:      models.AppConfig.Server.WriteTimeout,
		IdleTimeout:       models.AppConfig.Server.IdleTimeout,
		MaxHeaderBytes:    models.AppConfig.Server.MaxHeaderBytes,
		FrontendFS:        &frontendFS,
		CustomMiddlewares: []gin.HandlerFunc{
			commonAudit.Middleware("shield"),
		},
		OnShutdown: func(ctx context.Context) {
			// Cancel all running tasks to terminate AI processes
			services.CancelAllRunningTasks()
			// 优雅关闭审计落库引擎
			_ = commonAudit.Close(ctx)
		},
		RegisterRoutes: func(r *gin.Engine) {
			// Register Auth routes (unprotected)
			auth := r.Group("/api")
			{
				auth.POST("/login", handlers.Login)
				auth.GET("/auth/config", handlers.GetAuthConfig)
				auth.GET("/oauth2/authorize", handlers.StartOAuth2Flow)
				auth.GET("/oauth2/callback", handlers.OAuth2Callback)
			}

			// Register API routes (protected)
			api := r.Group("/api")
			api.Use(commonAuth.AuthMiddleware(commonAuth.AuthConfig{
				JWTSecretGetter: func() string { return models.AppConfig.Auth.JWTSecret },
				DB:              models.DB,
				MergeDBRoles:    true,
				OnUserNotFound:  handlers.ProvisionShieldUser,
				OnUserSynced:    handlers.SyncShieldUser,
			}))
			{
				api.GET("/me", handlers.GetMe)
				api.GET("/me/findings", handlers.GetMyFindings)
				api.PATCH("/password", handlers.UpdatePassword)
				api.POST("/me/department", handlers.UpdateMyDepartment)

				api.GET("/departments", handlers.GetDepartments)
				api.GET("/departments/export", handlers.ExportDepartments)

				api.GET("/users", handlers.GetUsers)
				api.GET("/users/export", handlers.ExportUsers)

				api.GET("/repos", handlers.GetRepos)
				api.GET("/repos/export", handlers.ExportRepos)

				api.GET("/config", handlers.GetConfig)

				// Task routes (generic, replaces /reviews/*)
				api.GET("/tasks/overview", handlers.GetTaskOverview)
				api.GET("/tasks", handlers.GetTasks)
				api.POST("/tasks/trigger", handlers.TriggerTask)
				api.POST("/tasks/trigger-missing", handlers.TriggerMissingTasks)
				api.POST("/tasks/:id/notify", handlers.TriggerManualNotification)
				api.POST("/tasks/:id/resume", handlers.ResumeTask)
				api.GET("/tasks/:id", handlers.GetTaskDetails)

				// Standardized Task Report & Export Routes (New Architecture)
				api.GET("/tasks/:id/report/summary", handlers.GetReportSummaryHandler)
				api.GET("/tasks/:id/report/findings", handlers.GetReportFindingsHandler)
				api.GET("/tasks/:id/report/diagnostics", handlers.GetReportDiagnosticsHandler)
				api.GET("/tasks/:id/summary", handlers.GetReportDiagnosticsHandler) // 兼容历史别名
				api.GET("/tasks/:id/report/aggregate", handlers.GetReportAggregateHandler)
				api.GET("/tasks/:id/report/export", handlers.ExportReportHandler)

				// Task type management (read-only for normal users)
				api.GET("/task-types", handlers.GetTaskTypes)
				api.GET("/task-types/:id", handlers.GetTaskType)
				api.GET("/task-types/:id/files", handlers.GetTaskTypeFiles)

				api.GET("/issues", handlers.GetIssues)
				api.POST("/issues", handlers.CreateIssue)
				api.PATCH("/issues/:id", handlers.UpdateIssue)

				// Dynamic Universal Campaign Routes (参数化通用专项路由)
				campaignGroup := api.Group("/analysis/:campaign")
				campaignGroup.Use(handlers.ResolveCampaignMiddleware())
				{
					campaignGroup.GET("/repos", handlers.GetDynamicCampaignRepos)
					campaignGroup.GET("/findings", handlers.GetDynamicCampaignFindings)
					campaignGroup.GET("/findings/:id", handlers.GetDynamicCampaignFinding)
					campaignGroup.GET("/findings/export", handlers.ExportDynamicCampaignFindings)
					campaignGroup.PATCH("/findings/:id", handlers.UpdateDynamicCampaignFinding)
					campaignGroup.GET("/departments", handlers.GetDynamicCampaignDepartments)
					campaignGroup.GET("/trends", handlers.GetDynamicCampaignTrends)
				}

				api.GET("/schedules", handlers.GetSchedules)
				api.POST("/schedules", handlers.CreateSchedule)
				api.PUT("/schedules/:id", handlers.UpdateSchedule)
				api.DELETE("/schedules/:id", handlers.DeleteSchedule)
				api.POST("/schedules/:id/trigger", handlers.TriggerSchedule)

				api.GET("/executions", handlers.GetExecutionLogs)
				api.DELETE("/executions/completed", handlers.ClearCompletedExecutionLogs)
				api.DELETE("/executions/batch", handlers.BatchDeletePendingExecutions)
				api.DELETE("/executions/:id", handlers.DeletePendingExecution)

				// Trigger logs (扫描批次触发日志，原 audit-logs)
				api.GET("/trigger-logs", handlers.GetTriggerLogs)
				api.GET("/trigger-logs/stats", handlers.GetTriggerLogStats)
				api.GET("/trigger-logs/:id", handlers.GetTriggerLogDetail)

				// Admin only routes
				admin := api.Group("/")
				admin.Use(commonAuth.RequireAdmin(commonAuth.RoleShieldAdmin))
				{
					admin.DELETE("/trigger-logs", handlers.ClearTriggerLogs)
					admin.POST("/task-types", handlers.CreateTaskType)
					admin.PATCH("/task-types/:id", handlers.UpdateTaskType)
					admin.DELETE("/task-types/:id", handlers.DeleteTaskType)
					admin.PUT("/task-types/:id/files/:file_type", handlers.UpdateTaskTypeFile)
					admin.POST("/task-types/:id/trigger-all", handlers.TriggerAllReposForTaskType)
					admin.DELETE("/tasks/invalid-reports", handlers.ClearInvalidReports)
					admin.DELETE("/tasks/:id", handlers.DeleteTaskReport)
					admin.PATCH("/config", handlers.UpdateConfig)
				}
			}
		},
	})
	if err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
