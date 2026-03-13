package cron_jobs

import (
	"log"
	"code-shield/models"
	"code-shield/services"
	
	"github.com/robfig/cron/v3"
)

func StartCronJobs() {
	c := cron.New()
	
	// Example: Run daily at 2:00 AM
	_, err := c.AddFunc("0 2 * * *", func() {
		log.Println("[Cron] Starting daily code review for all repositories...")
		
		var config models.SystemConfig
		models.DB.First(&config, 1)

		var repos []models.Repository
		if err := models.DB.Find(&repos).Error; err != nil {
			log.Printf("[Cron] Failed to fetch repositories: %v\n", err)
			return
		}
		
		for _, repo := range repos {
			// Spawn a background review for each
			// We delay slightly to avoid spiking git/claude simultaneously if many repos
			go services.RunAIReview(repo.ID, repo.URL, config.AutoNotify)
		}
		
	})
	
	if err != nil {
		log.Printf("Failed to add cron job: %v", err)
		return
	}
	
	c.Start()
	log.Println("Cron jobs started successfully.")
}
