package defects

import (
	"code-shield/models"
	"fmt"
	"log"
	"path/filepath"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

// MigrationResult 迁移执行汇总统计
type MigrationResult struct {
	TotalRecords  int
	MigratedCount int
	MergedCount   int
	SkippedCount  int
}

// RunFingerprintMigration 执行存量指纹原地物理重算与平滑升级（支持事务与 Dry-Run 试跑）
func RunFingerprintMigration(db *gorm.DB, repoRoot string, dryRun bool) (*MigrationResult, error) {
	if db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}

	var records []models.DefectFingerprintRecord
	if err := db.Where("status = ?", models.DiffStatusActive).Find(&records).Error; err != nil {
		return nil, fmt.Errorf("failed to query active defect records: %w", err)
	}

	res := &MigrationResult{
		TotalRecords: len(records),
	}

	log.Printf("[Migration] Found %d active defect records to migrate...\n", len(records))

	tx := db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	occupiedFPs := make(map[string]*models.DefectFingerprintRecord)

	for i := range records {
		rec := &records[i]
		normPath := strings.ToLower(filepath.ToSlash(rec.FilePath))
		cleanTrigger := CleanSourceToken(rec.TriggerLine)
		normScope := NormalizeScopeSymbol(rec.ScopeSymbol)

		// 1. 若提供了真实源码目录，通过 SourceEnricher 物理校准行号与代码 Token
		if repoRoot != "" {
			anchor, err := EnrichSourceAnchor(repoRoot, rec.FilePath, strconv.Itoa(rec.LineStart), rec.TriggerLine)
			if err == nil {
				normPath = anchor.NormalizedPath
				normScope = anchor.NormalizedScope
				cleanTrigger = anchor.PhysicalToken
				rec.LineStart = anchor.StartLine
				rec.LineEnd = anchor.EndLine
				rec.ScopeBodyHash = anchor.ScopeBodyHash
				fullPath := filepath.Join(repoRoot, normPath)
				if h, err := ComputeFileSHA256(fullPath); err == nil {
					rec.FileHashSnapshot = h
				}
			}
		}

		// 2. 依据新一代纯物理公式计算确定性 L1 强指纹 (不含 Category)
		newFP := CalculateDefectFingerprint(rec.RepoID, rec.TaskTypeID, normPath, cleanTrigger, normScope)

		// 3. 处理潜在收敛冲突 (旧算法因 category 漂移拆成多条，新算法收敛为同一条)
		if existing, conflict := occupiedFPs[newFP]; conflict {
			// 智能合并：若当前记录有人工反馈 (FALSE_POSITIVE/WONT_FIX)，优先继承人工反馈
			if (rec.FeedbackStatus == "FALSE_POSITIVE" || rec.FeedbackStatus == "WONT_FIX") &&
				existing.FeedbackStatus == "UNREVIEWED" {
				existing.FeedbackStatus = rec.FeedbackStatus
				existing.FeedbackReason = rec.FeedbackReason
				existing.FeedbackUserID = rec.FeedbackUserID
				existing.FeedbackAt = rec.FeedbackAt
			}
			// 将冗余的旧记录安全标记为已合并归档
			rec.Status = "MERGED_ARCHIVED"
			res.MergedCount++
			if !dryRun {
				tx.Save(rec)
				tx.Save(existing)
			}
			continue
		}

		// 4. 原地刷新字段
		rec.Fingerprint = newFP
		rec.FilePath = normPath
		rec.ScopeSymbol = normScope
		rec.TriggerLine = cleanTrigger
		rec.MissedCount = 0

		occupiedFPs[newFP] = rec
		res.MigratedCount++

		if !dryRun {
			if err := tx.Save(rec).Error; err != nil {
				tx.Rollback()
				return nil, fmt.Errorf("failed to save migrated record #%d: %w", rec.ID, err)
			}
		}
	}

	if dryRun {
		tx.Rollback()
		log.Printf("[Migration DRY-RUN] Success: %d records recalculated, %d duplicate records merged.\n",
			res.MigratedCount, res.MergedCount)
		return res, nil
	}

	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("transaction commit failed: %w", err)
	}

	log.Printf("[Migration LIVE] Complete! %d records updated, %d records merged. Historical feedbacks 100%% preserved.\n",
		res.MigratedCount, res.MergedCount)
	return res, nil
}
