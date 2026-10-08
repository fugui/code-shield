package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"code-common/backend/gormdb"
	"code-shield/models"
	"code-shield/services/coverage"

	"gorm.io/gorm"
)

type ConfigFlags struct {
	ConfigPath string
	Execute    bool
	RepoID     uint
	TaskTypeID uint
	TaskID     uint
}

type synthesisCoverageArtifact struct {
	Meta struct {
		ReportID     uint             `json:"report_id"`
		ScanCoverage coverage.Summary `json:"scan_coverage"`
	} `json:"meta"`
}

type BackfillStats struct {
	Candidates      int
	Eligible        int
	AlreadyNoScope  int
	MissingArtifact int
	WrongArtifactID int
	Ineligible      int
	Updated         int
	Failed          int
}

func main() {
	configPath := flag.String("config", "config.yaml", "配置文件路径")
	execute := flag.Bool("execute", false, "真实执行开关（默认 dry-run）")
	repoID := flag.Uint("repo-id", 0, "仅处理指定代码仓 ID（0 为全部）")
	taskTypeID := flag.Uint("task-type-id", 0, "仅处理指定任务类型 ID（0 为全部）")
	taskID := flag.Uint("task-id", 0, "仅处理指定报告 ID（0 为全部）")
	flag.Parse()

	flags := ConfigFlags{
		ConfigPath: *configPath,
		Execute:    *execute,
		RepoID:     *repoID,
		TaskTypeID: *taskTypeID,
		TaskID:     *taskID,
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
		ServiceName: "Shield-NoScopeBackfill",
	})
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}

	if flags.Execute {
		if err := db.AutoMigrate(&models.TaskReport{}); err != nil {
			return fmt.Errorf("migrate task_reports: %w", err)
		}
	}

	reports, err := fetchCandidates(db, flags)
	if err != nil {
		return fmt.Errorf("fetch candidates: %w", err)
	}

	stats := BackfillStats{Candidates: len(reports)}
	started := time.Now()
	for _, report := range reports {
		if report.CoverageNotApplicable {
			stats.AlreadyNoScope++
			continue
		}

		artifactPath := report.GetSynthesisJSONPath()
		artifactBytes, err := os.ReadFile(artifactPath)
		if err != nil {
			stats.MissingArtifact++
			log.Printf("SKIP #%d: read synthesis artifact: %v", report.ID, err)
			continue
		}

		var artifact synthesisCoverageArtifact
		if err := json.Unmarshal(artifactBytes, &artifact); err != nil {
			stats.Ineligible++
			log.Printf("SKIP #%d: parse synthesis artifact: %v", report.ID, err)
			continue
		}
		if artifact.Meta.ReportID != report.ID {
			stats.WrongArtifactID++
			log.Printf("SKIP #%d: artifact report_id=%d", report.ID, artifact.Meta.ReportID)
			continue
		}

		if !isZeroScopeArtifact(&artifact.Meta.ScanCoverage) {
			stats.Ineligible++
			log.Printf("SKIP #%d: not a zero-scope artifact (planned=%d scanned=%d failed_chunks=%d)",
				report.ID,
				artifact.Meta.ScanCoverage.PlannedFiles,
				artifact.Meta.ScanCoverage.ScannedFiles,
				artifact.Meta.ScanCoverage.FailedChunks)
			continue
		}

		stats.Eligible++
		if !flags.Execute {
			log.Printf("DRY-RUN #%d: would set status=success coverage_not_applicable=true", report.ID)
			continue
		}

		updates := map[string]interface{}{
			"status":                  models.StatusSuccess,
			"coverage_complete":       false,
			"coverage_degraded":       false,
			"coverage_not_applicable": true,
		}
		if _, err := models.UpdateActiveTaskReport(db, report.ID, updates); err != nil {
			stats.Failed++
			log.Printf("ERROR #%d: update report: %v", report.ID, err)
			continue
		}
		stats.Updated++
		log.Printf("UPDATED #%d: no-scope report restored to success", report.ID)
	}

	mode := "DRY-RUN"
	if flags.Execute {
		mode = "EXECUTE"
	}
	log.Printf("[%s] candidates=%d eligible=%d updated=%d already_no_scope=%d missing_artifact=%d wrong_artifact_id=%d ineligible=%d failed=%d elapsed=%s",
		mode,
		stats.Candidates,
		stats.Eligible,
		stats.Updated,
		stats.AlreadyNoScope,
		stats.MissingArtifact,
		stats.WrongArtifactID,
		stats.Ineligible,
		stats.Failed,
		time.Since(started),
	)
	if flags.Execute && stats.Failed > 0 {
		return fmt.Errorf("%d reports failed to update", stats.Failed)
	}
	return nil
}

func fetchCandidates(db *gorm.DB, flags ConfigFlags) ([]models.TaskReport, error) {
	query := db.Preload("Repo").Preload("TaskType").
		Where("status = ?", models.StatusDegraded).
		Where("coverage_degraded = ?", true).
		Order("id ASC")
	if flags.RepoID > 0 {
		query = query.Where("repo_id = ?", flags.RepoID)
	}
	if flags.TaskTypeID > 0 {
		query = query.Where("task_type_id = ?", flags.TaskTypeID)
	}
	if flags.TaskID > 0 {
		query = query.Where("id = ?", flags.TaskID)
	}

	var reports []models.TaskReport
	if err := query.Find(&reports).Error; err != nil {
		return nil, err
	}
	return reports, nil
}

func isZeroScopeArtifact(summary *coverage.Summary) bool {
	return summary != nil &&
		summary.PlannedFiles == 0 &&
		summary.ScannedFiles == 0 &&
		summary.SkippedFiles == 0 &&
		summary.FailedFiles == 0 &&
		summary.UnknownFiles == 0 &&
		summary.SuccessChunks == 0 &&
		summary.FailedChunks == 0 &&
		summary.SkippedChunks == 0 &&
		summary.UnknownChunks == 0
}
