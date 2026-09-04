package reconciliation

import (
	"code-shield/models"
	"os"
	"path/filepath"
	"time"
)

// PrunerDecision 退火修剪决议
type PrunerDecision struct {
	ShouldArchive bool
	Reason        string
	Lifecycle     string
	DiffStatus    string
	Note          string
}

// EvaluateUnmatchedBaseItem 评估未命中的基线条目应进入活动集还是退火休眠
func EvaluateUnmatchedBaseItem(
	item *SynthesisItem,
	currentReportID uint,
	repoRoot string,
	governanceMode string,
	repoUnchanged bool,
	gitDiffTouched bool,
) PrunerDecision {
	// 1. 守卫 L1：检查物理文件是否存在
	if repoRoot != "" && item.FilePath != "" {
		fullPath := filepath.Join(repoRoot, item.FilePath)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			return PrunerDecision{
				ShouldArchive: true,
				Reason:        ArchiveReasonObsoleteDeleted,
				Lifecycle:     LifecycleObsoleteDeleted,
				DiffStatus:    DiffStatusObsolete,
				Note:          "物理文件已被移除，缺陷自然失效归档",
			}
		}
	}

	// 2. 检查人工豁免
	if item.Payload.Feedback == "FALSE_POSITIVE" || item.Payload.Feedback == "WONT_FIX" {
		return PrunerDecision{
			ShouldArchive: true,
			Reason:        ArchiveReasonExemptedManual,
			Lifecycle:     LifecycleExemptedArchived,
			DiffStatus:    DiffStatusDormant,
			Note:          "经人工确认已豁免处理，移出活动台账",
		}
	}

	// 3. 轮次累加规则：仅在 full_ledger 全量基线模式下累加，change_focus 模式冻结计数器
	missedRounds := item.ConsecutiveMissedRounds
	if governanceMode == models.GovernanceModeFullLedger || governanceMode == models.GovernanceModeDefectTracking || governanceMode == "" {
		missedRounds++
	}

	// 4. 高危永不冷寂铁律 (L3)
	w := severityWeight(item.Payload.Severity)
	if w >= 3 {
		// 严重 (3) 或 致命 (4) 漏洞：绝对禁止自动休眠！
		return PrunerDecision{
			ShouldArchive: false,
			Lifecycle:     LifecycleCoverageGap,
			DiffStatus:    DiffStatusExistedGap,
			Note:          "【高危永不冷寂】本轮全量扫描未复现；因属高危隐患，强制保留在活动工作集持续关注",
		}
	}

	// 5. 低中危退火衰减 (L2)
	// 低危/建议项：连续 2 轮未复现自动休眠
	if w == 1 && missedRounds >= 2 {
		return PrunerDecision{
			ShouldArchive: true,
			Reason:        ArchiveReasonDormantAnnealed,
			Lifecycle:     LifecycleDormantArchived,
			DiffStatus:    DiffStatusDormant,
			Note:          "低危建议类缺陷连续 2 轮全量扫描未复现且代码未动，自动退火休眠，移出工作集",
		}
	}

	// 中危告警项：连续 4 轮未复现自动休眠
	if w == 2 && missedRounds >= 4 {
		return PrunerDecision{
			ShouldArchive: true,
			Reason:        ArchiveReasonDormantAnnealed,
			Lifecycle:     LifecycleDormantArchived,
			DiffStatus:    DiffStatusDormant,
			Note:          "中危告警类缺陷连续 4 轮全量扫描未复现且代码未动，自动退火休眠，移出工作集",
		}
	}

	// 6. 未达退火阈值：作为覆盖缺口继续保留在活动工作集
	return PrunerDecision{
		ShouldArchive: false,
		Lifecycle:     LifecycleCoverageGap,
		DiffStatus:    DiffStatusExistedGap,
		Note:          "本轮未复现；代码未变，保留在活动工作集跟踪覆盖率缺口",
	}
}

