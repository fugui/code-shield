package defectlifecycle

import (
	"math"
	"strings"
	"time"

	"code-shield/models"

	"gorm.io/gorm"
)

// SlotObservationDecision 单条 Finding 的对账裁决结果
type SlotObservationDecision struct {
	Verdict               string                `json:"verdict"` // EXISTED / NEW
	DefectID              uint                  `json:"defect_id,omitempty"`
	Tier                  string                `json:"tier"` // SLOT_PRECISE / CORE_SLOT_SINGLETON / SLOT_ALIAS / ELASTIC_SELF_HEAL / AI_RESOLVED / CONSERVATIVE_HEAL / UNCLAIMED
	Reason                string                `json:"reason,omitempty"`
	SelfHealTarget        *StabilizedFindingDTO `json:"self_heal_target,omitempty"`
	NeedInPlaceAnchorHeal bool                  `json:"need_in_place_anchor_heal,omitempty"`
	NewStmtAnchorHash     string                `json:"new_stmt_anchor_hash,omitempty"`
	NewSlotKey            string                `json:"new_slot_key,omitempty"`
}

// SlotBatchMatchResult 批次匹配整体汇总
type SlotBatchMatchResult struct {
	Decisions  []SlotObservationDecision
	ExistCount int
	NewCount   int
}

// AIArbiterProvider AI 终审裁决器接口契约
type AIArbiterProvider interface {
	ArbitrateAmbiguityWithSnippets(obs StabilizedFindingDTO, candidates []models.Defect) (ArbitrationVerdict, error)
}

// ArbitrationVerdict AI 仲裁结果
type ArbitrationVerdict struct {
	DefectID   uint
	Confidence float64
	Rationale  string
}

// LedgerTransition 生命周期状态流转意图
type LedgerTransition struct {
	DefectID  uint
	NewStatus string // ACTIVE / RESOLVED
	Action    string // SEEN / FIX_VERIFIED / COVERAGE_GAP
	Reason    string
}

// isSuspiciousCollision 判断是否属于异常碰撞 (如完全无关的高危冲突)
func isSuspiciousCollision(obs StabilizedFindingDTO, cand models.Defect) bool {
	// 如果归一化路径不一致，或者受控主类不一致，不允许合并
	if cand.NormPath != "" && obs.NormPath != "" && cand.NormPath != obs.NormPath {
		return true
	}
	if cand.DefectClassMajor != "" && obs.DefectClassMajor != "" && cand.DefectClassMajor != obs.DefectClassMajor {
		return true
	}
	return false
}

// recallElasticCandidates 两级召回漏斗 (级 1 同作用域 + 级 2 同文件跨作用域)
func recallElasticCandidates(obs StabilizedFindingDTO, active []models.Defect, claimed map[uint]bool) []models.Defect {
	var inScope []models.Defect
	var crossScope []models.Defect

	for _, d := range active {
		if claimed[d.ID] {
			continue
		}
		if d.NormPath != obs.NormPath {
			continue
		}
		if d.DefectClassMajor != "" && obs.DefectClassMajor != "" && d.DefectClassMajor != obs.DefectClassMajor {
			continue
		}

		// 级 1: 同作用域 (专用于捕获局部变量更名重构)
		if d.ScopeSymbol != "" && obs.ScopeSymbol != "" && d.ScopeSymbol == obs.ScopeSymbol {
			inScope = append(inScope, d)
		} else {
			// 级 2: 同文件跨作用域 (专用于捕获函数重命名与方法提取重构，要求 Token Jaccard >= 0.80)
			sim := CalculateTokenJaccard(obs.CleanToken, d.CleanToken)
			if sim >= 0.80 {
				crossScope = append(crossScope, d)
			}
		}
	}

	if len(inScope) > 0 {
		return inScope
	}
	return crossScope
}

