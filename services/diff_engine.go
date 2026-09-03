package services

import (
	"code-shield/models"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm/clause"
)

// DiffAndEnrichFindings 执行跨任务缺陷增量比对与状态机打标（内置双层物理守卫与多轮平滑观察期）
func DiffAndEnrichFindings(repoID uint, taskReportID uint, taskTypeID uint, scannedFiles []string, findings []models.AnalysisFinding, repoRootOpt ...string) ([]models.AnalysisFinding, error) {
	if models.DB == nil {
		return findings, nil
	}

	repoRoot := ""
	if len(repoRootOpt) > 0 {
		repoRoot = repoRootOpt[0]
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
	if err := models.DB.Where("repo_id = ? AND task_type_id = ?", repoID, taskTypeID).Find(&existingRecords).Error; err != nil {
		log.Printf("[DiffEngine] Warning: Failed to query existing fingerprint records: %v", err)
	}

	recordMap := make(map[string]*models.DefectFingerprintRecord, len(existingRecords))

	// 构建按 (normPath, normScope) 分组的历史记录桶，供 Tier 2 空间聚类秒查
	type scopeKey struct {
		Path  string
		Scope string
	}
	scopeBuckets := make(map[scopeKey][]*models.DefectFingerprintRecord)
	for i := range existingRecords {
		rec := &existingRecords[i]
		recordMap[rec.Fingerprint] = rec

		k := scopeKey{
			Path:  strings.ToLower(filepath.ToSlash(rec.FilePath)),
			Scope: NormalizeScopeSymbol(rec.ScopeSymbol),
		}
		scopeBuckets[k] = append(scopeBuckets[k], rec)

		// 若作用域包含多函数拼接 (如 "A / B")，将各子函数作为别名放入桶中
		if strings.Contains(rec.ScopeSymbol, " / ") {
			parts := strings.Split(rec.ScopeSymbol, " / ")
			for _, part := range parts {
				pNorm := NormalizeScopeSymbol(part)
				if pNorm != "" {
					scopeBuckets[scopeKey{Path: k.Path, Scope: pNorm}] = append(scopeBuckets[scopeKey{Path: k.Path, Scope: pNorm}], rec)
				}
			}
		}
	}

	seenInThisScan := make(map[string]bool)
	claimedRecordIDs := make(map[uint]bool) // 单轮独占认领锁，严禁同函数相邻两个缺陷踩踏合并
	var newCount, existedCount, resolvedCount, verifiedPendingCount int

	// 3. 遍历本次检出的 Findings 进行增量打标 (四级空间几何漏斗)
	for i := range findings {
		f := findings[i]

		// 确保作用域符号存在并规范化
		if f.ScopeSymbol == "" {
			f.ScopeSymbol = ExtractScopeSymbol(f.FilePath, f.CodeSnippet)
		}
		normScope := NormalizeScopeSymbol(f.ScopeSymbol)
		if normScope != "" {
			f.ScopeSymbol = normScope
		}
		normPath := strings.ToLower(filepath.ToSlash(f.FilePath))

		// 若有源码目录，通过 SourceEnricher 提取真实物理行与物理 Token
		startLine, endLine := ParseLineNumberRange(f.LineNumber)
		cleanTrigger := CleanSourceToken(f.TriggerLine)
		fileHash := ""
		scopeHash := ""

		if repoRoot != "" {
			fullFilePath := filepath.Join(repoRoot, normPath)
			if h, err := ComputeFileSHA256(fullFilePath); err == nil {
				fileHash = h
			}
			if anchor, err := EnrichSourceAnchor(repoRoot, f.FilePath, f.LineNumber, f.TriggerLine); err == nil {
				startLine = anchor.StartLine
				endLine = anchor.EndLine
				cleanTrigger = anchor.PhysicalToken
				scopeHash = anchor.ScopeBodyHash
				if anchor.NormalizedScope != "" {
					normScope = anchor.NormalizedScope
					f.ScopeSymbol = normScope
				}
			}
		}

		// 计算确定性 L1 物理强指纹 (不含 Category 与任何 LLM 文本)
		fp := CalculateDefectFingerprint(repoID, taskTypeID, f.FilePath, cleanTrigger, normScope)
		f.Fingerprint = fp
		seenInThisScan[fp] = true

		// ── Tier 1: 物理强指纹纳秒级精准 Hash 匹配 ──
		if record, exists := recordMap[fp]; exists && !claimedRecordIDs[record.ID] {
			claimedRecordIDs[record.ID] = true
			if record.Status == models.DiffStatusResolved || record.Status == models.DiffStatusVerifiedPending {
				f.DiffStatus = models.DiffStatusReopened // 复发
			} else {
				f.DiffStatus = models.DiffStatusExisted // 存量
			}
			existedCount++

			updateData := map[string]interface{}{
				"last_seen_at": now,
				"last_task_id": taskReportID,
				"status":       models.DiffStatusActive,
				"missed_count": 0,
			}
			if record.Severity == "" && f.Severity != "" {
				updateData["severity"] = f.Severity
			}
			if err := models.DB.Model(record).Updates(updateData).Error; err != nil {
				log.Printf("[DiffEngine] Warning: Failed to update active fingerprint record ID %d: %v", record.ID, err)
			}
		} else {
			// ── Tier 2: 作用域单体聚类与距离容错 (同 Path + 同 Scope) ──
			var matchedCandidate *models.DefectFingerprintRecord
			k := scopeKey{Path: normPath, Scope: normScope}

			var candidateList []*models.DefectFingerprintRecord
			if cands, ok := scopeBuckets[k]; ok {
				candidateList = append(candidateList, cands...)
			}
			if len(candidateList) == 0 && strings.Contains(normScope, " / ") {
				parts := strings.Split(normScope, " / ")
				for _, part := range parts {
					pNorm := NormalizeScopeSymbol(part)
					if cands, ok := scopeBuckets[scopeKey{Path: normPath, Scope: pNorm}]; ok {
						candidateList = append(candidateList, cands...)
					}
				}
			}

			for _, cand := range candidateList {
				if claimedRecordIDs[cand.ID] {
					continue // 已被认领，跳过
				}

				lineDiff := startLine - cand.LineStart
				if lineDiff < 0 {
					lineDiff = -lineDiff
				}
				tokenSim := CalculateTokenJaccard(cleanTrigger, cand.TriggerLine)
				isSameCat := strings.EqualFold(strings.TrimSpace(f.Category), strings.TrimSpace(cand.Category))

				// 收紧准则：(lineDiff <= 2 && tokenSim > 0.5) || tokenSim > 0.75 || (isSameCat && (lineDiff <= 15 || tokenSim > 0.65))
				if ((lineDiff <= 2 && tokenSim > 0.5) || tokenSim > 0.75) || (isSameCat && (lineDiff <= 15 || tokenSim > 0.65)) {
					matchedCandidate = cand
					break
				}
			}

			if matchedCandidate != nil {
				// Tier 2 命中存量！自愈更新指纹与锚点，继承人工反馈
				claimedRecordIDs[matchedCandidate.ID] = true
				if matchedCandidate.Status == models.DiffStatusResolved || matchedCandidate.Status == models.DiffStatusVerifiedPending {
					f.DiffStatus = models.DiffStatusReopened
				} else {
					f.DiffStatus = models.DiffStatusExisted
				}
				existedCount++

				// 自愈写回
				healData := map[string]interface{}{
					"fingerprint":  fp,
					"trigger_line": cleanTrigger,
					"line_start":   startLine,
					"line_end":     endLine,
					"last_seen_at": now,
					"last_task_id": taskReportID,
					"status":       models.DiffStatusActive,
					"missed_count": 0,
				}
				if f.Severity != "" {
					healData["severity"] = f.Severity
				}
				if fileHash != "" {
					healData["file_hash_snapshot"] = fileHash
				}
				if scopeHash != "" {
					healData["scope_body_hash"] = scopeHash
				}

				if err := models.DB.Model(matchedCandidate).Updates(healData).Error; err != nil {
					log.Printf("[DiffEngine] Warning: Failed to heal record ID %d: %v", matchedCandidate.ID, err)
				}
				// 同步更新内存映射
				recordMap[fp] = matchedCandidate
			} else {
				// ── 全新发现 NEW ──
				f.DiffStatus = models.DiffStatusNew
				newCount++

				newRecord := models.DefectFingerprintRecord{
					RepoID:           repoID,
					TaskTypeID:       taskTypeID,
					Fingerprint:      fp,
					FilePath:         f.FilePath,
					ScopeSymbol:      f.ScopeSymbol,
					Category:         f.Category,
					Severity:         f.Severity,
					Status:           models.DiffStatusActive,
					TriggerLine:      cleanTrigger,
					LineStart:        startLine,
					LineEnd:          endLine,
					FileHashSnapshot: fileHash,
					ScopeBodyHash:    scopeHash,
					MissedCount:      0,
					FeedbackStatus:   "UNREVIEWED",
					FirstTaskID:      taskReportID,
					LastTaskID:       taskReportID,
					FirstSeenAt:      now,
					LastSeenAt:       now,
				}

				// 使用 OnConflict{DoNothing: true} 消除并发扫描或同指纹竞态冲突
				onConflict := clause.OnConflict{
					Columns:   []clause.Column{{Name: "repo_id"}, {Name: "task_type_id"}, {Name: "fingerprint"}},
					DoNothing: true,
				}
				if err := models.DB.Clauses(onConflict).Create(&newRecord).Error; err != nil {
					log.Printf("[DiffEngine] Warning: Failed to create DefectFingerprintRecord: %v", err)
				}

				// 关键修复：立即同步更新内存映射与独占锁，确保循环内后续同指纹/同位置 finding 命中存量，不再重复 Create
				recordMap[fp] = &newRecord
				scopeBuckets[k] = append(scopeBuckets[k], &newRecord)
				if newRecord.ID > 0 {
					claimedRecordIDs[newRecord.ID] = true
				}
			}
		}

		enrichedList = append(enrichedList, f)
	}

	// 4. 识别已修复缺陷 (【双层物理守卫】+【连续2轮平滑观察期】+【高危预关闭缓冲】)
	for fp, record := range recordMap {
		normRecordPath := strings.ToLower(filepath.ToSlash(record.FilePath))
		if seenInThisScan[fp] || record.Status != models.DiffStatusActive || !scannedFileSet[normRecordPath] {
			continue
		}

		// 检查人工豁免标记
		if record.FeedbackStatus == "FALSE_POSITIVE" || record.FeedbackStatus == "WONT_FIX" {
			continue
		}

		// ── 【守卫 L1】检查物理文件 Git Blob / File Hash 是否发生任何实质变动 ──
		var currentFileHash string
		var isFileModified = true // 若无法验证磁盘文件，默认降级为允许检查

		if repoRoot != "" {
			fullPath := filepath.Join(repoRoot, normRecordPath)
			if h, err := ComputeFileSHA256(fullPath); err == nil {
				currentFileHash = h
				if record.FileHashSnapshot != "" && currentFileHash == record.FileHashSnapshot {
					// 物理文件内容 100% 未改动！必定为大模型单次概率漏报，强行拦截假修复！
					log.Printf("[DiffGuard-L1] Defect #%d in %s missed by LLM, but file hash unchanged (%s). PRESERVED ACTIVE.\n",
						record.ID, record.FilePath, currentFileHash)
					continue
				}
				isFileModified = (record.FileHashSnapshot != "" && currentFileHash != record.FileHashSnapshot)
			}
		}

		// ── 【守卫 L2】检查所在函数代码块是否被触碰 ──
		if repoRoot != "" && isFileModified && record.ScopeBodyHash != "" {
			fullPath := filepath.Join(repoRoot, normRecordPath)
			if bytes, err := os.ReadFile(fullPath); err == nil {
				lines := strings.Split(string(bytes), "\n")
				_, currentScopeBody := ExtractScopeAndBodyFromLines(normRecordPath, lines, record.LineStart)
				currentScopeHash := ""
				if currentScopeBody != "" {
					h := ComputeCleanTokenHash(currentScopeBody)
					currentScopeHash = h
				}
				if currentScopeHash != "" && currentScopeHash == record.ScopeBodyHash {
					// 所在函数代码块完全未修改！只是同文件其他函数或注释修改！
					log.Printf("[DiffGuard-L2] Defect #%d in %s missed by LLM. File modified, but scope body untouched. PRESERVED ACTIVE.\n",
						record.ID, record.FilePath)
					continue
				}
			}
		}

		// ── 【平滑观察期】物理代码确实发生了变更，连续未命中计数 MissedCount ──
		const MaxMissedThreshold = 2
		newMissed := record.MissedCount + 1

		if newMissed < MaxMissedThreshold {
			// 处于观察期，暂不关闭
			models.DB.Model(record).Updates(map[string]interface{}{
				"missed_count": newMissed,
			})
			log.Printf("[DiffGuard] Defect #%d in %s in observation period (missed %d/%d). PRESERVED ACTIVE.\n",
				record.ID, record.FilePath, newMissed, MaxMissedThreshold)
			continue
		}

		// ── 【分级关闭决策】连续 2 轮确认隐患消失 ──
		isHighSeverity := (record.Severity == "CRITICAL" || record.Severity == "HIGH" ||
			record.Severity == "致命" || record.Severity == "严重")

		if isHighSeverity {
			// 致命/严重漏洞：流转为 VERIFIED_PENDING（待人工确认 / 预关闭缓冲）
			if err := models.DB.Model(record).Updates(map[string]interface{}{
				"status":       models.DiffStatusVerifiedPending,
				"missed_count": newMissed,
				"resolved_at":  &now,
			}).Error; err != nil {
				log.Printf("[DiffEngine] Warning: Failed to update verified_pending for record ID %d: %v", record.ID, err)
			} else {
				verifiedPendingCount++
				log.Printf("[DiffGuard] High-severity Defect #%d in %s moved to VERIFIED_PENDING for review.\n",
					record.ID, record.FilePath)
			}
		} else {
			// 普通缺陷：正式标记为 RESOLVED 已修复
			if err := models.DB.Model(record).Updates(map[string]interface{}{
				"status":       models.DiffStatusResolved,
				"missed_count": newMissed,
				"resolved_at":  &now,
			}).Error; err != nil {
				log.Printf("[DiffEngine] Warning: Failed to resolve record ID %d: %v", record.ID, err)
			} else {
				resolvedCount++
				log.Printf("[DiffGuard] Defect #%d in %s formally RESOLVED after %d missed scans.\n",
					record.ID, record.FilePath, newMissed)
			}
		}
	}

	log.Printf("[DiffEngine] Incremental summary: New=%d, Existed=%d, Resolved=%d, VerifiedPending=%d (Scanned files: %d)\n",
		newCount, existedCount, resolvedCount, verifiedPendingCount, len(scannedFiles))

	// 5. 更新报告维度的增量统计
	models.DB.Model(&models.TaskReport{}).Where("id = ?", taskReportID).Updates(map[string]interface{}{
		"new_defects_count":      newCount,
		"existed_defects_count":  existedCount,
		"resolved_defects_count": resolvedCount,
	})

	return enrichedList, nil
}

// ComputeCleanTokenHash 辅助计算代码段清洗后的哈希
func ComputeCleanTokenHash(body string) string {
	clean := CleanSourceToken(body)
	if clean == "" {
		return ""
	}
	hash := sha256.Sum256([]byte(clean))
	return hex.EncodeToString(hash[:])
}