// CleanAndPurgeColdPool 对冷归档池实施有界治理 (超过 20 轮未复现的已删除条目彻底清除)
func CleanAndPurgeColdPool(archivedList []ArchivedItem, currentReportID uint) []ArchivedItem {
	var kept []ArchivedItem
	for _, it := range archivedList {
		// 已物理删除超过 20 轮的条目，彻底 purge 终止无界累加
		if it.ArchiveReason == ArchiveReasonObsoleteDeleted && it.ConsecutiveMissedRounds > 20 {
			continue
		}
		kept = append(kept, it)
	}
	return kept
}

// TryResurrectFromColdPool 尝试从冷归档池中无损唤醒历史条目
func TryResurrectFromColdPool(archivedItems []ArchivedItem, currentFinding *InternalFinding, currentReportID uint) (*SynthesisItem, int) {
	for idx, arch := range archivedItems {
		isMatch := false
		// 1. 强指纹匹配
		if arch.Fingerprint == currentFinding.Fingerprint {
			isMatch = true
		} else if arch.FilePath == currentFinding.NormPath {
			// 2. 物理 Token 相似度兜底
			sim := CalculateTokenJaccard(currentFinding.CleanTrigger, arch.Payload.TriggerLine)
			if sim >= 0.85 {
				isMatch = true
			}
		}

		if isMatch {
			// 命中冷归档条目！秒级无损唤醒回热工作集
			rounds := append(arch.RoundsSeen, currentReportID)
			firstSeen := currentReportID
			if len(rounds) > 0 {
				firstSeen = rounds[0]
			}

			resurrected := &SynthesisItem{
				Fingerprint:             arch.Fingerprint,
				ItemUID:                 arch.ItemUID,
				DiffStatus:              DiffStatusExisted, // 继承血统，绝不误判为 NEW
				LifecycleStatus:         LifecycleActive,
				FilePath:                arch.FilePath,
				LineNumber:              currentFinding.Payload.LineNumber,
				RoundsSeen:              rounds,
				FirstSeenReport:         firstSeen,
				LastSeenReport:          currentReportID,
				ConsecutiveMissedRounds: 0,
				CoverageGap:             false,
				ReconRelation:           RelationSame,
				ReconBaseUID:            arch.ItemUID,
				Note:                    "从冷归档池中重新检出，无损唤醒恢复活跃并继承历史评价",
				Payload:                 currentFinding.Payload,
			}
			return resurrected, idx
		}
	}
	return nil, -1
}

// ConvertArchivedToSynthesisItem 将归档项转换为活动工作集条目
func ConvertArchivedToSynthesisItem(arch ArchivedItem, currentReportID uint, payload models.AnalysisFinding) SynthesisItem {
	rounds := append(arch.RoundsSeen, currentReportID)
	return SynthesisItem{
		Fingerprint:             arch.Fingerprint,
		ItemUID:                 arch.ItemUID,
		DiffStatus:              DiffStatusExisted,
		LifecycleStatus:         LifecycleActive,
		FilePath:                arch.FilePath,
		LineNumber:              payload.LineNumber,
		RoundsSeen:              rounds,
		FirstSeenReport:         rounds[0],
		LastSeenReport:          currentReportID,
		ConsecutiveMissedRounds: 0,
		CoverageGap:             false,
		ReconRelation:           RelationSame,
		ReconBaseUID:            arch.ItemUID,
		Note:                    "从冷归档池唤醒",
		Payload:                 payload,
	}
}

// MakeArchivedItem 构造冷归档条目
func MakeArchivedItem(item *SynthesisItem, reason string, lifecycle string, note string) ArchivedItem {
	return ArchivedItem{
		Fingerprint:             item.Fingerprint,
		ItemUID:                 item.ItemUID,
		DiffStatus:              DiffStatusDormant,
		LifecycleStatus:         lifecycle,
		FilePath:                item.FilePath,
		LineNumber:              item.LineNumber,
		RoundsSeen:              item.RoundsSeen,
		LastSeenReport:          item.LastSeenReport,
		ConsecutiveMissedRounds: item.ConsecutiveMissedRounds + 1,
		ArchiveReason:           reason,
		ArchivedAt:              time.Now(),
		Note:                    note,
		Payload:                 item.Payload,
	}
}
