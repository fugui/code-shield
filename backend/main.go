package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"

	"code-shield/cron_jobs"
	"code-shield/handlers"
	"code-shield/models"
	
	"github.com/gin-gonic/gin"
)

//go:embed frontend/dist/*
var frontendFS embed.FS

func main() {
	// Initialize database
	models.InitDB()

	// Start cron jobs
	cron_jobs.StartCronJobs()

	// Initialize Gin engine
	r := gin.Default()

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
		api.PATCH("/teams/:id", handlers.UpdateTeam)
		api.DELETE("/teams/:id", handlers.DeleteTeam)

		api.GET("/members", handlers.GetMembers)
		api.POST("/members", handlers.CreateMember)
		api.POST("/members/import", handlers.ImportMembers)
		api.PATCH("/members/:id", handlers.UpdateMember)
		api.DELETE("/members/:id", handlers.DeleteMember)

		api.GET("/repos", handlers.GetRepos)
		api.POST("/repos", handlers.CreateRepo)
		api.DELETE("/repos/:id", handlers.DeleteRepo)
		api.POST("/repos/import", handlers.ImportRepos)

		api.GET("/config", handlers.GetConfig)
		api.PATCH("/config", handlers.UpdateConfig)

		api.GET("/reviews", handlers.GetReviews)
		api.POST("/reviews/trigger", handlers.TriggerReview)
		api.POST("/reviews/:id/notify", handlers.TriggerManualNotification)
		api.GET("/reviews/:id", handlers.GetReviewDetails)
		api.GET("/reviews/:id/report", handlers.GetReviewReportMarkdown)

		api.GET("/issues", handlers.GetIssues)
		api.POST("/issues", handlers.CreateIssue)
		api.PATCH("/issues/:id", handlers.UpdateIssue)

		// Admin only routes for user management
		admin := api.Group("/users")
		admin.Use(handlers.AdminMiddleware())
		{
			admin.GET("", handlers.GetUsers)
			admin.POST("", handlers.CreateUser)
			admin.PATCH("/:id/status", handlers.UpdateUserStatus)
			admin.DELETE("/:id", handlers.DeleteUser)
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

	// Start server
	log.Println("Starting server on :8080...")
	if err := r.Run(":8080"); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
