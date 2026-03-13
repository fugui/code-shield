package cron_jobs

import (
	"encoding/json"
	"log"

	"code-shield/models"
	"code-shield/services"

	"github.com/robfig/cron/v3"
)

var globalCron *cron.Cron

func StartCronJobs() {
	globalCron = cron.New()
	globalCron.Start()
	log.Println("[Cron] Cron scheduler started.")

	SyncSchedules()
}

// SyncSchedules clears existing jobs and reloads them from the database
func SyncSchedules() {
	if globalCron == nil {
		return
	}

	// Remove all existing jobs
	for _, entry := range globalCron.Entries() {
		globalCron.Remove(entry.ID)
	}

	var schedules []models.ScheduleConfig
	if err := models.DB.Where("is_active = ?", true).Find(&schedules).Error; err != nil {
		log.Printf("[Cron] Failed to fetch active schedules: %v\n", err)
		return
	}

	var sysConfig models.SystemConfig
	models.DB.First(&sysConfig, 1)

	for _, schedule := range schedules {
		// Create a copy of the schedule for the closure
		sched := schedule

		_, err := globalCron.AddFunc(sched.CronExpr, func() {
			log.Printf("[Cron] Triggering schedule: %s (ID: %d)\n", sched.Name, sched.ID)

			// Determine which repos to run against based on TargetMode
			query := models.DB.Model(&models.Repository{}).Where("is_active = ?", true)

			switch sched.TargetMode {
			case "all":
				// no additional filters
			case "service_group":
				var groups []string
				json.Unmarshal(sched.TargetValues, &groups)
				if len(groups) > 0 {
					query = query.Where("service_group IN ?", groups)
				}
			case "team":
				var teamIDs []uint
				json.Unmarshal(sched.TargetValues, &teamIDs)
				if len(teamIDs) > 0 {
					query = query.Where("team_id IN ?", teamIDs)
				}
			case "specific":
				var repoIDs []uint
				json.Unmarshal(sched.TargetValues, &repoIDs)
				if len(repoIDs) > 0 {
					query = query.Where("id IN ?", repoIDs)
				}
			}

			var repos []models.Repository
			if err := query.Find(&repos).Error; err != nil {
				log.Printf("[Cron] Failed to fetch repos for schedule %d: %v\n", sched.ID, err)
				return
			}

			log.Printf("[Cron] Schedule %d found %d repositories to scan.\n", sched.ID, len(repos))
			for _, repo := range repos {
				schedID := sched.ID // Create local copy for pointer
				services.EnqueueReviewTask(&schedID, repo.ID, repo.URL, sysConfig.AutoNotify, "cron")
			}
		})

		if err != nil {
			log.Printf("[Cron] Failed to schedule %s (%s): %v\n", sched.Name, sched.CronExpr, err)
		} else {
			log.Printf("[Cron] Scheduled %s: %s\n", sched.Name, sched.CronExpr)
		}
	}
}
