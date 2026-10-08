package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"code-common/backend/gormdb"
	"code-shield/models"
)

type ConfigFlags struct {
	ConfigPath string
	Execute    bool
	BatchSize  int
	RepoID     uint
	TaskTypeID uint
}

type BackfillStats struct {
	TotalCandidates int64
	Processed       int64
	Updated         int64
	NotFound        int64
	Failed          int64
}

func main() {
	configPath := flag.String("config", "config.yaml", "配置文件路径")
	execute := flag.Bool("execute", false, "真实执行开关（默认 dry-run）")
	batchSize := flag.Int("batch-size", 200, "每批处理数量（默认 200）")
	repoID := flag.Uint("repo-id", 0, "仅处理指定代码仓 ID（0 为全部）")
	taskTypeID := flag.Uint("task-type-id", 0, "仅处理指定任务类型 ID（0 为全部）")
	flag.Parse()

	flags := ConfigFlags{
		ConfigPath: *configPath,
		Execute:    *execute,
		BatchSize:  *batchSize,
		RepoID:     *repoID,
		TaskTypeID: *taskTypeID,
	}

	if err := run(flags); err != nil {
		log.Fatalf("backfill failed: %v", err)
	}
}

func run(flags ConfigFlags) error {
	if err := models.LoadConfig(flags.ConfigPath); err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	db, err := gormdb.Connect(models.AppConfig.Database, gormdb.Options{
		ServiceName: "Shield-DefectTextBackfill",
	})
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}

	if flags.Execute {
		if err := db.AutoMigrate(&models.Defect{}); err != nil {
			return fmt.Errorf("migrate defects: %w", err)
		}
	}

	query := db.Model(&models.Defect{}).Where("title = '' OR title IS NULL")
	if flags.RepoID > 0 {
		query = query.Where("repo_id = ?", flags.RepoID)
	}
	if flags.TaskTypeID > 0 {
		query = query.Where("task_type_id = ?", flags.TaskTypeID)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return fmt.Errorf("count candidate defects: %w", err)
	}

	log.Printf("[Backfill] Found %d defects needing text backfill (execute=%v, batchSize=%d)...", total, flags.Execute, flags.BatchSize)
	if total == 0 {
		log.Println("[Backfill] No candidate defects found. Everything is up to date.")
		return nil
	}

	stats := BackfillStats{TotalCandidates: total}
	started := time.Now()

	batchSize := flags.BatchSize
	if batchSize <= 0 {
		batchSize = 200
	}

	var lastID uint = 0
	for {
		var defects []models.Defect
		batchQuery := db.Where("id > ? AND (title = '' OR title IS NULL)", lastID)
		if flags.RepoID > 0 {
			batchQuery = batchQuery.Where("repo_id = ?", flags.RepoID)
		}
		if flags.TaskTypeID > 0 {
			batchQuery = batchQuery.Where("task_type_id = ?", flags.TaskTypeID)
		}

		if err := batchQuery.Order("id ASC").Limit(batchSize).Find(&defects).Error; err != nil {
			return fmt.Errorf("find candidate defects batch: %w", err)
		}

		if len(defects) == 0 {
			break
		}

		for _, d := range defects {
			lastID = d.ID
			stats.Processed++

			var finding models.AnalysisFinding
			found := false

			// 优先通过最近一次关联的 DefectObservation 查找 Finding
			var obs models.DefectObservation
			if err := db.Where("defect_id = ?", d.ID).Order("id DESC").First(&obs).Error; err == nil {
				if obs.ObservationGroupUID != "" {
					if err := db.Where("task_report_id = ? AND observation_group_uid = ?", obs.ReportID, obs.ObservationGroupUID).First(&finding).Error; err == nil {
						found = true
					}
				}
				if !found && d.NormPath != "" {
					if err := db.Where("task_report_id = ? AND file_path = ?", obs.ReportID, d.NormPath).First(&finding).Error; err == nil {
						found = true
					}
				}
			}

			// 回退通过 FirstReportID 查找
			if !found && d.FirstReportID > 0 && d.NormPath != "" {
				if err := db.Where("task_report_id = ? AND file_path = ?", d.FirstReportID, d.NormPath).First(&finding).Error; err == nil {
					found = true
				}
			}

			if !found {
				stats.NotFound++
				continue
			}

			updates := map[string]interface{}{
				"title":          finding.Title,
				"category":       finding.Category,
				"code_snippet":   finding.CodeSnippet,
				"suggestion":     finding.Suggestion,
				"detail_summary": finding.Detail,
			}

			if flags.Execute {
				if err := db.Model(&models.Defect{}).Where("id = ?", d.ID).Updates(updates).Error; err != nil {
					log.Printf("ERROR updating defect #%d: %v", d.ID, err)
					stats.Failed++
					continue
				}
			}
			stats.Updated++
		}

		log.Printf("[Backfill] Processed %d / %d defects (updated: %d, notFound: %d, failed: %d)...",
			stats.Processed, total, stats.Updated, stats.NotFound, stats.Failed)
	}

	log.Printf("[Backfill] Finished in %v. Total: %d, Processed: %d, Updated: %d, NotFound: %d, Failed: %d (Execute=%v)",
		time.Since(started), stats.TotalCandidates, stats.Processed, stats.Updated, stats.NotFound, stats.Failed, flags.Execute)
	return nil
}
