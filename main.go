package main

import (
	"context"
	"embed"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os/signal"
	"strings"
	"syscall"
	"time"

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
	backfill := flag.Bool("backfill", false, "Backfill historical findings from successful task reports of specialized campaigns into campaign tables")
	flag.Parse()

	if *staleAction != "recover" && *staleAction != "ignore" && *staleAction != "delete" {
		log.Fatalf("Invalid -stale option: %q. Allowed values: recover, ignore, delete", *staleAction)
	}

	log.Printf("Code-Shield Server %s (commit: %s, built: %s)\n", Version, CommitID, BuildTime)

	// Initialize database
	models.InitDB()

	// Load global configuration
	if err := models.LoadConfig("config.yaml"); err != nil {
		log.Fatalf("Failed to load config.yaml: %v", err)
	}

	// 初始化多 LLM 调度分配器
	services.InitModelDispatcher()

	if *backfill {
		log.Println("[Server] Running in backfill mode.")
		if err := services.BackfillHistoricalFindings(); err != nil {
			log.Fatalf("[Server] Backfill failed: %v", err)
		}
		log.Println("[Server] Backfill completed successfully.")
		return
	}

	// Sync opencode agent files with current prompt files
	services.SyncAllAgents()

	// Start worker pool with configured concurrency
	services.StartWorkerPool(models.AppConfig.Server.WorkerCount)

	// Recover tasks that were pending/running before the last shutdown
	services.RecoverPendingTasks(*staleAction)

	// Start cron jobs
	cron_jobs.StartCronJobs()

	// Initialize Gin engine
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	if models.AppConfig.Server.GinLog {
		r.Use(gin.Logger())
		log.Println("[Server] GIN request logging enabled")
	}

	// Register Auth routes (unprotected)
	auth := r.Group("/api")
	{
		auth.POST("/login", handlers.Login)
		auth.GET("/auth/config", handlers.GetAuthConfig)
		auth.GET("/public/tasks/:id", handlers.GetPublicTaskDetails)
		auth.GET("/public/tasks/:id/findings", handlers.GetPublicAnalysisFindings)

		// Sync endpoints
		auth.POST("/sync/user", handlers.SyncUser)
		auth.POST("/sync/department", handlers.SyncDepartment)
		auth.POST("/sync/repo", handlers.SyncRepo)

	}

	// Register API routes (protected)
	api := r.Group("/api")
	api.Use(handlers.AuthMiddleware())
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
		api.PATCH("/config", handlers.UpdateConfig)

		// Task routes (generic, replaces /reviews/*)
		api.GET("/tasks/overview", handlers.GetTaskOverview)
		api.GET("/tasks", handlers.GetTasks)
		api.POST("/tasks/trigger", handlers.TriggerTask)
		api.POST("/tasks/trigger-missing", handlers.TriggerMissingTasks)
		api.POST("/tasks/:id/notify", handlers.TriggerManualNotification)
		api.POST("/tasks/:id/resume", handlers.ResumeTask)
		api.GET("/tasks/:id", handlers.GetTaskDetails)
		api.GET("/tasks/:id/report", handlers.GetTaskReportMarkdown)
		api.GET("/tasks/:id/synthesis", handlers.GetTaskReportSynthesisJSON)
		api.GET("/tasks/:id/synthesis/csv", handlers.ExportTaskReportSynthesisCSV)
		api.GET("/tasks/:id/summary", handlers.GetTaskReportSummaryJSON)
		api.GET("/tasks/:id/findings", handlers.GetAnalysisFindings)

		// Task type management (read-only for normal users)
		api.GET("/task-types", handlers.GetTaskTypes)
		api.GET("/task-types/:id", handlers.GetTaskType)
		api.GET("/task-types/:id/files", handlers.GetTaskTypeFiles)

		api.GET("/issues", handlers.GetIssues)
		api.POST("/issues", handlers.CreateIssue)
		api.PATCH("/issues/:id", handlers.UpdateIssue)

		// UT Effectiveness Dashboard
		api.GET("/analysis/ut/repos", handlers.GetUTRepos)
		api.GET("/analysis/ut/findings", handlers.GetUTFindings)
		api.GET("/analysis/ut/findings/export", handlers.ExportUTFindings)
		api.PATCH("/analysis/ut/findings/:id", handlers.UpdateUTFinding)
		api.GET("/analysis/ut/departments", handlers.GetUTDepartments)
		api.GET("/analysis/ut/trends", handlers.GetUTTrends)

		// Campaign generic routes mapping
		registerCampaignRoutes[models.CoredumpFinding](api, "coredump", "coredump_risk")
		registerCampaignRoutes[models.FloatFinding](api, "float", "float_comparison")
		registerCampaignRoutes[models.ThreadFinding](api, "thread", "thread_create")
		registerCampaignRoutes[models.CjsonFinding](api, "cjson", "cjson_scan")
		registerCampaignRoutes[models.DeepReviewFinding](api, "deep-review", "deep_review")

		api.GET("/schedules", handlers.GetSchedules)
		api.POST("/schedules", handlers.CreateSchedule)
		api.PUT("/schedules/:id", handlers.UpdateSchedule)
		api.DELETE("/schedules/:id", handlers.DeleteSchedule)
		api.POST("/schedules/:id/trigger", handlers.TriggerSchedule)

		api.GET("/executions", handlers.GetExecutionLogs)
		api.DELETE("/executions/completed", handlers.ClearCompletedExecutionLogs)
		api.DELETE("/executions/:id", handlers.DeletePendingExecution)

		// Admin only routes
		admin := api.Group("/")
		admin.Use(handlers.AdminMiddleware())
		{
			admin.POST("/task-types", handlers.CreateTaskType)
			admin.PATCH("/task-types/:id", handlers.UpdateTaskType)
			admin.DELETE("/task-types/:id", handlers.DeleteTaskType)
			admin.PUT("/task-types/:id/files/:file_type", handlers.UpdateTaskTypeFile)
			admin.POST("/task-types/:id/trigger-all", handlers.TriggerAllReposForTaskType)
			admin.DELETE("/tasks/invalid-reports", handlers.ClearInvalidReports)
			admin.DELETE("/tasks/:id", handlers.DeleteTaskReport)
		}
	}

	// Serve frontend static files
	distFS, err := fs.Sub(frontendFS, "frontend/dist")
	if err != nil {
		log.Println("Warning: frontend dist folder not found, skipping frontend embedding.")
	} else {
		httpFS := http.FS(distFS)
		r.NoRoute(func(c *gin.Context) {
			path := c.Request.URL.Path

			// DO NOT serve frontend index.html for API routes
			if len(path) >= 4 && path[:4] == "/api" {
				c.JSON(http.StatusNotFound, gin.H{"error": "API route not found"})
				return
			}

			// If the user visits the root / or exact /shield (no slash), redirect to /shield/
			if path == "/" || path == "/shield" {
				c.Redirect(http.StatusFound, "/shield/")
				return
			}

			// Strip /shield prefix if present to match internal dist directory structure
			cleanPath := path
			if strings.HasPrefix(path, "/shield") {
				cleanPath = strings.TrimPrefix(path, "/shield")
			}

			if cleanPath != "" && cleanPath != "/" {
				f, err := distFS.Open(cleanPath[1:])
				if err == nil {
					f.Close()
					c.FileFromFS(cleanPath, httpFS)
					return
				}
			}
			indexBytes, err := fs.ReadFile(distFS, "index.html")
			if err != nil {
				c.String(http.StatusNotFound, "index.html not found")
				return
			}
			c.Data(http.StatusOK, "text/html; charset=utf-8", indexBytes)
		})
	}

	// Build HTTP server with timeouts
	port := models.AppConfig.Server.Port
	if port == "" {
		port = ":8080"
	}

	// Wrap standard handler to strip /shield prefix for API requests before passing to Gin
	var httpHandler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, "/shield/api") {
			req.URL.Path = strings.TrimPrefix(req.URL.Path, "/shield")
		}
		r.ServeHTTP(w, req)
	})

	srv := &http.Server{
		Addr:              port,
		Handler:           httpHandler,
		ReadTimeout:       models.AppConfig.Server.ReadTimeout,
		ReadHeaderTimeout: models.AppConfig.Server.ReadHeaderTimeout,
		WriteTimeout:      models.AppConfig.Server.WriteTimeout,
		IdleTimeout:       models.AppConfig.Server.IdleTimeout,
		MaxHeaderBytes:    models.AppConfig.Server.MaxHeaderBytes,
	}

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("Starting server on %s ...\n", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("Shutting down server ...")

	// Cancel all running tasks to terminate AI processes
	services.CancelAllRunningTasks()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}
	log.Println("Server exited gracefully")
}

func registerCampaignRoutes[T any](rg *gin.RouterGroup, campaignPath string, taskTypeName string) {
	rg.GET("/analysis/"+campaignPath+"/repos", handlers.GetCampaignRepos[T](taskTypeName))
	rg.GET("/analysis/"+campaignPath+"/findings", handlers.GetCampaignFindings[T]())
	rg.GET("/analysis/"+campaignPath+"/findings/export", handlers.ExportCampaignFindings[T]())
	rg.PATCH("/analysis/"+campaignPath+"/findings/:id", handlers.UpdateCampaignFinding[T]())
	rg.GET("/analysis/"+campaignPath+"/departments", handlers.GetCampaignDepartments[T]())
	rg.GET("/analysis/"+campaignPath+"/trends", handlers.GetCampaignTrends[T]())
}
