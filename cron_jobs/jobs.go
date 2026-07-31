package cron_jobs

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

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

	for _, schedule := range schedules {
		// Create a copy of the schedule for the closure
		sched := schedule

		_, err := globalCron.AddFunc(sched.CronExpr, func() {
			ExecuteScheduleContext(sched.ID, "cron")
		})

		if err != nil {
			log.Printf("[Cron] Failed to schedule %s (%s): %v\n", sched.Name, sched.CronExpr, err)
		} else {
			log.Printf("[Cron] Scheduled %s: %s\n", sched.Name, sched.CronExpr)
		}
	}
}

// ExecuteScheduleContext triggers a schedule immediately
func ExecuteScheduleContext(schedID uint, triggerSource string) error {
	return ExecuteScheduleContextWithOperator(schedID, triggerSource, nil, "", "")
}

// ExecuteScheduleContextWithOperator triggers a schedule with operator details
func ExecuteScheduleContextWithOperator(schedID uint, triggerSource string, operatorID *uint, operatorName string, clientIP string) error {
	var sched models.ScheduleConfig
	if err := models.DB.Preload("TaskType").First(&sched, schedID).Error; err != nil {
		return err
	}

	log.Printf("[Cron-%s] Triggering schedule: %s (ID: %d, TaskTypeID: %d)\n", triggerSource, sched.Name, sched.ID, sched.TaskTypeID)

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
			query = query.Where("department_id IN ?", teamIDs)
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
		log.Printf("[Cron-%s] Failed to fetch repos for schedule %d: %v\n", triggerSource, sched.ID, err)
		return err
	}

	log.Printf("[Cron-%s] Schedule %d found %d repositories to scan.\n", triggerSource, sched.ID, len(repos))

	// Determine trigger type and operator name
	triggerType := "cron_auto"
	opName := "系统定时任务 (System Cron)"
	if triggerSource == "manual" {
		triggerType = "cron_manual"
		if operatorName != "" {
			opName = operatorName
		} else {
			opName = "管理员手动触发"
		}
	}

	targetSummary := fmt.Sprintf("定时策略: %s (覆盖 %d 个代码仓)", sched.Name, len(repos))

	// Create TaskTriggerLog
	batchNo := fmt.Sprintf("TRG-%s-%d", time.Now().Format("20060102150405"), sched.ID)
	sID := sched.ID
	triggerLog := models.TaskTriggerLog{
		TriggerBatch:  batchNo,
		TriggerType:   triggerType,
		OperatorID:    operatorID,
		OperatorName:  opName,
		TaskTypeID:    sched.TaskTypeID,
		TargetMode:    sched.TargetMode,
		TargetSummary: targetSummary,
		FilterParams:  sched.TargetValues,
		ScheduleID:    &sID,
		TotalRepos:    len(repos),
		ClientIP:      clientIP,
		Remark:        fmt.Sprintf("策略 Cron 表达式: %s", sched.CronExpr),
		CreatedAt:     time.Now(),
	}

	if err := models.DB.Create(&triggerLog).Error; err != nil {
		log.Printf("[Cron-%s] Failed to create TaskTriggerLog: %v\n", triggerSource, err)
	}

	// Parse run params from schedule config
	var runParams models.RunParams
	if len(sched.RunParams) > 0 {
		json.Unmarshal(sched.RunParams, &runParams)
	}

	successCount := 0
	skipCount := 0

	for _, repo := range repos {
		var tLogID *uint
		if triggerLog.ID > 0 {
			tLogID = &triggerLog.ID
		}
		ok := services.EnqueueTaskWithTriggerLog(&sID, tLogID, repo.ID, repo.URL, sched.TaskTypeID, sched.AutoNotify, triggerSource, runParams)
		if ok {
			successCount++
		} else {
			skipCount++
		}
	}

	if triggerLog.ID > 0 {
		models.DB.Model(&triggerLog).Updates(map[string]interface{}{
			"success_count": successCount,
			"skip_count":    skipCount,
		})
	}

	return nil
}