// selectBestCandidateByTokenAndProximity 根据代码相似度与物理行临近度评选最佳候选
func selectBestCandidateByTokenAndProximity(obs StabilizedFindingDTO, candidates []models.Defect) (*models.Defect, bool) {
	if len(candidates) == 0 {
		return nil, false
	}
	if len(candidates) == 1 {
		return &candidates[0], false
	}

	var best *models.Defect
	highestScore := -1.0
	secondScore := -1.0

	obsLine := obs.LineStart
	if obsLine <= 0 {
		obsLine = 1
	}

	for i := range candidates {
		c := &candidates[i]
		sim := CalculateTokenJaccard(obs.CleanToken, c.CleanToken)

		// 临近度衰减 [0.0, 1.0]
		cLine := 1
		if c.LineStart != nil && *c.LineStart > 0 {
			cLine = *c.LineStart
		}
		dist := math.Abs(float64(obsLine - cLine))
		proximity := math.Max(0.0, 1.0-(dist/100.0))

		score := sim*0.7 + proximity*0.3
		if score > highestScore {
			secondScore = highestScore
			highestScore = score
			best = c
		} else if score > secondScore {
			secondScore = score
		}
	}

	// 若前两位得分过近且相似度不高，则视为有歧义
	hasAmbiguity := (highestScore-secondScore) < 0.15 && highestScore < 0.85
	return best, hasAmbiguity
}

// selectStrictConservativeCandidate 保守兜底安全评选 (带防吞噬安全护栏)
func selectStrictConservativeCandidate(obs StabilizedFindingDTO, candidates []models.Defect) *models.Defect {
	// 铁律 1: CRITICAL 缺陷严禁保守合并吞噬
	severityUpper := strings.ToUpper(obs.Severity)
	if severityUpper == "CRITICAL" || severityUpper == "FATAL" || severityUpper == "致命" {
		return nil
	}

	obsLine := obs.LineStart
	if obsLine <= 0 {
		obsLine = 1
	}

	var best *models.Defect
	bestSim := 0.0
	minDist := 999999

	for i := range candidates {
		c := &candidates[i]
		sim := CalculateTokenJaccard(obs.CleanToken, c.CleanToken)
		cLine := 1
		if c.LineStart != nil && *c.LineStart > 0 {
			cLine = *c.LineStart
		}
		dist := int(math.Abs(float64(obsLine - cLine)))

		// 铁律 2: 空间跨度 > 30 行且相似度 < 0.60 严禁合并
		if dist > 30 && sim < 0.60 {
			continue
		}

		if sim >= 0.60 && (sim > bestSim || (sim == bestSim && dist < minDist)) {
			bestSim = sim
			minDist = dist
			best = c
		}
	}

	return best
}

