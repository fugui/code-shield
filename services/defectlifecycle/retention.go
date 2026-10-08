package defectlifecycle

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"code-shield/models"

	"gorm.io/gorm"
)

// PurgeExpiredScanSnapshots 根据保底基线与 TTL 时间窗口，批量清理过期报告的事实快照数据。
// 保底规则：同一代码仓同任务类型下，始终保留最新的 maxRetainedPerRepo 轮成功/降级快照；
// 仅清理排序在 maxRetainedPerRepo 之后且创建时间早于 retentionDays 的报告所关联的快照（明细条目与事实数据）。
func PurgeExpiredScanSnapshots(db *gorm.DB, retentionDays int, maxRetainedPerRepo int) (int, error) {
	if db == nil {
		return 0, fmt.Errorf("db is nil")
	}
	if retentionDays <= 0 {
		retentionDays = 30
	}
	if maxRetainedPerRepo <= 0 {
		maxRetainedPerRepo = 10
	}

	cutoffTime := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)

	// 使用 CTE 窗口函数识别超期可清理的报告 ID（SQLite 3.25+ 与 PostgreSQL 原生支持）
	const rankedQuery = `
WITH ranked_reports AS (
    SELECT 
        id AS report_id,
        repo_id,
        task_type_id,
        created_at,
        ROW_NUMBER() OVER(PARTITION BY repo_id, task_type_id ORDER BY id DESC) as rn
    FROM task_reports
    WHERE status IN ('success', 'degraded')
)
SELECT report_id 
FROM ranked_reports 
WHERE rn > ? AND created_at < ?
ORDER BY report_id ASC`

	var reportIDs []uint
	err := db.Raw(rankedQuery, maxRetainedPerRepo, cutoffTime).Scan(&reportIDs).Error
	if err != nil {
		log.Printf("[Retention] Window query failed: %v, falling back to manual partition filtering", err)
		reportIDs, err = findExpiredReportIDsFallback(db, maxRetainedPerRepo, cutoffTime)
		if err != nil {
			return 0, fmt.Errorf("find expired report ids: %w", err)
		}
	}

	if len(reportIDs) == 0 {
		return 0, nil
	}

	const batchSize = 50
	totalPurged := 0

	for i := 0; i < len(reportIDs); i += batchSize {
		end := i + batchSize
		if end > len(reportIDs) {
			end = len(reportIDs)
		}
		batch := reportIDs[i:end]

		// 查询涉及报告以获取外部 Manifest 文件路径
		var reports []models.TaskReport
		_ = db.Select("id", "report_path").Where("id IN ?", batch).Find(&reports).Error

		// 在独立短事务中执行物理删除
		err := db.Transaction(func(tx *gorm.DB) error {
			// 1. 删除逐文件覆盖明细
			if err := tx.Where("report_id IN ?", batch).Delete(&models.ScanScopeEntry{}).Error; err != nil {
				return fmt.Errorf("purge scan_scope_entries: %w", err)
			}

			// 2. 删除原始观测事实
			if err := tx.Where("report_id IN ?", batch).Delete(&models.DefectObservation{}).Error; err != nil {
				return fmt.Errorf("purge defect_observations: %w", err)
			}

			// 3. 删除报告 findings 快照
			if err := tx.Where("task_report_id IN ?", batch).Delete(&models.AnalysisFinding{}).Error; err != nil {
				return fmt.Errorf("purge analysis_findings: %w", err)
			}

			// 4. 清理非人工审计事件（严格保留 actor_type = 'HUMAN' 的审计流水）
			if err := tx.Where("report_id IN ? AND actor_type <> ?", batch, "HUMAN").Delete(&models.DefectEvent{}).Error; err != nil {
				return fmt.Errorf("purge system defect_events: %w", err)
			}

			return nil
		})
		if err != nil {
			return totalPurged, fmt.Errorf("purge batch %v: %w", batch, err)
		}

		// 事务提交成功后，物理清理外置 scope manifest 压缩文件
		for _, r := range reports {
			manifestPath := r.GetScopeManifestPath()
			if manifestPath != "" {
				_ = os.Remove(manifestPath)
			}
			if models.AppConfig.Retention.ScopeManifestDir != "" {
				cfgPath := filepath.Join(models.AppConfig.Retention.ScopeManifestDir, fmt.Sprintf("scope_manifest-%d.json.gz", r.ID))
				_ = os.Remove(cfgPath)
			}
		}

		totalPurged += len(batch)

		// 批次间微休眠，让出 DB 锁与 IO 资源
		if end < len(reportIDs) {
			time.Sleep(10 * time.Millisecond)
		}
	}

	// 清理 Tier 4 语义判决缓存（若存在表）
	if db.Migrator().HasTable("defect_ai_verdict_cache") {
		verdictCutoff := time.Now().Add(-60 * 24 * time.Hour)
		_ = db.Table("defect_ai_verdict_cache").Where("created_at < ?", verdictCutoff).Delete(map[string]interface{}{}).Error
	}

	return totalPurged, nil
}

func findExpiredReportIDsFallback(db *gorm.DB, maxRetained int, cutoff time.Time) ([]uint, error) {
	type miniReport struct {
		ID         uint      `gorm:"column:id"`
		RepoID     uint      `gorm:"column:repo_id"`
		TaskTypeID uint      `gorm:"column:task_type_id"`
		CreatedAt  time.Time `gorm:"column:created_at"`
	}
	var reports []miniReport
	if err := db.Table("task_reports").
		Select("id, repo_id, task_type_id, created_at").
		Where("status IN ('success', 'degraded')").
		Order("repo_id ASC, task_type_id ASC, id DESC").
		Find(&reports).Error; err != nil {
		return nil, err
	}

	var expired []uint
	grouped := make(map[string][]miniReport)
	for _, r := range reports {
		key := fmt.Sprintf("%d_%d", r.RepoID, r.TaskTypeID)
		grouped[key] = append(grouped[key], r)
	}

	for _, list := range grouped {
		if len(list) <= maxRetained {
			continue
		}
		for i := maxRetained; i < len(list); i++ {
			if list[i].CreatedAt.Before(cutoff) {
				expired = append(expired, list[i].ID)
			}
		}
	}
	return expired, nil
}

// PurgeOrphanManifestTempFiles 清理指定目录下超过 maxAge 的未转正 Manifest 临时文件
func PurgeOrphanManifestTempFiles(manifestDir string, maxAge time.Duration) int {
	if manifestDir == "" {
		manifestDir = models.AppConfig.Retention.ScopeManifestDir
	}
	if manifestDir == "" {
		manifestDir = filepath.Join(models.AppConfig.GetDataDir(), "reports")
	}
	if maxAge <= 0 {
		maxAge = 24 * time.Hour
	}

	entries, err := os.ReadDir(manifestDir)
	if err != nil {
		return 0
	}

	now := time.Now()
	cleaned := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "scope_manifest-tmp-") && strings.HasSuffix(name, ".json.gz") {
			fullPath := filepath.Join(manifestDir, name)
			info, statErr := entry.Info()
			if statErr != nil {
				continue
			}
			if now.Sub(info.ModTime()) > maxAge {
				if removeErr := os.Remove(fullPath); removeErr == nil {
					cleaned++
				}
			}
		}
	}
	return cleaned
}
