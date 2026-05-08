package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"code-shield/cron_jobs"
	"code-shield/handlers"
	"code-shield/models"
	"code-shield/services"

	"github.com/gin-gonic/gin"
)

//go:embed frontend/dist/*
var frontendFS embed.FS

func main() {
	// Initialize database
	models.InitDB()

	// Load global configuration
	if err := models.LoadConfig("config.yaml"); err != nil {
		log.Fatalf("Failed to load config.yaml: %v", err)
	}

	// Start worker pool (e.g. 5 concurrent workers)
	services.StartWorkerPool(5)

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
	}

	// Register API routes (protected)
	api := r.Group("/api")
	api.Use(handlers.AuthMiddleware())
	{
		api.GET("/me", handlers.GetMe)
		api.PATCH("/password", handlers.UpdatePassword)

		api.GET("/teams", handlers.GetTeams)
		api.POST("/teams", handlers.CreateTeam)
		api.POST("/teams/import", handlers.ImportTeams)
		api.GET("/teams/export", handlers.ExportTeams)
		api.PATCH("/teams/:id", handlers.UpdateTeam)
		api.DELETE("/teams/:id", handlers.DeleteTeam)

		api.GET("/members", handlers.GetMembers)
		api.POST("/members", handlers.CreateMember)
		api.POST("/members/import", handlers.ImportMembers)
		api.GET("/members/export", handlers.ExportMembers)
		api.PATCH("/members/:id", handlers.UpdateMember)
		api.DELETE("/members/:id", handlers.DeleteMember)

		api.GET("/repos", handlers.GetRepos)
		api.POST("/repos", handlers.CreateRepo)
		api.PATCH("/repos/:id", handlers.UpdateRepo)
		api.DELETE("/repos/:id", handlers.DeleteRepo)
		api.POST("/repos/import", handlers.ImportRepos)
		api.GET("/repos/export", handlers.ExportRepos)

		api.GET("/config", handlers.GetConfig)
		api.PATCH("/config", handlers.UpdateConfig)

		// Task routes (generic, replaces /reviews/*)
		api.GET("/tasks/overview", handlers.GetTaskOverview)
		api.GET("/tasks", handlers.GetTasks)
		api.POST("/tasks/trigger", handlers.TriggerTask)
		api.POST("/tasks/:id/notify", handlers.TriggerManualNotification)
		api.GET("/tasks/:id", handlers.GetTaskDetails)
		api.GET("/tasks/:id/report", handlers.GetTaskReportMarkdown)

		// Task type management (read-only for normal users)
		api.GET("/task-types", handlers.GetTaskTypes)
		api.GET("/task-types/:id", handlers.GetTaskType)
		api.GET("/task-types/:id/files", handlers.GetTaskTypeFiles)

		api.GET("/issues", handlers.GetIssues)
		api.POST("/issues", handlers.CreateIssue)
		api.PATCH("/issues/:id", handlers.UpdateIssue)

		api.GET("/schedules", handlers.GetSchedules)
		api.POST("/schedules", handlers.CreateSchedule)
		api.PUT("/schedules/:id", handlers.UpdateSchedule)
		api.DELETE("/schedules/:id", handlers.DeleteSchedule)
		api.POST("/schedules/:id/trigger", handlers.TriggerSchedule)

		api.GET("/executions", handlers.GetExecutionLogs)
		api.DELETE("/executions/completed", handlers.ClearCompletedExecutionLogs)

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

			users := admin.Group("/users")
			{
				users.GET("", handlers.GetUsers)
				users.POST("", handlers.CreateUser)
				users.PUT("/:id", handlers.UpdateUser)
				users.PATCH("/:id/status", handlers.UpdateUserStatus)
				users.DELETE("/:id", handlers.DeleteUser)
			}
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

			if path != "/" {
				f, err := distFS.Open(path[1:])
				if err == nil {
					f.Close()
					c.FileFromFS(path, httpFS)
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
	srv := &http.Server{
		Addr:              port,
		Handler:           r,
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

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}
	log.Println("Server exited gracefully")
}