// MatchBatchObservations 纯内存两级槽位批次互斥指派与全自动裁决引擎
func MatchBatchObservations(
	findings []StabilizedFindingDTO,
	activeDefects []models.Defect,
	aliases []models.DefectAlias,
	aiArbiter AIArbiterProvider,
) SlotBatchMatchResult {
	result := SlotBatchMatchResult{
		Decisions: make([]SlotObservationDecision, len(findings)),
	}

	// 1. 构建快速查找纯内存索引
	claimedDefects := make(map[uint]bool) // 批次内 1-to-1 互斥锁死集合
	preciseSlotIndex := make(map[string][]models.Defect)
	coreSlotIndex := make(map[string][]models.Defect)
	defectMap := make(map[uint]models.Defect)

	for _, d := range activeDefects {
		defectMap[d.ID] = d
		if d.SlotKey != "" {
			preciseSlotIndex[d.SlotKey] = append(preciseSlotIndex[d.SlotKey], d)
		}
		if d.CoreSlotKey != "" {
			coreSlotIndex[d.CoreSlotKey] = append(coreSlotIndex[d.CoreSlotKey], d)
		}
	}

	// 别名倒排索引：包含重构迁移与历史自愈的 SlotKey (类型为 SLOT_HEALED / RENAME)
	aliasSlotIndex := make(map[string]uint)
	for _, a := range aliases {
		if a.AliasType == "SLOT_HEALED" || a.AliasType == "RENAME" {
			aliasSlotIndex[a.AliasValue] = a.DefectID
		}
	}

	unmatchedIndices := make([]int, 0, len(findings))

	// ── Tier 0 & Tier 0.5: 两级物理槽位直连与单实例秒配自愈 ──
	for i, obs := range findings {
		if obs.TargetEntity == "" || obs.Confidence == ConfidenceLow {
			unmatchedIndices = append(unmatchedIndices, i)
			continue
		}

		preciseSlotKey := obs.SlotKey
		if preciseSlotKey == "" {
			preciseSlotKey = ComputePreciseSlotKey(obs.CoreSlotKey, obs.StmtAnchorHash)
		}

		matched := false

		// A. Tier 0: 优先精准槽位完全匹配 (多实例竞争精确对账)
		if candidates, ok := preciseSlotIndex[preciseSlotKey]; ok {
			for _, cand := range candidates {
				if !claimedDefects[cand.ID] && !isSuspiciousCollision(obs, cand) {
					claimedDefects[cand.ID] = true
					result.Decisions[i] = SlotObservationDecision{
						Verdict:  "EXISTED",
						DefectID: cand.ID,
						Tier:     "SLOT_PRECISE",
						Reason:   "Precise two-level slot exact match",
					}
					result.ExistCount++
					matched = true
					break
				}
			}
		}

		// B. Tier 0.5: 一级核心主槽位单实例秒配与就地自愈 (抗代码格式化与微调变动)
		if !matched && obs.CoreSlotKey != "" {
			if candidates, ok := coreSlotIndex[obs.CoreSlotKey]; ok {
				var unclaimed []models.Defect
				for _, c := range candidates {
					if !claimedDefects[c.ID] {
						unclaimed = append(unclaimed, c)
					}
				}

				// 核心法则：独占单实例 (len == 1) 允许语句锚点哈希位移，直接认领并就地自愈！
				if len(unclaimed) == 1 && !isSuspiciousCollision(obs, unclaimed[0]) {
					cand := unclaimed[0]
					claimedDefects[cand.ID] = true
					result.Decisions[i] = SlotObservationDecision{
						Verdict:               "EXISTED",
						DefectID:              cand.ID,
						Tier:                  "CORE_SLOT_SINGLETON",
						Reason:                "Singleton core slot match with in-place anchor self-healing",
						NeedInPlaceAnchorHeal: true,
						NewStmtAnchorHash:     obs.StmtAnchorHash,
						NewSlotKey:            preciseSlotKey,
					}
					result.ExistCount++
					matched = true
				}
			}
		}

		// C. 检索自愈别名倒排索引 (RENAME / SLOT_HEALED)
		if !matched {
			if defectID, ok := aliasSlotIndex[preciseSlotKey]; ok && !claimedDefects[defectID] {
				cand := defectMap[defectID]
				if !isSuspiciousCollision(obs, cand) {
					claimedDefects[cand.ID] = true
					result.Decisions[i] = SlotObservationDecision{
						Verdict:  "EXISTED",
						DefectID: cand.ID,
						Tier:     "SLOT_ALIAS",
						Reason:   "Matched healed alias slot index",
					}
					result.ExistCount++
					matched = true
				}
			}
		}

		if !matched {
			unmatchedIndices = append(unmatchedIndices, i)
		}
	}

	// ── Tier 1: 两级弹性召回匹配与自愈 (变量更名 + 函数重构) ──
	tier2Indices := make([]int, 0, len(unmatchedIndices))
	for _, idx := range unmatchedIndices {
		obs := findings[idx]
		candidates := recallElasticCandidates(obs, activeDefects, claimedDefects)

		if len(candidates) > 0 {
			best, hasAmbiguity := selectBestCandidateByTokenAndProximity(obs, candidates)
			if best != nil && !hasAmbiguity {
				claimedDefects[best.ID] = true
				cloneObs := obs
				result.Decisions[idx] = SlotObservationDecision{
					Verdict:        "EXISTED",
					DefectID:       best.ID,
					Tier:           "ELASTIC_SELF_HEAL",
					Reason:         "Elastic recall match with token/proximity convergence",
					SelfHealTarget: &cloneObs,
				}
				result.ExistCount++
				continue
			}
		}
		tier2Indices = append(tier2Indices, idx)
	}

	// ── Tier 2: 歧义残差受限 AI 自动终审 (带防吞噬安全护栏) ──
	for _, idx := range tier2Indices {
		obs := findings[idx]
		candidates := recallElasticCandidates(obs, activeDefects, claimedDefects)

		if len(candidates) > 0 && aiArbiter != nil {
			verdict, err := aiArbiter.ArbitrateAmbiguityWithSnippets(obs, candidates)
			if err == nil && verdict.DefectID > 0 && !claimedDefects[verdict.DefectID] {
				claimedDefects[verdict.DefectID] = true
				cloneObs := obs
				result.Decisions[idx] = SlotObservationDecision{
					Verdict:        "EXISTED",
					DefectID:       verdict.DefectID,
					Tier:           "AI_RESOLVED",
					Reason:         verdict.Rationale,
					SelfHealTarget: &cloneObs,
				}
				result.ExistCount++
				continue
			}

			// 异常或超时保护网 (Fail-Safe 严格护栏)
			bestSafe := selectStrictConservativeCandidate(obs, candidates)
			if bestSafe != nil && !claimedDefects[bestSafe.ID] {
				claimedDefects[bestSafe.ID] = true
				result.Decisions[idx] = SlotObservationDecision{
					Verdict:  "EXISTED",
					DefectID: bestSafe.ID,
					Tier:     "CONSERVATIVE_HEAL",
					Reason:   "Strict conservative fallback under guarded proximity",
				}
				result.ExistCount++
				continue
			}
		}

		// ── Tier 3: 确认为真实新增缺陷 (NEW) ──
		result.Decisions[idx] = SlotObservationDecision{
			Verdict: "NEW",
			Tier:    "UNCLAIMED",
			Reason:  "No valid slot or elastic candidate matched",
		}
		result.NewCount++
	}

	return result
}

