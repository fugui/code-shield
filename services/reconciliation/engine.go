package reconciliation

import (
	"code-shield/models"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"
)

// Reconcile 报告对报告对账引擎核心总入口 (Pure Function / Idempotent)
func Reconcile(req *ReconcileRequest) (*ReconcileResult, error) {
	if req == nil {
		return nil, fmt.Errorf("reconciliation request is nil")
	}

	// 1. 运行时判定治理模式 (兼容存量 defect_tracking)
	govMode := models.ResolveGovernanceMode(req.GovernanceMode)

	// 2. 准备 Current 侧条目的物理锚点与指纹特征
	currentList := PrepareInternalFindings(req.CurrentReportID, req.CurrentFindings, req.RepoID, req.TaskTypeID, req.RepoRoot)

	// 3. 解析基线数据 (支持新版台账与旧版平铺数组双模兼容)
	var baseItems []SynthesisItem
	var baseArchived []ArchivedItem

	if len(req.BaseSynthesisJSON) > 0 {
		var parsedLedger SynthesisLedger
		if err := json.Unmarshal(req.BaseSynthesisJSON, &parsedLedger); err == nil && len(parsedLedger.Items) > 0 {
			baseItems = parsedLedger.Items
			baseArchived = parsedLedger.ArchivedItems
		} else {
			// 回退兼容旧版 []models.AnalysisFinding 平铺数组
			var legacyFindings []models.AnalysisFinding
			if err2 := json.Unmarshal(req.BaseSynthesisJSON, &legacyFindings); err2 == nil && len(legacyFindings) > 0 {
				baseInternal := PrepareInternalFindings(req.BaseReportID, legacyFindings, req.RepoID, req.TaskTypeID, req.RepoRoot)
				for _, bf := range baseInternal {
					baseItems = append(baseItems, SynthesisItem{
						Fingerprint:             bf.Fingerprint,
						ItemUID:                 bf.ItemUID,
						DiffStatus:              DiffStatusExisted,
						LifecycleStatus:         LifecycleActive,
						FilePath:                bf.NormPath,
						LineNumber:              bf.Payload.LineNumber,
						RoundsSeen:              []uint{req.BaseReportID},
						FirstSeenReport:         req.BaseReportID,
						LastSeenReport:          req.BaseReportID,
						ConsecutiveMissedRounds: 0,
						CoverageGap:             false,
						Payload:                 bf.Payload,
					})
				}
			}
		}
	}

	// 4. 若为变更焦点模式 (change_focus)，直接路由至专用工作流
	if govMode == models.GovernanceModeChangeFocus {
		return RunChangeFocusReconciliation(req, baseItems, currentList)
	}

	// ── 5. 全量基线台账模式 (full_ledger) 核心工作流 ──

	// 若无任何基线数据 (如仓库首次扫描)
	if len(baseItems) == 0 && len(baseArchived) == 0 {
		return handleFirstScanInitialLedger(req, currentList)
	}

	// 转换基线数据为内部结构供漏斗比对
	baseInternal := make([]InternalFinding, len(baseItems))
	for idx, it := range baseItems {
		sLine, eLine := ParseLineNumberRange(it.LineNumber)
		cleanTrig := CleanSourceToken(it.Payload.TriggerLine)
		normScope := NormalizeScopeSymbol(it.Payload.ScopeSymbol)
		if normScope == "" {
			normScope = filepath.Base(strings.ToLower(filepath.ToSlash(it.FilePath)))
		}

		baseInternal[idx] = InternalFinding{
			OriginalIndex: idx,
			Payload:       it.Payload,
			NormPath:      strings.ToLower(filepath.ToSlash(strings.TrimSpace(it.FilePath))),
			NormScope:     normScope,
			CleanTrigger:  cleanTrig,
			StartLine:     sLine,
			EndLine:       eLine,
			Fingerprint:   it.Fingerprint,
			Category:      SanitizeCategory(it.Payload.Category),
			Severity:      it.Payload.Severity,
			ItemUID:       it.ItemUID,
		}
	}

	// 执行 R1~R4 纯规则空间几何漏斗
	matchedCurrentMap, claimedBaseMap, matchedBaseMap := RunDeterministicFunnel(baseInternal, currentList)

	// 收集同文件残差准备执行 R5
	var baseResiduals []InternalFinding
	for idx, bf := range baseInternal {
		if !claimedBaseMap[idx] && !matchedBaseMap[idx] {
			baseResiduals = append(baseResiduals, bf)
		}
	}
	var currentResiduals []InternalFinding
	for j, cf := range currentList {
		if matchedCurrentMap[j] == nil {
			currentResiduals = append(currentResiduals, cf)
		}
	}

	// 执行 R5 残差二部图对齐 (带复杂度熔断)
	if len(baseResiduals) > 0 && len(currentResiduals) > 0 {
		r5Results := ArbitrateResiduals(baseResiduals, currentResiduals)
		for _, r5 := range r5Results {
			if matchedCurrentMap[r5.CurrentIndex] == nil && !claimedBaseMap[r5.BaseIndex] {
				claimedBaseMap[r5.BaseIndex] = true
				matchedBaseMap[r5.BaseIndex] = true
				matchedCurrentMap[r5.CurrentIndex] = &MatchFunnelResult{
					BaseIndex:      r5.BaseIndex,
					CurrentIndex:   r5.CurrentIndex,
					MatchedTier:    5,
					Confidence:     r5.Confidence,
					Relation:       r5.Relation,
					Reason:         r5.Reason,
					SeverityRange:  r5.SeverityRange,
					SeverityTriage: r5.SeverityTriage,
				}
			}
		}
	}

	// ── 6. 构造台账活动工作集 items[] ──
	var newWorkingItems []SynthesisItem
	var links []models.ReconciliationLink
	var diffLinks []ReconDiffLink

	existedCount := 0
	newCount := 0
	multiViewMergedCount := 0

	// 跟踪当前轮次使用的 archived_items（排除已唤醒的条目）
	resurrectedArchivedIndices := make(map[int]bool)

	for j, curr := range currentList {
		match := matchedCurrentMap[j]
		if match != nil {
			// 命中热工作集基线
			baseIt := baseItems[match.BaseIndex]
			existedCount++
			if match.Relation == RelationSameMultiView {
				multiViewMergedCount++
			}

			// 继承并更新历史轮次
			rounds := append(baseIt.RoundsSeen, req.CurrentReportID)
			firstSeen := baseIt.FirstSeenReport
			if firstSeen == 0 && len(rounds) > 0 {
				firstSeen = rounds[0]
			}

			sevRange, sevConflict := match.SeverityRange, match.SeverityTriage
			if sevRange == "" {
				sevRange, sevConflict = NormalizeSeverityRange(baseIt.Payload.Severity, curr.Payload.Severity)
			}

			it := SynthesisItem{
				Fingerprint:             curr.Fingerprint,
				ItemUID:                 curr.ItemUID,
				DiffStatus:              DiffStatusExisted,
				LifecycleStatus:         LifecycleActive,
				FilePath:                curr.NormPath,
				LineNumber:              curr.Payload.LineNumber,
				RoundsSeen:              rounds,
				FirstSeenReport:         firstSeen,
				LastSeenReport:          req.CurrentReportID,
				ConsecutiveMissedRounds: 0,
				CoverageGap:             false,
				TemplateFamilyID:        baseIt.TemplateFamilyID,
				ReconRelation:           match.Relation,
				ReconBaseUID:            baseIt.ItemUID,
				SeverityRange:           sevRange,
				SeverityTriage:          sevConflict,
				Note:                    match.Reason,
				Payload:                 curr.Payload,
			}
			newWorkingItems = append(newWorkingItems, it)

			link := models.ReconciliationLink{
				BaseFP:         baseIt.Fingerprint,
				CurrentFP:      curr.Fingerprint,
				BaseItemUID:    baseIt.ItemUID,
				CurrentItemUID: curr.ItemUID,
				BaseScope:      baseIt.Payload.ScopeSymbol,
				CurrScope:      curr.NormScope,
				MatchedTier:    match.MatchedTier,
				Confidence:     match.Confidence,
				Relation:       match.Relation,
				SeverityRange:  sevRange,
				SeverityTriage: sevConflict,
				Reason:         match.Reason,
			}
			links = append(links, link)

			diffLinks = append(diffLinks, ReconDiffLink{
				BaseFP:         baseIt.Fingerprint,
				CurrentFP:      curr.Fingerprint,
				BaseItemUID:    baseIt.ItemUID,
				CurrentItemUID: curr.ItemUID,
				Relation:       match.Relation,
				MatchedTier:    match.MatchedTier,
				Confidence:     match.Confidence,
				File:           curr.NormPath,
				Reason:         match.Reason,
				SeverityRange:  sevRange,
				SeverityTriage: sevConflict,
			})
		} else {
			// 未命中热工作集，尝试从冷归档池中唤醒 (Phase 3.5.4)
			resurrectedItem, archIdx := TryResurrectFromColdPool(baseArchived, &curr, req.CurrentReportID)
			if resurrectedItem != nil {
				resurrectedArchivedIndices[archIdx] = true
				existedCount++
				newWorkingItems = append(newWorkingItems, *resurrectedItem)

				diffLinks = append(diffLinks, ReconDiffLink{
					BaseFP:         resurrectedItem.Fingerprint,
					CurrentFP:      curr.Fingerprint,
					BaseItemUID:    resurrectedItem.ItemUID,
					CurrentItemUID: curr.ItemUID,
					Relation:       RelationSame,
					MatchedTier:    1,
					Confidence:     1.0,
					File:           curr.NormPath,
					Reason:         "从冷归档池无损唤醒 (Resurrected)",
				})
			} else {
				// 全新问题
				newCount++
				it := SynthesisItem{
					Fingerprint:             curr.Fingerprint,
					ItemUID:                 curr.ItemUID,
					DiffStatus:              DiffStatusNew,
					LifecycleStatus:         LifecycleActive,
					FilePath:                curr.NormPath,
					LineNumber:              curr.Payload.LineNumber,
					RoundsSeen:              []uint{req.CurrentReportID},
					FirstSeenReport:         req.CurrentReportID,
					LastSeenReport:          req.CurrentReportID,
					ConsecutiveMissedRounds: 0,
					CoverageGap:             false,
					ReconRelation:           RelationNew,
					Payload:                 curr.Payload,
				}
				newWorkingItems = append(newWorkingItems, it)

				diffLinks = append(diffLinks, ReconDiffLink{
					CurrentFP:      curr.Fingerprint,
					CurrentItemUID: curr.ItemUID,
					Relation:       RelationNew,
					Confidence:     1.0,
					File:           curr.NormPath,
					Reason:         "第一轮未报出的全新问题",
				})
			}
		}
	}

	// ── 7. 处理 A 侧未被命中的基线条目 (退火剪枝与覆盖缺口守卫) ──
	vanishedCoverageGap := 0
	vanishedFixCandidate := 0
	dormantCount := 0
	obsoleteCount := 0

	var newArchivedItems []ArchivedItem

	// 继承未被唤醒的原有冷归档条目
	for idx, arch := range baseArchived {
		if !resurrectedArchivedIndices[idx] {
			newArchivedItems = append(newArchivedItems, arch)
		}
	}

	for aIdx, baseIt := range baseItems {
		if matchedBaseMap[aIdx] || claimedBaseMap[aIdx] {
			continue // 已被命中或多视角认领
		}

		// 检查该条目的代码行是否被变更触碰
		gitDiffTouched := false
		if !req.RepoUnchanged && len(req.HunkRanges) > 0 {
			normPath := strings.ToLower(filepath.ToSlash(strings.TrimSpace(baseIt.FilePath)))
			sLine, eLine := ParseLineNumberRange(baseIt.LineNumber)
			if ranges, ok := req.HunkRanges[normPath]; ok {
				for _, r := range ranges {
					if sLine <= r.End && eLine >= r.Start {
						gitDiffTouched = true
						break
					}
				}
			}
		}

		// Phase 3.5 退火剪枝评估
		decision := EvaluateUnmatchedBaseItem(&baseIt, req.CurrentReportID, req.RepoRoot, govMode, req.RepoUnchanged, gitDiffTouched)

		if decision.ShouldArchive {
			// 移入冷归档池
			archItem := MakeArchivedItem(&baseIt, decision.Reason, decision.Lifecycle, decision.Note)
			newArchivedItems = append(newArchivedItems, archItem)
			if decision.Reason == ArchiveReasonObsoleteDeleted {
				obsoleteCount++
			} else {
				dormantCount++
			}

			diffLinks = append(diffLinks, ReconDiffLink{
				BaseFP:      baseIt.Fingerprint,
				BaseItemUID: baseIt.ItemUID,
				Relation:    RelationVanished,
				Confidence:  1.0,
				File:        baseIt.FilePath,
				Reason:      decision.Note,
			})
		} else {
			// 保留在活动工作集（作为覆盖率缺口）
			vanishedCoverageGap++
			gapItem := baseIt
			gapItem.DiffStatus = decision.DiffStatus
			gapItem.LifecycleStatus = decision.Lifecycle
			gapItem.CoverageGap = true
			gapItem.ConsecutiveMissedRounds++
			gapItem.ReconRelation = RelationVanished
			gapItem.Note = decision.Note
			newWorkingItems = append(newWorkingItems, gapItem)

			diffLinks = append(diffLinks, ReconDiffLink{
				BaseFP:      baseIt.Fingerprint,
				BaseItemUID: baseIt.ItemUID,
				Relation:    RelationVanished,
				Confidence:  1.0,
				File:        baseIt.FilePath,
				Reason:      decision.Note,
			})
		}
	}

	// 8. 执行 R6 跨文件模板族聚类 (只聚簇，绝不合并实体)
	newWorkingItems = ClusterTemplateFamilies(newWorkingItems)

	// 计算模板族数量
	templateFamSet := make(map[string]bool)
	for _, it := range newWorkingItems {
		if it.TemplateFamilyID != "" {
			templateFamSet[it.TemplateFamilyID] = true
		}
	}

	// 9. 冷归档池有界治理 (清理超过 20 轮的物理删除条目)
	newArchivedItems = CleanAndPurgeColdPool(newArchivedItems, req.CurrentReportID)

	now := time.Now()
	ledger := SynthesisLedger{
		Meta: SynthesisMeta{
			ReportID:             req.CurrentReportID,
			BaseReportID:         req.BaseReportID,
			RepoID:               req.RepoID,
			TaskTypeID:           req.TaskTypeID,
			TaskName:             req.TaskName,
			GovernanceMode:       govMode,
			CommitHash:           req.HeadCommit,
			RepoUnchanged:        req.RepoUnchanged,
			ActiveCount:          len(newWorkingItems),
			ArchivedCount:        len(newArchivedItems),
			NewIntroducedCount:   newCount,
			ResolvedHistoryCount: 0,
			ReconciledAt:         now,
		},
		Items:         newWorkingItems,
		ArchivedItems: newArchivedItems,
	}

	reconSession := models.ScanReconciliation{
		RepoID:                req.RepoID,
		TaskTypeID:            req.TaskTypeID,
		BaseReportID:          req.BaseReportID,
		CurrentReportID:       req.CurrentReportID,
		BaseCommit:            req.BaseCommit,
		HeadCommit:            req.HeadCommit,
		RepoUnchanged:         req.RepoUnchanged,
		NewCount:              newCount,
		ExistedCount:          existedCount,
		VanishedCoverageGap:   vanishedCoverageGap,
		VanishedFixCandidate:  vanishedFixCandidate,
		MultiViewMerged:       multiViewMergedCount,
		TemplateFamilyCount:   len(templateFamSet),
		ActiveWorkingCount:    len(newWorkingItems),
		DormantArchivedCount:  len(newArchivedItems),
		ObsoleteDeletedCount:  obsoleteCount,
		GovernanceMode:        govMode,
		ResolvedByChangeCount: 0,
		Status:                "confirmed",
		CreatedAt:             now,
		UpdatedAt:             now,
	}

	payload := ReconDiffPayload{
		BaseReportID:    req.BaseReportID,
		CurrentReportID: req.CurrentReportID,
		RepoUnchanged:   req.RepoUnchanged,
		Summary: ReconSummary{
			BaseCount:            len(baseItems),
			CurrentCount:         len(currentList),
			Existed:              existedCount,
			New:                  newCount,
			VanishedCoverageGap:  vanishedCoverageGap,
			VanishedFixCandidate: vanishedFixCandidate,
			MultiViewMerged:      multiViewMergedCount,
			TemplateFamily:       len(templateFamSet),
			ActiveWorkingCount:   len(newWorkingItems),
			DormantArchivedCount: len(newArchivedItems),
			ObsoleteDeletedCount: obsoleteCount,
		},
		Links: diffLinks,
	}

	log.Printf("[Reconciliation] Completed R2R reconcile for Report %d vs %d (Mode: %s). New: %d, Existed: %d, Gaps: %d, MultiView: %d, Families: %d, Active: %d, Archived: %d\n",
		req.CurrentReportID, req.BaseReportID, govMode, newCount, existedCount, vanishedCoverageGap, multiViewMergedCount, len(templateFamSet), len(newWorkingItems), len(newArchivedItems))

	return &ReconcileResult{
		Ledger:         ledger,
		DiffPayload:    payload,
		Reconciliation: reconSession,
		Links:          links,
	}, nil
}

