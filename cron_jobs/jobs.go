package cron_jobs

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"code-shield/models"
	"code-shield/services"
	"code-shield/services/defectlifecycle"
	"code-shield/services/governance"

	"github.com/robfig/cron/v3"
)

var (
	globalCron       *cron.Cron
	scheduleEntryIDs []cron.EntryID
)

func StartCronJobs() {
	globalCron = cron.New()
	globalCron.Start()
	log.Println("[Cron] Cron scheduler started.")

	// 注册每天凌晨 03:30 自动执行的磁盘临时文件 GC 清理任务
	_, err := globalCron.AddFunc("30 3 * * *", func() {
		retentionDays := models.AppConfig.AI.Debate.LogRetentionDays
		if retentionDays <= 0 {
			retentionDays = 7
		}
		services.CleanExpiredTempArtifacts(retentionDays)
	})
	if err != nil {
		log.Printf("[Cron] Failed to register disk GC cron job: %v\n", err)
	} else {
		log.Println("[Cron] Registered daily temp artifact disk GC cron (03:30 AM).")
	}

	_, err = globalCron.AddFunc("0 4 * * *", RunLedgerRetentionGCJob)
	if err != nil {
		log.Printf("[Cron] Failed to register ledger retention GC cron job: %v\n", err)
	} else {
		log.Println("[Cron] Registered daily ledger retention GC cron (04:00 AM).")
	}

	_, err = globalCron.AddFunc("30 4 * * *", RunCategoryRegressionTrendJob)
	if err != nil {
		log.Printf("[Cron] Failed to register category semantic regression cron job: %v\n", err)
	} else {
		log.Println("[Cron] Registered daily category semantic regression cron (04:30 AM).")
	}

	_, err = globalCron.AddFunc("0 5 1 * *", BuildCategoryConfusionMatrixJob)
	if err != nil {
		log.Printf("[Cron] Failed to register category confusion matrix cron job: %v\n", err)
	} else {
		log.Println("[Cron] Registered monthly category confusion matrix cron (05:00 AM, day 1).")
	}

	SyncSchedules()
}

// RunLedgerRetentionGCJob 每日凌晨执行缺陷台账与快照生命周期 TTL 归档清理
func RunLedgerRetentionGCJob() {
	if models.DB == nil {
		return
	}
	retentionDays := models.AppConfig.Retention.LedgerRetentionDays
	if retentionDays <= 0 {
		retentionDays = 30
	}
	maxRetainedScans := models.AppConfig.Retention.MaxRetainedScansPerRepo
	if maxRetainedScans <= 0 {
		maxRetainedScans = 10
	}

	log.Println("[GC] Starting ledger retention and snapshot cleanup...")
	cleanedReports, err := defectlifecycle.PurgeExpiredScanSnapshots(models.DB, retentionDays, maxRetainedScans)
	if err != nil {
		log.Printf("[GC] Scan snapshot cleanup failed: %v", err)
		return
	}

	// 清理超过 24 小时的未关联孤儿 Manifest 临时文件
	cleanedOrphans := defectlifecycle.PurgeOrphanManifestTempFiles(
		models.AppConfig.Retention.ScopeManifestDir, 24*time.Hour)
	if cleanedOrphans > 0 {
		log.Printf("[GC] Cleaned %d orphaned manifest temp files.", cleanedOrphans)
	}

	// 针对 PostgreSQL 执行非阻塞式 VACUUM ANALYZE，促使引擎及时回收 Dead Tuples 并刷新统计信息
	if models.DB.Dialector.Name() == "postgres" {
		_ = models.DB.Exec("VACUUM (ANALYZE) scan_scope_entries, defect_observations, analysis_findings;").Error
	}
	log.Printf("[GC] Ledger retention cleanup completed. Pruned snapshots for %d expired reports.", cleanedReports)
}

func RunCategoryRegressionTrendJob() {
	if models.DB == nil {
		return
	}
	synced, runCount, err := governance.RunCategoryGovernanceJobs(
		models.DB, governance.DefaultCategoryRegressionCorpusPath(), nil,
	)
	if err != nil {
		log.Printf("[Cron] Category semantic regression failed: synced=%d run=%d error=%v\n", synced, runCount, err)
		return
	}
	log.Printf("[Cron] Category semantic regression completed: synced=%d run=%d\n", synced, runCount)
}

func BuildCategoryConfusionMatrixJob() {
	if models.DB == nil {
		return
	}
	now := time.Now()
	periodEnd := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	periodStart := periodEnd.AddDate(0, -1, 0)
	matrices, err := governance.BuildMonthlyCategoryConfusionMatrices(models.DB, periodStart, periodEnd)
	if err != nil {
		log.Printf("[Cron] Category confusion matrix failed: matrices=%d error=%v\n", len(matrices), err)
		return
	}
	log.Printf("[Cron] Category confusion matrix completed: matrices=%d\n", len(matrices))
}

// SyncSchedules clears existing dynamic jobs and reloads them from the database
func SyncSchedules() {
	if globalCron == nil {
		return
	}

	// Remove all previous user-defined schedule jobs
	for _, entryID := range scheduleEntryIDs {
		globalCron.Remove(entryID)
	}
	scheduleEntryIDs = nil

	var schedules []models.ScheduleConfig
	if err := models.DB.Where("is_active = ?", true).Find(&schedules).Error; err != nil {
		log.Printf("[Cron] Failed to fetch active schedules: %v\n", err)
		return
	}

	for _, schedule := range schedules {
		// Create a copy of the schedule for the closure
		sched := schedule

		entryID, err := globalCron.AddFunc(sched.CronExpr, func() {
			ExecuteScheduleContext(sched.ID, "cron")
		})

		if err != nil {
			log.Printf("[Cron] Failed to schedule %s (%s): %v\n", sched.Name, sched.CronExpr, err)
		} else {
			scheduleEntryIDs = append(scheduleEntryIDs, entryID)
			log.Printf("[Cron] Scheduled %s: %s (EntryID: %d)\n", sched.Name, sched.CronExpr, entryID)
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