// SettleLedgerLifecycle 闭环结算存量缺陷生命周期状态机
func SettleLedgerLifecycle(
	activeDefects []models.Defect,
	claimedDefects map[uint]bool,
	evaluator ScopeCoverageEvaluator,
) []LedgerTransition {
	var transitions []LedgerTransition

	for _, defect := range activeDefects {
		if claimedDefects[defect.ID] {
			// 本轮被认领：保持 ACTIVE
			transitions = append(transitions, LedgerTransition{
				DefectID:  defect.ID,
				NewStatus: "ACTIVE",
				Action:    "SEEN",
				Reason:    "Observed and claimed in current scan",
			})
			continue
		}

		// 未被本轮任何 Finding 认领：依赖扫描覆盖真相判定
		if evaluator != nil && evaluator.IsCovered(defect.NormPath) {
			// 文件已被完整扫描，但未再报出该缺陷：确认为已修复！
			transitions = append(transitions, LedgerTransition{
				DefectID:  defect.ID,
				NewStatus: "RESOLVED",
				Action:    "FIX_VERIFIED",
				Reason:    "Covered by scan and no longer observed",
			})
		} else {
			// 扫描分片失败或局部扫描未覆盖该文件：标记覆盖缺口保持 ACTIVE
			transitions = append(transitions, LedgerTransition{
				DefectID:  defect.ID,
				NewStatus: "ACTIVE",
				Action:    "COVERAGE_GAP",
				Reason:    "File not fully covered in this scan run",
			})
		}
	}

	return transitions
}

// ApplyInPlaceAnchorSelfHealing 隐式就地自愈存量缺陷的语句静止锚点哈希与精准槽位
func ApplyInPlaceAnchorSelfHealing(tx *gorm.DB, defectID uint, newStmtHash string, newSlotKey string) error {
	if tx == nil || defectID == 0 || newStmtHash == "" || newSlotKey == "" {
		return nil
	}
	return tx.Model(&models.Defect{}).Where("id = ?", defectID).Updates(map[string]interface{}{
		"stmt_anchor_hash": newStmtHash,
		"slot_key":         newSlotKey,
	}).Error
}

// RecordSlotAliasSelfHealing 记录槽位别名自愈索引
func RecordSlotAliasSelfHealing(tx *gorm.DB, defectID uint, repoID uint, taskTypeID uint, reportID uint, aliasSlotKey string, aliasType string) error {
	if tx == nil || defectID == 0 || aliasSlotKey == "" {
		return nil
	}
	if aliasType == "" {
		aliasType = "SLOT_HEALED"
	}
	var existing models.DefectAlias
	err := tx.Where("repo_id = ? AND task_type_id = ? AND alias_type = ? AND alias_value = ?",
		repoID, taskTypeID, aliasType, aliasSlotKey).First(&existing).Error
	if err == nil {
		return tx.Model(&existing).Updates(map[string]interface{}{
			"hit_count":      existing.HitCount + 1,
			"last_report_id": reportID,
		}).Error
	}
	alias := models.DefectAlias{
		DefectID:      defectID,
		RepoID:        repoID,
		TaskTypeID:    taskTypeID,
		AliasType:     aliasType,
		AliasValue:    aliasSlotKey,
		AliasClass:    "SLOT",
		FirstReportID: reportID,
		LastReportID:  reportID,
		HitCount:      1,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	return tx.Create(&alias).Error
}
