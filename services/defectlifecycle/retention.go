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
// 针对 failed / skipped / cancelled 等异常任务，若创建时间早于 nonSuccessRetentionDays（默认 7 天），直接清理关联明细。
func PurgeExpiredScanSnapshots(db *gorm.DB, retentionDays int, maxRetainedPerRepo int, nonSuccessDays ...int) (int, error) {
	if db == nil {
		return 0, fmt.Errorf("db is nil")
	}
	if retentionDays <= 0 {
		retentionDays = 30
	}
	if maxRetainedPerRepo <= 0 {
		maxRetainedPerRepo = 10
	}
	nonSuccessRetentionDays := 7
	if len(nonSuccessDays) > 0 && nonSuccessDays[0] > 0 {
		nonSuccessRetentionDays = nonSuccessDays[0]
	}

	cutoffTime := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	nonSuccessCutoff := time.Now().Add(-time.Duration(nonSuccessRetentionDays) * 24 * time.Hour)

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
),
baseline_expired AS (
    SELECT report_id 
    FROM ranked_reports 
    WHERE rn > ? AND created_at < ?
),
non_success_expired AS (
    SELECT id AS report_id
    FROM task_reports
    WHERE status IN ('failed', 'skipped', 'cancelled') AND created_at < ?
)
SELECT report_id FROM baseline_expired
UNION
SELECT report_id FROM non_success_expired
ORDER BY report_id ASC`

	var reportIDs []uint
	err := db.Raw(rankedQuery, maxRetainedPerRepo, cutoffTime, nonSuccessCutoff).Scan(&reportIDs).Error
	if err != nil {
		log.Printf("[Retention] Window query failed: %v, falling back to manual partition filtering", err)
		reportIDs, err = findExpiredReportIDsFallback(db, maxRetainedPerRepo, cutoffTime, nonSuccessCutoff)
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

			// 5. 清理构件修复审计流水 (单表大体积治理)
			if err := tx.Where("report_id IN ?", batch).Delete(&models.ArtifactRepairAudit{}).Error; err != nil {
				return fmt.Errorf("purge artifact_repair_audits: %w", err)
			}

			// 6. 清理关联的智能体辩论轨迹
			if err := tx.Where("task_report_id IN ?", batch).Delete(&models.TaskDebateLog{}).Error; err != nil {
				return fmt.Errorf("purge task_debate_logs: %w", err)
			}

			// 7. 清理关联的旧专项缺陷记录
			if err := tx.Where("task_report_id IN ?", batch).Delete(&models.CampaignFinding{}).Error; err != nil {
				return fmt.Errorf("purge campaign_findings: %w", err)
			}

			return nil
		})
		if err != nil {
			return totalPurged, fmt.Errorf("purge batch %v: %w", batch, err)
		}

		// 事务提交成功后，物理清理关联文件
		for _, r := range reports {
			taskDir := r.GetReportDir()
			baseName := filepath.Base(taskDir)
			// 若为独立任务专属目录（如 report-198-fmt），直接整目录原子清理
			if strings.HasPrefix(baseName, fmt.Sprintf("report-%d-", r.ID)) {
				_ = os.RemoveAll(taskDir)
				// 顺便尝试清理可能变空的父级日期目录（若非空则安全忽略）
				_ = os.Remove(filepath.Dir(taskDir))
				continue
			}

			// 兼容历史平铺旧目录结构：逐个清理外置文件
			manifestPath := r.GetScopeManifestPath()
			if manifestPath != "" {
				_ = os.Remove(manifestPath)
			}
			// 额外清理可能遗留的旧格式文件与历史根目录遗留文件
			legacyNamed := filepath.Join(r.GetReportDir(), fmt.Sprintf("scope_manifest-%d.json.gz", r.ID))
			if legacyNamed != manifestPath {
				_ = os.Remove(legacyNamed)
			}
			rootLegacy := filepath.Join(models.AppConfig.GetDataDir(), "reports", fmt.Sprintf("scope_manifest-%d.json.gz", r.ID))
			_ = os.Remove(rootLegacy)
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

func findExpiredReportIDsFallback(db *gorm.DB, maxRetained int, cutoff time.Time, nonSuccessCutoff time.Time) ([]uint, error) {
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

	expiredMap := make(map[uint]struct{})
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
				expiredMap[list[i].ID] = struct{}{}
			}
		}
	}

	// 补充清理超期的失败/跳过/取消任务
	var nonSuccessIDs []uint
	if err := db.Table("task_reports").
		Where("status IN ('failed', 'skipped', 'cancelled') AND created_at < ?", nonSuccessCutoff).
		Pluck("id", &nonSuccessIDs).Error; err == nil {
		for _, id := range nonSuccessIDs {
			expiredMap[id] = struct{}{}
		}
	}

	var expired []uint
	for id := range expiredMap {
		expired = append(expired, id)
	}
	return expired, nil
}

// PurgeExpiredDebateLogs 批量物理清理超过 retentionDays 天数的辩论轨迹数据 (防单表无限膨胀)
func PurgeExpiredDebateLogs(db *gorm.DB, retentionDays int) (int64, error) {
	if db == nil {
		return 0, fmt.Errorf("db is nil")
	}
	if retentionDays <= 0 {
		retentionDays = 30
	}
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	const batchSize = 2000
	var totalPurged int64

	for {
		var ids []uint
		err := db.Model(&models.TaskDebateLog{}).
			Where("created_at < ?", cutoff).
			Limit(batchSize).
			Pluck("id", &ids).Error
		if err != nil {
			return totalPurged, fmt.Errorf("query expired debate log ids: %w", err)
		}
		if len(ids) == 0 {
			break
		}
		res := db.Where("id IN ?", ids).Delete(&models.TaskDebateLog{})
		if res.Error != nil {
			return totalPurged, fmt.Errorf("delete debate logs batch: %w", res.Error)
		}
		totalPurged += res.RowsAffected
		if len(ids) < batchSize {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return totalPurged, nil
}

// PurgeOrphanFindings 自愈清理失去关联报告的孤儿 campaign_findings 记录
func PurgeOrphanFindings(db *gorm.DB) (int64, error) {
	if db == nil {
		return 0, fmt.Errorf("db is nil")
	}
	const batchSize = 1000
	var totalPurged int64

	for {
		var orphanIDs []uint
		err := db.Table("campaign_findings").
			Select("campaign_findings.id").
			Joins("LEFT JOIN task_reports ON campaign_findings.task_report_id = task_reports.id").
			Where("campaign_findings.task_report_id > 0 AND task_reports.id IS NULL").
			Limit(batchSize).
			Pluck("campaign_findings.id", &orphanIDs).Error
		if err != nil {
			return totalPurged, fmt.Errorf("query orphan campaign_findings: %w", err)
		}
		if len(orphanIDs) == 0 {
			break
		}
		res := db.Where("id IN ?", orphanIDs).Delete(&models.CampaignFinding{})
		if res.Error != nil {
			return totalPurged, fmt.Errorf("delete orphan campaign_findings: %w", res.Error)
		}
		totalPurged += res.RowsAffected
		if len(orphanIDs) < batchSize {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return totalPurged, nil
}

// PurgeOrphanManifestTempFiles 递归遍历指定报告根目录，清理超过 maxAge 的未转正 Manifest 临时文件
func PurgeOrphanManifestTempFiles(reportsBaseDir string, maxAge time.Duration) int {
	if reportsBaseDir == "" {
		reportsBaseDir = filepath.Join(models.AppConfig.GetDataDir(), "reports")
	}
	if maxAge <= 0 {
		maxAge = 24 * time.Hour
	}

	if _, err := os.Stat(reportsBaseDir); err != nil {
		return 0
	}

	now := time.Now()
	cleaned := 0

	_ = filepath.Walk(reportsBaseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		name := info.Name()
		// 匹配各类 Manifest 临时文件：
		// 1. 标准规范：report-{id}-scope-manifest.json.gz.tmp.*
		// 2. 遗留规范：scope_manifest-{id}.json.gz.tmp.*
		// 3. 历史单元测试格式：scope_manifest-tmp-*
		isManifestTemp := strings.Contains(name, "-scope-manifest.json.gz.tmp.") ||
			(strings.HasPrefix(name, "scope_manifest-") && strings.Contains(name, ".tmp.")) ||
			strings.HasPrefix(name, "scope_manifest-tmp-")

		if isManifestTemp && now.Sub(info.ModTime()) > maxAge {
			if removeErr := os.Remove(path); removeErr == nil {
				cleaned++
			}
		}
		return nil
	})

	return cleaned
}
