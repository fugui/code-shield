package services

import (
	"code-shield/models"
	"path/filepath"
	"strings"
	"time"
)

// DiffAndEnrichFindings 执行跨任务缺陷增量比对与状态机打标（内置扫描范围守卫，杜绝假修复误判）
func DiffAndEnrichFindings(repoID uint, taskReportID uint, taskTypeID uint, scannedFiles []string, findings []models.AnalysisFinding) ([]models.AnalysisFinding, error) {
	if models.DB == nil {
		return findings, nil
	}

	now := time.Now()
	var enrichedList []models.AnalysisFinding

	// 1. 构建本次成功扫描的有效文件集合 (用于扫描范围守卫 Scan Scope Guard)
	scannedFileSet := make(map[string]bool, len(scannedFiles))
	for _, f := range scannedFiles {
		scannedFileSet[strings.ToLower(filepath.ToSlash(f))] = true
	}

	// 2. 查询该仓库在该任务类型下的全部历史指纹记录
	var existingRecords []models.DefectFingerprintRecord
	models.DB.Where("repo_id = ? AND task_type_id = ?", repoID, taskTypeID).Find(&existingRecords)

	recordMap := make(map[string]*models.DefectFingerprintRecord, len(existingRecords))
	scopeFallbackMap := make(map[string]*models.DefectFingerprintRecord)

	for i := range existingRecords {
		rec := &existingRecords[i]
		recordMap[rec.Fingerprint] = rec
		// 建立 L2 弱指纹索引
		weakKey := CalculateWeakScopeFingerprint(repoID, taskTypeID, rec.FilePath, rec.ScopeSymbol, rec.Category)
		scopeFallbackMap[weakKey] = rec
	}

	seenInThisScan := make(map[string]bool)
	var newCount, existedCount, resolvedCount int

	// 3. 遍历本次检出的 Findings 进行增量打标
	for i := range findings {
		f := findings[i]

		// 确保作用域符号存在
		if f.ScopeSymbol == "" {
			f.ScopeSymbol = ExtractScopeSymbol(f.FilePath, f.CodeSnippet)
		}

		// 计算 L1 强指纹
		fp := CalculateDefectFingerprint(repoID, taskTypeID, f.FilePath, f.TriggerLine, f.ScopeSymbol)
		f.Fingerprint = fp
		seenInThisScan[fp] = true

		if record, exists := recordMap[fp]; exists {
			// L1 强指纹精准命中
			if record.Status == "RESOLVED" {
				f.DiffStatus = models.DiffStatusReopened // 复发
			} else {
				f.DiffStatus = models.DiffStatusExisted // 存量
			}
			existedCount++

			// 更新活跃信息
			models.DB.Model(record).Updates(map[string]interface{}{
				"last_seen_at": now,
				"last_task_id": taskReportID,
				"status":       "ACTIVE",
			})
		} else {
			// 尝试 L2 弱指纹容错匹配
			weakKey := CalculateWeakScopeFingerprint(repoID, taskTypeID, f.FilePath, f.ScopeSymbol, f.Category)
			if weakRecord, weakExists := scopeFallbackMap[weakKey]; weakExists {
				f.DiffStatus = models.DiffStatusExisted
				existedCount++
				models.DB.Model(weakRecord).Updates(map[string]interface{}{
					"fingerprint":  fp,
					"last_seen_at": now,
					"last_task_id": taskReportID,
					"status":       "ACTIVE",
				})
			} else {
				// 本次全新发现
				f.DiffStatus = models.DiffStatusNew
				newCount++

				newRecord := models.DefectFingerprintRecord{
					RepoID:         repoID,
					TaskTypeID:     taskTypeID,
					Fingerprint:    fp,
					FilePath:       f.FilePath,
					ScopeSymbol:    f.ScopeSymbol,
					Category:       f.Category,
					Status:         "ACTIVE",
					FeedbackStatus: "UNREVIEWED",
					FirstTaskID:    taskReportID,
					LastTaskID:     taskReportID,
					FirstSeenAt:    now,
					LastSeenAt:     now,
				}
				models.DB.Create(&newRecord)
			}
		}

		enrichedList = append(enrichedList, f)
	}

	// 4. 识别已修复缺陷 (【范围守卫】: 仅当该文件被本次成功扫描过且指纹未出现时，才标记为已修复)
	for fp, record := range recordMap {
		normRecordPath := strings.ToLower(filepath.ToSlash(record.FilePath))
		if !seenInThisScan[fp] && record.Status == "ACTIVE" && scannedFileSet[normRecordPath] {
			models.DB.Model(record).Updates(map[string]interface{}{
				"status": "RESOLVED",
			})
			resolvedCount++
		}
	}

	// 5. 更新报告维度的增量统计
	models.DB.Model(&models.TaskReport{}).Where("id = ?", taskReportID).Updates(map[string]interface{}{
		"new_defects_count":      newCount,
		"existed_defects_count":  existedCount,
		"resolved_defects_count": resolvedCount,
	})

	return enrichedList, nil
}