// handleFirstScanInitialLedger 处理无基线时的首次扫描台账初始化
func handleFirstScanInitialLedger(req *ReconcileRequest, currentList []InternalFinding) (*ReconcileResult, error) {
	now := time.Now()
	items := make([]SynthesisItem, len(currentList))
	for i, f := range currentList {
		items[i] = SynthesisItem{
			Fingerprint:             f.Fingerprint,
			ItemUID:                 f.ItemUID,
			DiffStatus:              DiffStatusNew,
			LifecycleStatus:         LifecycleActive,
			FilePath:                f.NormPath,
			LineNumber:              f.Payload.LineNumber,
			RoundsSeen:              []uint{req.CurrentReportID},
			FirstSeenReport:         req.CurrentReportID,
			LastSeenReport:          req.CurrentReportID,
			ConsecutiveMissedRounds: 0,
			CoverageGap:             false,
			ReconRelation:           RelationNew,
			Payload:                 f.Payload,
		}
	}

	// 跨文件模板族聚类
	items = ClusterTemplateFamilies(items)

	ledger := SynthesisLedger{
		Meta: SynthesisMeta{
			ReportID:       req.CurrentReportID,
			RepoID:         req.RepoID,
			TaskTypeID:     req.TaskTypeID,
			TaskName:       req.TaskName,
			GovernanceMode: models.ResolveGovernanceMode(req.GovernanceMode),
			CommitHash:     req.HeadCommit,
			RepoUnchanged:  req.RepoUnchanged,
			ActiveCount:    len(items),
			ArchivedCount:  0,
			ReconciledAt:   now,
		},
		Items:         items,
		ArchivedItems: []ArchivedItem{},
	}

	reconSession := models.ScanReconciliation{
		RepoID:             req.RepoID,
		TaskTypeID:         req.TaskTypeID,
		CurrentReportID:    req.CurrentReportID,
		BaseCommit:         req.BaseCommit,
		HeadCommit:         req.HeadCommit,
		RepoUnchanged:      req.RepoUnchanged,
		NewCount:           len(items),
		ActiveWorkingCount: len(items),
		GovernanceMode:     models.ResolveGovernanceMode(req.GovernanceMode),
		Status:             "confirmed",
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	return &ReconcileResult{
		Ledger:         ledger,
		Reconciliation: reconSession,
	}, nil
}
