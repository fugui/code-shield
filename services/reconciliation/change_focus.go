package reconciliation

import (
	"code-shield/models"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// RunChangeFocusReconciliation 执行变更增量焦点模式 (change_focus) 的专用工作流
// 核心铁律：
// 1. 未被变更文件触碰的历史问题 100% 物理旁路隔离，绝不拉取、不处理、不合入本报告，COVERAGE_GAP 严格为 0；
// 2. 双向聚焦：检出未匹配项判定为 NEW_IN_DIFF (门禁拦截卡点)；变动 Hunk 区间消失缺陷判定为 RESOLVED_BY_CHANGE (顺带核销并出具 Proof of Fix)。
func RunChangeFocusReconciliation(
	req *ReconcileRequest,
	baseItems []SynthesisItem,
	currentFindings []InternalFinding,
) (*ReconcileResult, error) {
	changedFileSet := make(map[string]bool)
	for _, f := range req.ChangedFiles {
		changedFileSet[strings.ToLower(filepath.ToSlash(strings.TrimSpace(f)))] = true
	}

	// 1. 物理旁路隔离：过滤基线条目，仅保留落在本次变动文件列表中的条目
	var touchedBaseItems []SynthesisItem
	for _, it := range baseItems {
		normPath := strings.ToLower(filepath.ToSlash(strings.TrimSpace(it.FilePath)))
		if changedFileSet[normPath] {
			touchedBaseItems = append(touchedBaseItems, it)
		}
	}

	// 2. 把 touchedBaseItems 转换为 InternalFinding 参与漏斗匹配
	var baseInternal []InternalFinding
	for idx, it := range touchedBaseItems {
		sLine, eLine := ParseLineNumberRange(it.LineNumber)
		baseInternal = append(baseInternal, InternalFinding{
			OriginalIndex: idx,
			Payload:       it.Payload,
			NormPath:      strings.ToLower(filepath.ToSlash(strings.TrimSpace(it.FilePath))),
			NormScope:     NormalizeScopeSymbol(it.Payload.ScopeSymbol),
			CleanTrigger:  CleanSourceToken(it.Payload.TriggerLine),
			StartLine:     sLine,
			EndLine:       eLine,
			Fingerprint:   it.Fingerprint,
			Category:      SanitizeCategory(it.Payload.Category),
			Severity:      it.Payload.Severity,
			ItemUID:       it.ItemUID,
		})
	}

	matchedCurrentMap, claimedBaseMap, matchedBaseMap := RunDeterministicFunnel(baseInternal, currentFindings)

	var items []SynthesisItem
	var links []models.ReconciliationLink
	var diffLinks []ReconDiffLink
	var resolvedFindings []models.AnalysisFinding

	newIntroducedCount := 0
	resolvedHistoryCount := 0
	existedCount := 0

	// 3. 处理 Current 侧（双向聚焦：报新）
	for j, curr := range currentFindings {
		match := matchedCurrentMap[j]
		if match != nil {
			// 变动区存量缺陷 (改动但未修掉)
			existedCount++
			bUID := curr.ItemUID
			baseIt := touchedBaseItems[match.BaseIndex]
			it := SynthesisItem{
				Fingerprint:             curr.Fingerprint,
				ItemUID:                 bUID,
				DiffStatus:              DiffStatusExisted,
				LifecycleStatus:         LifecycleActive,
				FilePath:                curr.NormPath,
				LineNumber:              curr.Payload.LineNumber,
				RoundsSeen:              append(baseIt.RoundsSeen, req.CurrentReportID),
				FirstSeenReport:         baseIt.FirstSeenReport,
				LastSeenReport:          req.CurrentReportID,
				ConsecutiveMissedRounds: 0,
				CoverageGap:             false,
				ReconRelation:           match.Relation,
				ReconBaseUID:            baseIt.ItemUID,
				SeverityRange:           match.SeverityRange,
				SeverityTriage:          match.SeverityTriage,
				Note:                    "本次改动触碰了该文件，但未消除既有缺陷",
				Payload:                 curr.Payload,
			}
			items = append(items, it)

			link := models.ReconciliationLink{
				BaseFP:         baseIt.Fingerprint,
				CurrentFP:      curr.Fingerprint,
				BaseItemUID:    baseIt.ItemUID,
				CurrentItemUID: bUID,
				BaseScope:      baseIt.Payload.ScopeSymbol,
				CurrScope:      curr.NormScope,
				MatchedTier:    match.MatchedTier,
				Confidence:     match.Confidence,
				Relation:       match.Relation,
				SeverityRange:  match.SeverityRange,
				SeverityTriage: match.SeverityTriage,
				Reason:         match.Reason,
			}
			links = append(links, link)

			diffLinks = append(diffLinks, ReconDiffLink{
				BaseFP:         baseIt.Fingerprint,
				CurrentFP:      curr.Fingerprint,
				BaseItemUID:    baseIt.ItemUID,
				CurrentItemUID: bUID,
				Relation:       match.Relation,
				MatchedTier:    match.MatchedTier,
				Confidence:     match.Confidence,
				File:           curr.NormPath,
				Reason:         match.Reason,
			})
		} else {
			// 本次改动新引入缺陷 (CI 拦截卡点)
			newIntroducedCount++
			bUID := curr.ItemUID
			it := SynthesisItem{
				Fingerprint:             curr.Fingerprint,
				ItemUID:                 bUID,
				DiffStatus:              "NEW_IN_DIFF",
				LifecycleStatus:         LifecycleActive,
				FilePath:                curr.NormPath,
				LineNumber:              curr.Payload.LineNumber,
				RoundsSeen:              []uint{req.CurrentReportID},
				FirstSeenReport:         req.CurrentReportID,
				LastSeenReport:          req.CurrentReportID,
				ConsecutiveMissedRounds: 0,
				CoverageGap:             false,
				ReconRelation:           RelationNew,
				Note:                    "本次变更真正引入的增量缺陷 (NEW_IN_DIFF)",
				Payload:                 curr.Payload,
			}
			items = append(items, it)

			link := models.ReconciliationLink{
				CurrentFP:      curr.Fingerprint,
				CurrentItemUID: bUID,
				CurrScope:      curr.NormScope,
				MatchedTier:    0,
				Confidence:     1.0,
				Relation:       RelationNew,
				Reason:         "变更区间检出的全新缺陷",
			}
			links = append(links, link)

			diffLinks = append(diffLinks, ReconDiffLink{
				CurrentFP:      curr.Fingerprint,
				CurrentItemUID: bUID,
				Relation:       RelationNew,
				Confidence:     1.0,
				File:           curr.NormPath,
				Reason:         "本次变更引入新缺陷",
			})
		}
	}

	// 4. 处理变动区内消失的历史缺陷 (双向聚焦：顺带销账)
	for aIdx, baseIt := range touchedBaseItems {
		if matchedBaseMap[aIdx] || claimedBaseMap[aIdx] {
			continue
		}

		// 检查该缺陷是否落在本次变更的 Hunk 区间内
		isInsideHunk := false
		sLine, eLine := ParseLineNumberRange(baseIt.LineNumber)
		normPath := strings.ToLower(filepath.ToSlash(strings.TrimSpace(baseIt.FilePath)))

		if ranges, ok := req.HunkRanges[normPath]; ok {
			for _, r := range ranges {
				if sLine <= r.End && eLine >= r.Start {
					isInsideHunk = true
					break
				}
			}
		} else {
			// 若未提供细粒度 Hunk 区间，但文件本身在 changed_files 中且代码发生实质变动，视为在改动范围内
			if !req.RepoUnchanged {
				isInsideHunk = true
			}
		}

		if isInsideHunk && !req.RepoUnchanged {
			// 本次改动成功修复既有隐患！
			resolvedHistoryCount++
			proof := &ProofOfFixData{
				CommitHash: req.HeadCommit,
				Evidence:   fmt.Sprintf("在提交 %s 中，该作用域代码已被实质重写并消除了原缺陷模式", req.HeadCommit),
			}

			it := SynthesisItem{
				Fingerprint:             baseIt.Fingerprint,
				ItemUID:                 baseIt.ItemUID,
				DiffStatus:              DiffStatusResolved,
				LifecycleStatus:         LifecycleResolved,
				FilePath:                baseIt.FilePath,
				LineNumber:              baseIt.LineNumber,
				RoundsSeen:              baseIt.RoundsSeen,
				FirstSeenReport:         baseIt.FirstSeenReport,
				LastSeenReport:          baseIt.LastSeenReport,
				ConsecutiveMissedRounds: baseIt.ConsecutiveMissedRounds,
				CoverageGap:             false,
				ReconRelation:           RelationResolvedByChg,
				ReconBaseUID:            baseIt.ItemUID,
				ProofOfFix:              proof,
				Note:                    "本次变更顺带实质修复了该历史存量缺陷 (Proof of Fix)",
				Payload:                 baseIt.Payload,
			}
			items = append(items, it)

			link := models.ReconciliationLink{
				BaseFP:      baseIt.Fingerprint,
				BaseItemUID: baseIt.ItemUID,
				BaseScope:   baseIt.Payload.ScopeSymbol,
				MatchedTier: 0,
				Confidence:  1.0,
				Relation:    RelationResolvedByChg,
				Reason:      "变更 Hunk 覆盖的历史锚点缺陷在本次检出中消失 (Proof of Fix)",
			}
			links = append(links, link)

			diffLinks = append(diffLinks, ReconDiffLink{
				BaseFP:      baseIt.Fingerprint,
				BaseItemUID: baseIt.ItemUID,
				Relation:    RelationResolvedByChg,
				Confidence:  1.0,
				File:        baseIt.FilePath,
				Reason:      "变更顺带修复",
			})

			// 记录供上层在同一事务中同步更新数据库
			resolvedFinding := baseIt.Payload
			resolvedFinding.DiffStatus = DiffStatusResolved
			resolvedFindings = append(resolvedFindings, resolvedFinding)
		}
		// 若历史缺陷不在变动 Hunk 内，完全旁路豁免，不计入本次报告！
	}

	now := time.Now()
	ledger := SynthesisLedger{
		Meta: SynthesisMeta{
			ReportID:             req.CurrentReportID,
			BaseReportID:         req.BaseReportID,
			RepoID:               req.RepoID,
			TaskTypeID:           req.TaskTypeID,
			TaskName:             req.TaskName,
			GovernanceMode:       models.GovernanceModeChangeFocus,
			CommitHash:           req.HeadCommit,
			RepoUnchanged:        req.RepoUnchanged,
			ActiveCount:          len(items),
			ArchivedCount:        0,
			ChangedFiles:         req.ChangedFiles,
			NewIntroducedCount:   newIntroducedCount,
			ResolvedHistoryCount: resolvedHistoryCount,
			ReconciledAt:         now,
		},
		Items:         items,
		ArchivedItems: []ArchivedItem{},
	}

	reconSession := models.ScanReconciliation{
		RepoID:                req.RepoID,
		TaskTypeID:            req.TaskTypeID,
		BaseReportID:          req.BaseReportID,
		CurrentReportID:       req.CurrentReportID,
		BaseCommit:            req.BaseCommit,
		HeadCommit:            req.HeadCommit,
		RepoUnchanged:         req.RepoUnchanged,
		NewCount:              newIntroducedCount,
		ExistedCount:          existedCount,
		VanishedCoverageGap:   0, // 严格断言：change_focus 模式覆盖缺口必须为 0
		VanishedFixCandidate:  0,
		ActiveWorkingCount:    len(items),
		DormantArchivedCount:  0,
		ObsoleteDeletedCount:  0,
		GovernanceMode:        models.GovernanceModeChangeFocus,
		ResolvedByChangeCount: resolvedHistoryCount,
		Status:                "confirmed",
		CreatedAt:             now,
		UpdatedAt:             now,
	}

	payload := ReconDiffPayload{
		BaseReportID:    req.BaseReportID,
		CurrentReportID: req.CurrentReportID,
		RepoUnchanged:   req.RepoUnchanged,
		Summary: ReconSummary{
			BaseCount:            len(touchedBaseItems),
			CurrentCount:         len(currentFindings),
			Existed:              existedCount,
			New:                  newIntroducedCount,
			VanishedCoverageGap:  0,
			VanishedFixCandidate: 0,
			ActiveWorkingCount:   len(items),
		},
		Links: diffLinks,
	}

	return &ReconcileResult{
		Ledger:           ledger,
		DiffPayload:      payload,
		Reconciliation:   reconSession,
		Links:            links,
		ResolvedByChange: resolvedFindings,
	}, nil
}
