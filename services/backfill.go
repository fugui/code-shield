package services

import (
	"code-shield/models"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// BackfillHistoricalFindings processes all successful historical reports for the 3 specialized campaigns
// and invokes their hooks to populate/sync the campaign tables.
func BackfillHistoricalFindings() error {
	log.Println("[Backfill] Starting backfill of historical findings...")

	// 1. Resolve the three specialized task types
	campaignTaskTypes := []string{"ut_effectiveness", "coredump_risk", "float_comparison"}
	var taskTypes []models.TaskType
	if err := models.DB.Where("name IN ?", campaignTaskTypes).Find(&taskTypes).Error; err != nil {
		return fmt.Errorf("failed to query specialized task types: %w", err)
	}

	taskTypeMap := make(map[uint]models.TaskType)
	for _, tt := range taskTypes {
		taskTypeMap[tt.ID] = tt
	}

	if len(taskTypes) == 0 {
		log.Println("[Backfill] No specialized task types registered. Nothing to backfill.")
		return nil
	}

	// 2. Query all successful reports for these task types ordered by id ASC (chronologically)
	var taskTypeIDs []uint
	for _, tt := range taskTypes {
		taskTypeIDs = append(taskTypeIDs, tt.ID)
	}

	var reports []models.TaskReport
	if err := models.DB.Preload("Repo").Where("task_type_id IN ? AND status = ?", taskTypeIDs, "success").Order("id asc").Find(&reports).Error; err != nil {
		return fmt.Errorf("failed to query successful task reports: %w", err)
	}

	log.Printf("[Backfill] Found %d successful historical task reports to process", len(reports))

	processedCount := 0
	hookTriggeredCount := 0

	for _, r := range reports {
		tt, ok := taskTypeMap[r.TaskTypeID]
		if !ok {
			continue
		}

		log.Printf("[Backfill] Processing Report ID: %d, Repo: %q, Task Type: %q", r.ID, r.Repo.Name, tt.Name)

		// 3. Load findings for this report
		var findings []models.AnalysisFinding

		// First: Try querying database AnalysisFinding table
		if err := models.DB.Where("task_report_id = ?", r.ID).Order("id asc").Find(&findings).Error; err != nil {
			log.Printf("[Backfill] Error querying findings from database for Report ID %d: %v. Will fall back to file.", r.ID, err)
		}

		if len(findings) == 0 && r.ReportPath != "" {
			reportsDir := filepath.Dir(r.GetAbsReportPath())
			safeRepoName := strings.ReplaceAll(r.Repo.Name, "/", "-")
			synthesisPath := filepath.Join(reportsDir, fmt.Sprintf("report-%d-synthesis-%s.json", r.ID, safeRepoName))

			if _, err := os.Stat(synthesisPath); err == nil {
				// File exists! Load and parse it
				data, err := os.ReadFile(synthesisPath)
				if err != nil {
					log.Printf("[Backfill] Failed to read synthesis JSON file %s: %v", synthesisPath, err)
				} else {
					if err := json.Unmarshal(data, &findings); err != nil {
						log.Printf("[Backfill] Failed to unmarshal findings from synthesis JSON file %s: %v", synthesisPath, err)
					} else {
						log.Printf("[Backfill] Loaded %d findings from synthesis JSON file on disk", len(findings))
					}
				}
			} else {
				log.Printf("[Backfill] No findings found in database and synthesis JSON file not found at %s", synthesisPath)
			}
		} else if len(findings) > 0 {
			log.Printf("[Backfill] Loaded %d findings from database AnalysisFinding table", len(findings))
		}

		// 4. Build mock taskContext and execute hooks
		ctx := &taskContext{
			report:   r,
			repo:     r.Repo,
			taskType: tt,
		}

		ctx.executeHooks(findings)
		processedCount++
		hookTriggeredCount += len(findings)
	}

	log.Printf("[Backfill] Backfill complete. Processed %d reports, triggered hooks with %d total findings.", processedCount, hookTriggeredCount)
	return nil
}
