package defectlifecycle

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"code-shield/models"
)

const (
	VerdictNew      = "NEW"
	VerdictExisted  = "EXISTED"
	VerdictReopened = "REOPENED"
	VerdictProbable = "PROBABLE"
	VerdictOutside  = "OUTSIDE_FOCUS"
	VerdictCleared  = "CLEARED"

	MatchStrong     = "STRONG"
	MatchStruct     = "STRUCT"
	MatchBucket     = "BUCKET"
	MatchAssign     = "ASSIGN"
	MatchPass       = "PASS"
	MatchAI         = "AI"
	MatchAIFallback = "AI_FALLBACK"
	MatchHuman      = "HUMAN"
	MatchUnclaimed  = "UNCLAIMED"

	AliasSlotPrecise = "SLOT_PRECISE"
	AliasSlotHealed  = "SLOT_HEALED"
	AliasPath        = "PATH"
	AliasRename      = "RENAME"
	AliasHuman       = "HUMAN"
	AliasRoot        = "ROOT"
	AliasResource    = "RESOURCE"
	AliasChain       = "CHAIN"

	AliasStrong = "STRONG"
	AliasBucket = "BUCKET"

	ArbitrationSame      = "SAME"
	ArbitrationDifferent = "DIFFERENT"
	ArbitrationReview    = "REVIEW"

	reasonNoCandidate       = "NO_CANDIDATE"
	reasonLowConfidence     = "ALL_CANDIDATES_BELOW_GATE"
	reasonGrayBand          = "GRAY_BAND_CANDIDATE"
	reasonAssignedElsewhere = "CANDIDATE_ASSIGNED_ELSEWHERE"
	reasonCandidateBudget   = "CANDIDATE_BUDGET_EXCEEDED"
	reasonAIDifferent       = "AI_DIFFERENT"
	reasonAISame            = "AI_SAME"
	reasonAIReview          = "AI_REVIEW"
	reasonAIUnavailable     = "AI_UNAVAILABLE"
	reasonAIDisabled        = "AI_DISABLED"
	reasonAIEmpty           = "AI_EMPTY"
	reasonAutoMerge         = "AUTO_MERGE"
	reasonAutoNew           = "AUTO_NEW"
	reasonPassCleared       = "PASS_CLEARED"
)

type MatchBudget struct {
	MaxCandidates      int
	MaxEdges           int
	MaxAICalls         int
	MaxAssignmentWidth int
}

type MatchThresholds struct {
	StrongSame         float64
	AssignBand         float64
	RejectBelow        float64
	AutoResolve        bool
	AIConfidence       float64
	FallbackMergeScore float64
}

type Candidate struct {
	Defect  models.Defect
	Sources map[string]struct{}
}

type MatchingInput struct {
	Observations  []ObservationGroup
	Defects       []models.Defect
	Aliases       []models.DefectAlias
	RenameTargets map[string]string
	Arbitrator    ArbitrationProvider
	Budget        MatchBudget
	Thresholds    MatchThresholds
}

type MatchingResult struct {
	Observations []ObservationDecision
	AICalls      int
}

type ObservationDecision struct {
	ObservationGroupUID string
	Identity            Identity
	DefectID            *uint
	Verdict             string
	MatchTier           string
	Score               float64
	Confidence          float64
	ScoreDetail         map[string]float64
	Reason              string
}

type ArbitrationCandidate struct {
	Defect      models.Defect
	Score       float64
	ScoreDetail map[string]float64
}

type ArbitrationRequest struct {
	Observation ObservationGroup
	Candidates  []ArbitrationCandidate
	RepoRoot    string
}

type ArbitrationDecision struct {
	DefectID    uint    `json:"candidate_id"`
	AltDefectID uint    `json:"defect_id,omitempty"`
	Decision    string  `json:"decision"`
	Confidence  float64 `json:"confidence"`
	Reason      string  `json:"reason"`
}

func (d *ArbitrationDecision) UnmarshalJSON(data []byte) error {
	type rawDecision ArbitrationDecision
	var raw rawDecision
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*d = ArbitrationDecision(raw)
	if d.DefectID == 0 && d.AltDefectID != 0 {
		d.DefectID = d.AltDefectID
	}
	return nil
}

type ArbitrationProvider interface {
	Arbitrate(request ArbitrationRequest) ([]ArbitrationDecision, error)
}

func RunMatching(input MatchingInput) MatchingResult {
	if input.Budget.MaxCandidates <= 0 {
		input.Budget.MaxCandidates = 64
	}
	if input.Budget.MaxEdges <= 0 {
		input.Budget.MaxEdges = 20000
	}
	if input.Budget.MaxAssignmentWidth <= 0 {
		input.Budget.MaxAssignmentWidth = 16
	}
	if input.Budget.MaxAICalls <= 0 {
		input.Budget.MaxAICalls = 20
	}
	if input.Thresholds.StrongSame <= 0 {
		input.Thresholds.StrongSame = 0.90
	}
	if input.Thresholds.AssignBand <= 0 {
		input.Thresholds.AssignBand = 0.65
	}
	if input.Thresholds.RejectBelow <= 0 {
		input.Thresholds.RejectBelow = 0.45
	}
	if input.Thresholds.AIConfidence <= 0 {
		input.Thresholds.AIConfidence = 0.70
	}
	if input.Thresholds.FallbackMergeScore <= 0 {
		input.Thresholds.FallbackMergeScore = 0.60
	}

	observations := append([]ObservationGroup(nil), input.Observations...)
	sort.SliceStable(observations, func(i, j int) bool {
		left, right := observations[i].Identity, observations[j].Identity
		if left.NormPath != right.NormPath {
			return left.NormPath < right.NormPath
		}
		if left.ScopeKey != right.ScopeKey {
			return left.ScopeKey < right.ScopeKey
		}
		if left.LineStart != right.LineStart {
			return left.LineStart < right.LineStart
		}
		return observations[i].UID < observations[j].UID
	})

	aliasIndex := make(map[string][]models.DefectAlias)
	for _, alias := range input.Aliases {
		if alias.RepoID == 0 && alias.TaskTypeID == 0 {
			aliasIndex[alias.AliasValue] = append(aliasIndex[alias.AliasValue], alias)
			continue
		}
		namespaceKey := alias.AliasValue + "\x00" + fmt.Sprint(alias.RepoID) + "\x00" + fmt.Sprint(alias.TaskTypeID)
		aliasIndex[namespaceKey] = append(aliasIndex[namespaceKey], alias)
	}

	edges := make([][]scoredCandidate, len(observations))
	result := MatchingResult{Observations: make([]ObservationDecision, len(observations))}
	remainingEdges := input.Budget.MaxEdges
	for i, observation := range observations {
		result.Observations[i] = ObservationDecision{
			ObservationGroupUID: observation.UID,
			Identity:            observation.Identity,
			Verdict:             VerdictProbable,
			MatchTier:           MatchUnclaimed,
			ScoreDetail:         map[string]float64{},
		}

		candidates, budgetExceeded := retrieveCandidates(observation.Identity, aliasIndex, input, input.Budget.MaxCandidates)
		scored := make([]scoredCandidate, 0, len(candidates))
		for _, candidate := range candidates {
			if !hardGatePass(observation.Identity, candidate) {
				continue
			}
			score, detail := scoreCandidate(observation.Identity, candidate)
			strongAlias := hasSource(candidate, AliasSlotPrecise) || hasSource(candidate, AliasSlotHealed)
			if detail["evidence_veto"] == 1 && !strongAlias {
				if !(isPassOnlyObservationGroup(observation) && passAnchorGate(candidate, detail)) {
					result.Observations[i].ScoreDetail = detail
					continue
				}
			}
			if detail["evidence_gate"] == 1 && score < input.Thresholds.RejectBelow {
				score = input.Thresholds.RejectBelow + 0.01
				detail["gate_lifted"] = 1
			}
			if score < input.Thresholds.RejectBelow && !strongAlias {
				if !(isPassOnlyObservationGroup(observation) && passAnchorGate(candidate, detail)) {
					continue
				}
			}
			scored = append(scored, scoredCandidate{candidate: candidate, score: score, detail: detail})
		}
		sort.SliceStable(scored, func(left, right int) bool {
			return compareCandidates(scored[left], scored[right])
		})
		if len(scored) > remainingEdges {
			scored = scored[:remainingEdges]
			budgetExceeded = true
		}
		remainingEdges -= len(scored)
		edges[i] = scored
		if budgetExceeded {
			result.Observations[i].Reason = reasonCandidateBudget
		} else if len(candidates) > 0 && len(scored) == 0 {
			result.Observations[i].Reason = reasonLowConfidence
		}
	}

	bindStrongAliases(edges, result.Observations, input.Thresholds)
	bindCoreSlotSingletons(observations, input, edges, result.Observations, input.Thresholds)
	bindCrossScopeEntitySingletons(observations, input, result.Observations, input.Thresholds)
	assignCandidates(observations, edges, result.Observations, input.Thresholds)
	bindPassObservations(observations, edges, result.Observations)
	arbitrateUnclaimed(input, observations, edges, result.Observations, &result.AICalls)
	finalizeUnclaimed(edges, result.Observations, input.Thresholds)
	return result
}

func defectControlledClass(defect models.Defect) string {
	if cls := ReduceControlledDefectClass(defect.DefectClassMajor, ""); cls != "" && cls != ClassOther {
		return cls
	}
	if strings.TrimSpace(defect.Category) != "" {
		return ReduceControlledDefectClass(defect.Category, defect.Title)
	}
	return ReduceControlledDefectClass("", defect.Title)
}

func classesCompatible(defect models.Defect, identity Identity) bool {
	if defect.DefectClassMajor == "" || identity.DefectClassMajor == "" {
		return false
	}
	if defect.DefectClassMajor == identity.DefectClassMajor {
		return true
	}
	if identity.LegacyDefectClassMajor != "" && defect.DefectClassMajor == identity.LegacyDefectClassMajor {
		return true
	}
	defectV2 := defectControlledClass(defect)
	obsV2 := identity.DefectClassMajor
	if obsV2 != "" && obsV2 != ClassOther && defectV2 == obsV2 {
		return true
	}
	return false
}

func slotMatches(defect models.Defect, identity Identity) bool {
	if defect.SlotKey == "" {
		return false
	}
	if identity.SlotKey != "" && defect.SlotKey == identity.SlotKey {
		return true
	}
	return identity.LegacySlotKey != "" && defect.SlotKey == identity.LegacySlotKey
}

func defectV2CoreSlot(defect models.Defect) string {
	normPath := strings.ToLower(strings.TrimSpace(defect.NormPath))
	if normPath == "" {
		return ""
	}
	scope := NormalizeScopeSymbol(defect.ScopeSymbol)
	if scope == "" {
		scope = NormalizeScopeSymbol(defect.SymbolPath)
	}
	classV2 := defectControlledClass(defect)
	target := defect.TargetEntity
	if target == "" && defect.CleanToken != "" {
		target, _ = ExtractPhysicalTargetEntity(defect.CleanToken, "", classV2)
	}
	if target == "" {
		return ""
	}
	return ComputeCoreSlotKey(normPath, scope, classV2, target)
}

func coreSlotMatches(defect models.Defect, identity Identity) bool {
	if identity.CoreSlotKey == "" {
		return false
	}
	if defect.CoreSlotKey != "" {
		if defect.CoreSlotKey == identity.CoreSlotKey {
			return true
		}
		if identity.LegacyCoreSlotKey != "" && defect.CoreSlotKey == identity.LegacyCoreSlotKey {
			return true
		}
	}
	if v2Core := defectV2CoreSlot(defect); v2Core != "" && v2Core == identity.CoreSlotKey {
		return true
	}
	return false
}

// bindCoreSlotSingletons 执行 Tier 2 (CORE_SLOT_SINGLETON) 离散孤例直接归并：
// 当同文件内具备明确物理受体 TargetEntity 的一级核心槽位在当前未认领观测和未认领活跃/待核验/豁免缺陷中均为 1 对 1 孤例时直接绑定，
// 免疫多行拆行或函数内无关代码增删带来的行号/StmtAnchorHash 漂移。
func bindCoreSlotSingletons(observations []ObservationGroup, input MatchingInput, edges [][]scoredCandidate, decisions []ObservationDecision, thresholds MatchThresholds) {
	assignedDefects := assignedDefectIDs(decisions)
	coreSlotObsCount := make(map[string]int, len(observations))
	for i := range observations {
		if decisions[i].DefectID != nil || isPassOnlyObservationGroup(observations[i]) {
			continue
		}
		id := observations[i].Identity
		if id.CoreSlotKey == "" || id.TargetEntity == "" {
			continue
		}
		coreSlotObsCount[id.NormPath+"\x00"+id.CoreSlotKey]++
	}

	for i := range observations {
		if decisions[i].DefectID != nil || isPassOnlyObservationGroup(observations[i]) {
			continue
		}
		id := observations[i].Identity
		if id.CoreSlotKey == "" || id.TargetEntity == "" {
			continue
		}
		if coreSlotObsCount[id.NormPath+"\x00"+id.CoreSlotKey] != 1 {
			continue
		}
		renameTarget := ""
		if input.RenameTargets != nil {
			renameTarget = input.RenameTargets[id.NormPath]
		}
		var matches []models.Defect
		for _, d := range input.Defects {
			if d.ID == 0 || assignedDefects[d.ID] {
				continue
			}
			switch d.Status {
			case DefectStatusMerged, DefectStatusResolved, DefectStatusObsolete:
				continue
			}
			if d.NormPath != id.NormPath && (renameTarget == "" || d.NormPath != renameTarget) {
				continue
			}
			if !classesCompatible(d, id) || !coreSlotMatches(d, id) {
				continue
			}
			matches = append(matches, d)
		}
		if len(matches) != 1 {
			continue
		}
		target := matches[0]
		applySingletonMatch(&decisions[i], id, target, thresholds, "core_slot_singleton", "CORE_SLOT_SINGLETON", AliasRoot)
		assignedDefects[target.ID] = true
		_ = edges
	}
}

func applySingletonMatch(decision *ObservationDecision, id Identity, target models.Defect, thresholds MatchThresholds, flagKey string, reason string, sources ...string) {
	candSources := make(map[string]struct{}, len(sources))
	for _, s := range sources {
		candSources[s] = struct{}{}
	}
	cand := Candidate{Defect: target, Sources: candSources}
	score, detail := scoreCandidate(id, cand)
	detail[flagKey] = 1
	detail["strong_alias_equal"] = 1
	finalScore := math.Max(score, thresholds.StrongSame)

	targetID := target.ID
	decision.DefectID = &targetID
	decision.Score = finalScore
	decision.Confidence = finalScore
	decision.ScoreDetail = detail
	decision.Verdict = matchedVerdict(target.Status)
	decision.MatchTier = MatchStruct
	decision.Reason = reason
}

func canonicalSingletonTargetEntity(raw string) string {
	norm := strings.ToLower(strings.TrimSpace(raw))
	if norm == "" || strings.HasPrefix(norm, "control:") {
		return ""
	}
	return norm
}

func defectSingletonTargetEntity(d models.Defect) string {
	target := d.TargetEntity
	if target == "" && d.CleanToken != "" {
		target, _ = ExtractPhysicalTargetEntity(d.CleanToken, "", defectControlledClass(d))
	}
	return canonicalSingletonTargetEntity(target)
}

// bindCrossScopeEntitySingletons 执行跨函数作用域重命名/提取时的物理受体孤例自愈归并：
// 当函数重命名或代码块提取导致 ScopeKey 与行号同时变化，但同文件（或重命名源路径）内
// (NormPath, DefectClassMajor, TargetEntity) 在剩余未认领观测与未认领存量缺陷中均呈唯一 1 对 1，
// 且清洗源码语句保持有效相似度（Jaccard >= 0.45）时，直接完成结构自愈绑定，防止跌破 AssignBand 误报 NEW。
func bindCrossScopeEntitySingletons(observations []ObservationGroup, input MatchingInput, decisions []ObservationDecision, thresholds MatchThresholds) {
	assignedDefects := assignedDefectIDs(decisions)
	entityObsCount := make(map[string]int, len(observations))
	for i := range observations {
		if decisions[i].DefectID != nil || isPassOnlyObservationGroup(observations[i]) {
			continue
		}
		id := observations[i].Identity
		targetEntity := canonicalSingletonTargetEntity(id.TargetEntity)
		if id.NormPath == "" || id.DefectClassMajor == "" || targetEntity == "" {
			continue
		}
		key := id.NormPath + "\x00" + id.DefectClassMajor + "\x00" + targetEntity
		entityObsCount[key]++
	}

	for i := range observations {
		if decisions[i].DefectID != nil || isPassOnlyObservationGroup(observations[i]) {
			continue
		}
		id := observations[i].Identity
		targetEntity := canonicalSingletonTargetEntity(id.TargetEntity)
		if id.NormPath == "" || id.DefectClassMajor == "" || targetEntity == "" {
			continue
		}
		key := id.NormPath + "\x00" + id.DefectClassMajor + "\x00" + targetEntity
		if entityObsCount[key] != 1 {
			continue
		}
		renameTarget := ""
		if input.RenameTargets != nil {
			renameTarget = input.RenameTargets[id.NormPath]
		}
		var matches []models.Defect
		for _, d := range input.Defects {
			if d.ID == 0 || assignedDefects[d.ID] {
				continue
			}
			switch d.Status {
			case DefectStatusMerged, DefectStatusResolved, DefectStatusObsolete:
				continue
			}
			if d.NormPath != id.NormPath && (renameTarget == "" || d.NormPath != renameTarget) {
				continue
			}
			if !classesCompatible(d, id) || defectSingletonTargetEntity(d) != targetEntity {
				continue
			}
			matches = append(matches, d)
		}
		if len(matches) != 1 {
			continue
		}
		target := matches[0]
		if id.CleanToken != "" && target.CleanToken != "" {
			if CalculateTokenJaccard(id.CleanToken, target.CleanToken) < 0.45 {
				continue
			}
		} else if id.CleanToken != target.CleanToken {
			// 一方有代码词元而另一方完全缺失时不盲目跨越函数归并
			continue
		}
		applySingletonMatch(&decisions[i], id, target, thresholds, "cross_scope_entity_singleton", "CROSS_SCOPE_ENTITY_SINGLETON", AliasRoot, AliasResource)
		assignedDefects[target.ID] = true
	}
}

func bindStrongAliases(edges [][]scoredCandidate, decisions []ObservationDecision, thresholds MatchThresholds) {
	assignedDefects := make(map[uint]bool)
	for i := range decisions {
		if decisions[i].DefectID != nil && *decisions[i].DefectID > 0 {
			assignedDefects[*decisions[i].DefectID] = true
			continue
		}
		decisions[i].DefectID = nil
		for _, edge := range edges[i] {
			defectID := edge.candidate.Defect.ID
			if defectID == 0 || assignedDefects[defectID] {
				continue
			}
			if !hasSource(edge.candidate, AliasSlotPrecise) &&
				!hasSource(edge.candidate, AliasSlotHealed) {
				continue
			}
			edge.detail["strong_alias_equal"] = 1
			edge.score = math.Max(edge.score, thresholds.StrongSame)
			decisions[i].DefectID = &defectID
			decisions[i].Score = edge.score
			decisions[i].Confidence = edge.score
			decisions[i].ScoreDetail = edge.detail
			decisions[i].Verdict = matchedVerdict(edge.candidate.Defect.Status)
			decisions[i].MatchTier = MatchStrong
			if hasSource(edge.candidate, AliasSlotPrecise) {
				decisions[i].Reason = "SLOT_PRECISE"
			} else {
				decisions[i].Reason = "SLOT_HEALED"
			}
			assignedDefects[defectID] = true
			break
		}
	}
}

type scoredCandidate struct {
	candidate Candidate
	score     float64
	detail    map[string]float64
}

func retrieveCandidates(identity Identity, aliasIndex map[string][]models.DefectAlias, input MatchingInput, limit int) ([]Candidate, bool) {
	defectsByID := make(map[uint]models.Defect, len(input.Defects))
	for _, defect := range input.Defects {
		if defect.ID > 0 {
			defectsByID[defect.ID] = defect
		}
	}
	slotKey := identity.SlotKey
	if slotKey == "" && identity.NormPath != "" && identity.DefectClassMajor != "" {
		targetEntity, _ := ExtractPhysicalTargetEntity(identity.CleanToken, "", identity.DefectClassMajor)
		stmtHash := ComputeStmtAnchorHash(identity.CleanToken)
		scopeSymbol := NormalizeScopeSymbol(identity.SymbolPath)
		coreSlot := ComputeCoreSlotKey(identity.NormPath, scopeSymbol, identity.DefectClassMajor, targetEntity)
		slotKey = ComputePreciseSlotKey(coreSlot, stmtHash)
	}
	// RFC-006 P0-A: 强特征优先进入候选池，低信噪比的 AliasPath 置后，防止高密度文件候选预算被无关历史挤爆
	values := make([]struct {
		aliasType string
		value     string
	}, 0, 6)
	if slotKey != "" {
		values = append(values, struct {
			aliasType string
			value     string
		}{AliasSlotHealed, slotKey})
	}
	if identity.LegacySlotKey != "" && identity.LegacySlotKey != slotKey {
		values = append(values, struct {
			aliasType string
			value     string
		}{AliasSlotHealed, identity.LegacySlotKey})
	}
	values = append(values,
		struct {
			aliasType string
			value     string
		}{AliasRoot, identity.RootFamily},
		struct {
			aliasType string
			value     string
		}{AliasResource, identity.ResourceIdentity},
		struct {
			aliasType string
			value     string
		}{AliasChain, identity.ValidationChain},
	)
	if target, ok := input.RenameTargets[identity.NormPath]; ok {
		values = append(values, struct {
			aliasType string
			value     string
		}{AliasRename, target})
	}
	values = append(values, struct {
		aliasType string
		value     string
	}{AliasPath, identity.NormPath})

	namespaceKey := func(value string) string {
		return value + "\x00" + fmt.Sprint(identity.RepoID) + "\x00" + fmt.Sprint(identity.TaskTypeID)
	}
	activeSlotOwners := make(map[string]uint)
	for _, d := range input.Defects {
		if d.Status == DefectStatusActive && d.SlotKey != "" {
			activeSlotOwners[d.SlotKey] = d.ID
		}
	}
	candidates := make(map[uint]*Candidate)
	budgetExceeded := false
	addAlias := func(alias models.DefectAlias) {
		if alias.DefectID == 0 {
			return
		}
		defect, existsDefect := defectsByID[alias.DefectID]
		if !existsDefect || defect.ID == 0 {
			return
		}
		// 若候选已存在，优先补强其命中来源而不额外占用预算配额
		if candidate, exists := candidates[alias.DefectID]; exists {
			candidate.Sources[alias.AliasType] = struct{}{}
			return
		}
		// 纯路径弱召回跳过已终结/已并入的历史缺陷，保留候选预算给活跃或未终结缺陷
		if alias.AliasType == AliasPath &&
			(defect.Status == DefectStatusResolved || defect.Status == DefectStatusObsolete || defect.Status == DefectStatusMerged) {
			return
		}
		// 若该缺陷不是 ACTIVE 状态，且其槽位已被其他活跃缺陷占有，则该非活跃历史记录不可作为候选被复活
		if defect.Status != DefectStatusActive && defect.SlotKey != "" {
			if activeID, occupied := activeSlotOwners[defect.SlotKey]; occupied && activeID != defect.ID {
				return
			}
		}
		if len(candidates) >= limit {
			budgetExceeded = true
			return
		}
		candidates[alias.DefectID] = &Candidate{
			Defect:  defect,
			Sources: map[string]struct{}{alias.AliasType: {}},
		}
	}
	if slotKey != "" || identity.LegacySlotKey != "" {
		var activePreciseDefect *models.Defect
		var nonActivePreciseDefect *models.Defect
		for i := range input.Defects {
			defect := &input.Defects[i]
			if !slotMatches(*defect, identity) ||
				defect.NormPath != identity.NormPath ||
				!classesCompatible(*defect, identity) {
				continue
			}
			if defect.Status == DefectStatusActive {
				if activePreciseDefect == nil || defect.ID > activePreciseDefect.ID {
					activePreciseDefect = defect
				}
			} else if defect.Status != DefectStatusMerged {
				// SLOT_PRECISE 允许召回 HUMAN_CLOSED，确保人工已豁免缺陷在精确复现时触发 EXEMPT_RECHECK 而不误新开
				if nonActivePreciseDefect == nil ||
					(defect.Status == DefectStatusHumanClosed && nonActivePreciseDefect.Status != DefectStatusHumanClosed) ||
					((defect.Status == nonActivePreciseDefect.Status || nonActivePreciseDefect.Status != DefectStatusHumanClosed) && defect.ID > nonActivePreciseDefect.ID) {
					nonActivePreciseDefect = defect
				}
			}
		}
		if activePreciseDefect != nil {
			addAlias(models.DefectAlias{
				DefectID:  activePreciseDefect.ID,
				AliasType: AliasSlotPrecise,
			})
		} else if nonActivePreciseDefect != nil {
			addAlias(models.DefectAlias{
				DefectID:  nonActivePreciseDefect.ID,
				AliasType: AliasSlotPrecise,
			})
		}
	}
	for _, item := range values {
		if item.value == "" {
			continue
		}
		for _, alias := range aliasIndex[item.value] {
			if aliasMatchesIdentity(alias, item.aliasType, identity) {
				addAlias(alias)
			}
		}
		for _, alias := range aliasIndex[namespaceKey(item.value)] {
			if aliasMatchesIdentity(alias, item.aliasType, identity) {
				addAlias(alias)
			}
		}
	}

	result := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, *candidate)
	}
	sort.SliceStable(result, func(left, right int) bool {
		if sourcePriority(result[left]) != sourcePriority(result[right]) {
			return sourcePriority(result[left]) < sourcePriority(result[right])
		}
		return result[left].Defect.ID < result[right].Defect.ID
	})
	return result, budgetExceeded
}

func aliasMatchesIdentity(alias models.DefectAlias, aliasType string, identity Identity) bool {
	if alias.AliasType != aliasType {
		return false
	}
	if identity.RepoID != 0 && alias.RepoID != 0 && alias.RepoID != identity.RepoID {
		return false
	}
	if identity.TaskTypeID != 0 && alias.TaskTypeID != 0 && alias.TaskTypeID != identity.TaskTypeID {
		return false
	}
	return true
}

func sourcePriority(candidate Candidate) int {
	switch {
	case hasSource(candidate, AliasSlotPrecise):
		return 0
	case hasSource(candidate, AliasSlotHealed):
		return 1
	case hasSource(candidate, AliasHuman):
		return 2
	case hasSource(candidate, AliasRename):
		return 3
	case hasSource(candidate, AliasResource), hasSource(candidate, AliasChain):
		return 4
	default:
		return 5
	}
}

func hasSource(candidate Candidate, source string) bool {
	_, ok := candidate.Sources[source]
	return ok
}

func hardGatePass(identity Identity, candidate Candidate) bool {
	if hasSource(candidate, AliasSlotPrecise) || hasSource(candidate, AliasSlotHealed) {
		return true
	}
	if identity.NormPath == candidate.Defect.NormPath {
		return true
	}
	if hasSource(candidate, AliasRename) {
		return true
	}
	if hasSource(candidate, AliasHuman) {
		return true
	}
	return hasSource(candidate, AliasRoot) || hasSource(candidate, AliasResource) || hasSource(candidate, AliasChain)
}

func passAnchorGate(candidate Candidate, detail map[string]float64) bool {
	if hasSource(candidate, AliasSlotPrecise) || hasSource(candidate, AliasSlotHealed) {
		return true
	}
	return detail["scope_key_equal"] == 1 &&
		(detail["line_proximity"] == 1 || detail["clean_token_similarity"] >= 0.8)
}

func scoreCandidate(identity Identity, candidate Candidate) (float64, map[string]float64) {
	detail := map[string]float64{}
	if hasSource(candidate, AliasSlotPrecise) || hasSource(candidate, AliasSlotHealed) {
		detail["context_key_equal"] = 1
	}
	detail["clean_token_similarity"] = CalculateTokenJaccard(identity.CleanToken, candidate.Defect.CleanToken)
	if classesCompatible(candidate.Defect, identity) {
		detail["defect_class_major_equal"] = 1
	}
	detail["line_proximity"] = lineProximity(identity.LineStart, identity.LineEnd, derefInt(candidate.Defect.LineStart), derefInt(candidate.Defect.LineEnd))
	if identity.ScopeKey == candidate.Defect.ScopeKey && identity.ScopeKey != "" {
		detail["scope_key_equal"] = 1
	}
	if identity.ScopeBodyHash == candidate.Defect.ScopeBodyHash && identity.ScopeBodyHash != "" {
		detail["blob_change_consistency"] = 1
	}
	detail["severity_compatibility"] = severityCompatibility(identity.Severity, candidate.Defect.Severity)
	applyEvidenceGate(identity, candidate, detail)

	score := 0.0
	for _, item := range []struct {
		name   string
		weight float64
	}{
		{"context_key_equal", 0.20},
		{"clean_token_similarity", 0.30},
		{"scope_key_equal", 0.20},
		{"defect_class_major_equal", 0.15},
		{"line_proximity", 0.10},
		{"blob_change_consistency", 0.03},
		{"severity_compatibility", 0.02},
	} {
		score += item.weight * detail[item.name]
	}
	return roundScore(score), detail
}

func applyEvidenceGate(identity Identity, candidate Candidate, detail map[string]float64) {
	anchorGate := (detail["scope_key_equal"] == 1 && detail["clean_token_similarity"] >= 0.8) ||
		(detail["scope_key_equal"] == 1 && detail["line_proximity"] >= 0.75) ||
		(detail["clean_token_similarity"] >= 0.8 && detail["line_proximity"] >= 0.5)
	rootAliasGate := hasSource(candidate, AliasRoot)
	resourceAliasGate := hasSource(candidate, AliasResource)
	chainAliasGate := hasSource(candidate, AliasChain)
	candidateResource := strings.ToLower(strings.TrimSpace(candidate.Defect.TargetEntity))
	if candidateResource == "" {
		candidateResource = extractIdentityToken(candidate.Defect.CleanToken)
	}
	candidateChain := validationChainIdentity(candidate.Defect.CleanToken)
	derivedResourceEqual := identity.ResourceIdentity != "" &&
		identity.ResourceIdentity == candidateResource && detail["defect_class_major_equal"] == 1
	derivedChainEqual := identity.ValidationChain != "" && identity.ValidationChain == candidateChain
	rootGate := detail["defect_class_major_equal"] == 1 &&
		(detail["scope_key_equal"] == 1 || rootAliasGate)
	resourceGate := resourceAliasGate || derivedResourceEqual
	chainGate := chainAliasGate || derivedChainEqual
	if rootAliasGate {
		rootGate = true
	}
	detail["anchor_gate"] = boolScore(anchorGate)
	detail["root_gate"] = boolScore(rootGate)
	detail["resource_gate"] = boolScore(resourceGate)
	detail["chain_gate"] = boolScore(chainGate)
	detail["evidence_gate"] = boolScore(anchorGate && (rootGate || resourceGate || chainGate))

	sameClass := detail["defect_class_major_equal"] == 1
	sameScope := detail["scope_key_equal"] == 1
	if !sameClass && !rootAliasGate && !resourceAliasGate && !chainAliasGate &&
		!derivedResourceEqual && !derivedChainEqual {
		detail["veto_reason"] = 1
	}
	if sameClass && !sameScope && !resourceGate && !chainGate {
		detail["veto_reason"] = 1
	}
	detail["evidence_veto"] = detail["veto_reason"]
}

func lineProximity(leftStart, leftEnd, rightStart, rightEnd int) float64 {
	if leftStart <= 0 || rightStart <= 0 {
		return 0
	}
	if leftEnd < leftStart {
		leftEnd = leftStart
	}
	if rightEnd < rightStart {
		rightEnd = rightStart
	}
	if leftStart <= rightEnd && rightStart <= leftEnd {
		return 1
	}
	gap := leftStart - rightEnd
	if gap < 0 {
		gap = rightStart - leftEnd
	}
	if gap > 20 {
		return 0
	}
	return roundScore(1 - float64(gap)/21)
}

func severityCompatibility(left, right string) float64 {
	leftWeight := severityWeight(left)
	rightWeight := severityWeight(right)
	if leftWeight == rightWeight {
		return 1
	}
	if leftWeight-rightWeight == 1 || rightWeight-leftWeight == 1 {
		return 0.5
	}
	return 0
}

func boolScore(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func roundScore(value float64) float64 {
	return math.Round(value*10000) / 10000
}

func compareCandidates(left, right scoredCandidate) bool {
	if left.score != right.score {
		return left.score > right.score
	}
	if left.candidate.Defect.SlotKey != right.candidate.Defect.SlotKey {
		return left.candidate.Defect.SlotKey < right.candidate.Defect.SlotKey
	}
	if left.candidate.Defect.NormPath != right.candidate.Defect.NormPath {
		return left.candidate.Defect.NormPath < right.candidate.Defect.NormPath
	}
	if left.candidate.Defect.ScopeKey != right.candidate.Defect.ScopeKey {
		return left.candidate.Defect.ScopeKey < right.candidate.Defect.ScopeKey
	}
	if derefInt(left.candidate.Defect.LineStart) != derefInt(right.candidate.Defect.LineStart) {
		return derefInt(left.candidate.Defect.LineStart) < derefInt(right.candidate.Defect.LineStart)
	}
	return left.candidate.Defect.ID < right.candidate.Defect.ID
}

func bindPassObservations(observations []ObservationGroup, edges [][]scoredCandidate, decisions []ObservationDecision) {
	for i := range observations {
		if !isPassOnlyObservationGroup(observations[i]) {
			continue
		}
		decision := &decisions[i]
		if decision.DefectID != nil && *decision.DefectID == 0 {
			decision.DefectID = nil
		}
		if decision.DefectID == nil {
			for _, edge := range edges[i] {
				if edge.candidate.Defect.ID == 0 {
					continue
				}
				if !passAnchorGate(edge.candidate, edge.detail) {
					continue
				}
				defectID := edge.candidate.Defect.ID
				decision.DefectID = &defectID
				decision.Score = edge.score
				decision.Confidence = edge.score
				decision.ScoreDetail = edge.detail
				decision.Reason = reasonPassCleared
				break
			}
		}
		if decision.DefectID == nil || *decision.DefectID == 0 {
			decision.DefectID = nil
			continue
		}
		decision.Verdict = VerdictCleared
		decision.MatchTier = MatchPass
		if decision.Reason == "" {
			decision.Reason = reasonPassCleared
		}
	}
}

func derefInt(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func assignCandidates(observations []ObservationGroup, edges [][]scoredCandidate, decisions []ObservationDecision, thresholds MatchThresholds) {
	partitions := make(map[string][]int)
	for i := range observations {
		key := observations[i].Identity.NormPath
		partitions[key] = append(partitions[key], i)
	}
	keys := make([]string, 0, len(partitions))
	for key := range partitions {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		observationIndexes := partitions[key]
		qualified := make([]int, 0, len(observationIndexes))
		for _, observationIndex := range observationIndexes {
			if decisions[observationIndex].DefectID != nil {
				continue
			}
			for _, edge := range edges[observationIndex] {
				if edge.score >= thresholds.AssignBand {
					qualified = append(qualified, observationIndex)
					break
				}
			}
		}
		if len(qualified) == 0 {
			continue
		}
		obsEdges := make(map[int]map[uint]scoredCandidate, len(qualified))
		candidateIDSet := make(map[uint]struct{})
		alreadyAssigned := assignedDefectIDs(decisions)
		for _, observationIndex := range qualified {
			obsEdges[observationIndex] = make(map[uint]scoredCandidate)
			for _, edge := range edges[observationIndex] {
				if edge.score < thresholds.AssignBand || alreadyAssigned[edge.candidate.Defect.ID] {
					continue
				}
				obsEdges[observationIndex][edge.candidate.Defect.ID] = edge
				candidateIDSet[edge.candidate.Defect.ID] = struct{}{}
			}
		}
		candidateIDs := make([]uint, 0, len(candidateIDSet))
		for defectID := range candidateIDSet {
			candidateIDs = append(candidateIDs, defectID)
		}
		sort.Slice(candidateIDs, func(left, right int) bool { return candidateIDs[left] < candidateIDs[right] })

		assignments := greedyAssignment(qualified, obsEdges)
		for observationIndex, defectID := range assignments {
			if defectID == 0 {
				continue
			}
			best := obsEdges[observationIndex][defectID]
			if best.candidate.Defect.ID == 0 {
				continue
			}
			decision := &decisions[observationIndex]
			candID := best.candidate.Defect.ID
			decision.DefectID = &candID
			decision.Score = best.score
			decision.Confidence = best.score
			decision.ScoreDetail = best.detail
			decision.Verdict = matchedVerdict(best.candidate.Defect.Status)
			decision.Reason = "GLOBAL_ASSIGNMENT"
			decision.MatchTier = MatchAssign
			if hasSource(best.candidate, AliasHuman) {
				decision.MatchTier = MatchHuman
			} else if hasSource(best.candidate, AliasSlotPrecise) || hasSource(best.candidate, AliasSlotHealed) {
				decision.MatchTier = MatchStrong
			} else if best.score >= thresholds.StrongSame {
				decision.MatchTier = MatchStruct
			}
		}
	}
}

func greedyAssignment(observationIndexes []int, obsEdges map[int]map[uint]scoredCandidate) map[int]uint {
	type assignmentEdge struct {
		observationIndex int
		edge             scoredCandidate
	}
	assignmentEdges := make([]assignmentEdge, 0)
	for _, observationIndex := range observationIndexes {
		for _, edge := range obsEdges[observationIndex] {
			assignmentEdges = append(assignmentEdges, assignmentEdge{observationIndex: observationIndex, edge: edge})
		}
	}
	sort.SliceStable(assignmentEdges, func(left, right int) bool {
		if assignmentEdges[left].edge.score != assignmentEdges[right].edge.score {
			return assignmentEdges[left].edge.score > assignmentEdges[right].edge.score
		}
		return compareCandidates(
			assignmentEdges[left].edge,
			assignmentEdges[right].edge,
		)
	})
	assignments := make(map[int]uint)
	assignedDefects := make(map[uint]bool)
	for _, item := range assignmentEdges {
		if _, assigned := assignments[item.observationIndex]; assigned || assignedDefects[item.edge.candidate.Defect.ID] {
			continue
		}
		assignments[item.observationIndex] = item.edge.candidate.Defect.ID
		assignedDefects[item.edge.candidate.Defect.ID] = true
	}
	return assignments
}

func bestCandidate(edges []scoredCandidate) *scoredCandidate {
	var best *scoredCandidate
	for index := range edges {
		if best == nil || compareCandidates(edges[index], *best) {
			best = &edges[index]
		}
	}
	return best
}

func matchedVerdict(status string) string {
	switch status {
	case DefectStatusActive, DefectStatusCoverageGap, DefectStatusVerifiedPending, DefectStatusHumanClosed:
		return VerdictExisted
	case DefectStatusResolved, DefectStatusDormant, DefectStatusObsolete:
		return VerdictReopened
	default:
		return VerdictProbable
	}
}

func arbitrateUnclaimed(input MatchingInput, observations []ObservationGroup, edges [][]scoredCandidate, decisions []ObservationDecision, aiCalls *int) {
	if input.Arbitrator == nil {
		for i := range decisions {
			if decisions[i].DefectID == nil && len(edges[i]) > 0 && decisions[i].Reason == reasonGrayBand {
				decisions[i].Reason = reasonAIDisabled
			}
		}
		return
	}
	for i := range decisions {
		if (decisions[i].DefectID != nil && *decisions[i].DefectID > 0) || len(edges[i]) == 0 || *aiCalls >= input.Budget.MaxAICalls {
			continue
		}
		best := edges[i][0]
		assigned := assignedDefectIDs(decisions)
		if best.score < input.Thresholds.RejectBelow || best.score >= input.Thresholds.StrongSame {
			continue
		}
		request := ArbitrationRequest{Observation: observations[i]}
		for _, edge := range edges[i] {
			if edge.score < input.Thresholds.RejectBelow {
				continue
			}
			request.Candidates = append(request.Candidates, ArbitrationCandidate{
				Defect: edge.candidate.Defect, Score: edge.score, ScoreDetail: edge.detail,
			})
		}
		*aiCalls++
		arbitrations, err := input.Arbitrator.Arbitrate(request)
		if err != nil {
			decisions[i].Reason = reasonAIUnavailable
			continue
		}
		if len(arbitrations) == 0 {
			decisions[i].Reason = reasonAIEmpty
		}
		accepted := false
		autoFallback := true
		for _, decision := range arbitrations {
			if decision.DefectID == 0 {
				continue
			}
			for _, edge := range edges[i] {
				if edge.candidate.Defect.ID != decision.DefectID || edge.candidate.Defect.ID == 0 {
					continue
				}
				if decision.Decision == ArbitrationSame && decision.Confidence >= input.Thresholds.AIConfidence {
					accepted = true
					autoFallback = false
					candID := edge.candidate.Defect.ID
					decisions[i].DefectID = &candID
					decisions[i].Score = edge.score
					decisions[i].Confidence = roundScore(decision.Confidence)
					decisions[i].ScoreDetail = edge.detail
					decisions[i].Verdict = matchedVerdict(edge.candidate.Defect.Status)
					decisions[i].MatchTier = MatchAI
					decisions[i].Reason = reasonAISame + ":" + decision.Reason
				} else if decision.Decision == ArbitrationDifferent && decision.Confidence >= input.Thresholds.AIConfidence {
					accepted = true
					autoFallback = false
					decisions[i].Reason = reasonAIDifferent + ":" + decision.Reason
				} else if decision.Decision == ArbitrationReview {
					accepted = true
					decisions[i].Reason = reasonAIReview + ":" + decision.Reason
				}
			}
		}
		if !accepted && decisions[i].Reason == reasonGrayBand {
			decisions[i].Reason = reasonAIUnavailable
		}
		if input.Thresholds.AutoResolve && autoFallback && len(edges[i]) > 0 {
			applyGrayZoneFallback(&edges[i][0], &decisions[i], input.Thresholds,
				len(edges[i]) == 1 && !assigned[best.candidate.Defect.ID])
		}
	}
}

func applyGrayZoneFallback(edge *scoredCandidate, decision *ObservationDecision, thresholds MatchThresholds, allowMerge bool) {
	if (decision.DefectID != nil && *decision.DefectID > 0) || !thresholds.AutoResolve {
		return
	}
	hasEvidence := edge.detail["evidence_gate"] == 1 ||
		edge.detail["strong_alias_equal"] == 1 ||
		hasSource(edge.candidate, AliasHuman) ||
		hasSource(edge.candidate, AliasRename)
	if allowMerge && hasEvidence && edge.score >= thresholds.FallbackMergeScore {
		if edge.candidate.Defect.ID == 0 {
			return
		}
		candID := edge.candidate.Defect.ID
		decision.DefectID = &candID
		decision.Score = edge.score
		decision.Confidence = edge.score
		decision.ScoreDetail = edge.detail
		decision.Verdict = matchedVerdict(edge.candidate.Defect.Status)
		decision.MatchTier = MatchAIFallback
		decision.Reason = reasonAutoMerge + ":" + decision.Reason
		return
	}
	decision.Score = edge.score
	decision.Confidence = edge.score
	decision.ScoreDetail = edge.detail
	decision.MatchTier = MatchAIFallback
	if decision.Reason == "" || decision.Reason == reasonGrayBand {
		decision.Reason = reasonAIUnavailable
	}
	decision.Reason = reasonAutoNew + ":" + decision.Reason
}

func assignedDefectIDs(decisions []ObservationDecision) map[uint]bool {
	assigned := make(map[uint]bool)
	for _, decision := range decisions {
		if decision.DefectID != nil && *decision.DefectID > 0 {
			assigned[*decision.DefectID] = true
		}
	}
	return assigned
}

func finalizeUnclaimed(edges [][]scoredCandidate, decisions []ObservationDecision, thresholds MatchThresholds) {
	assignedDefects := assignedDefectIDs(decisions)
	for i := range decisions {
		if decisions[i].DefectID != nil && *decisions[i].DefectID > 0 {
			continue
		}
		decisions[i].DefectID = nil
		if len(edges[i]) == 0 {
			decisions[i].Verdict = VerdictNew
			decisions[i].MatchTier = MatchUnclaimed
			if decisions[i].Reason == "" {
				decisions[i].Reason = reasonNoCandidate
			}
			continue
		}
		best := edges[i][0]
		decisions[i].Score = best.score
		decisions[i].Confidence = best.score
		decisions[i].ScoreDetail = best.detail
		fallbackApplied := strings.HasPrefix(decisions[i].Reason, reasonAutoMerge) || strings.HasPrefix(decisions[i].Reason, reasonAutoNew)
		if decisions[i].Reason == reasonCandidateBudget {
			if thresholds.AutoResolve {
				applyGrayZoneFallback(&best, &decisions[i], thresholds,
					len(edges[i]) == 1 && !assignedDefects[best.candidate.Defect.ID])
				if decisions[i].DefectID != nil && *decisions[i].DefectID > 0 {
					assignedDefects[*decisions[i].DefectID] = true
				}
			} else {
				decisions[i].Verdict = VerdictProbable
				decisions[i].MatchTier = MatchUnclaimed
			}
			continue
		}
		if fallbackApplied {
			if decisions[i].DefectID == nil || *decisions[i].DefectID == 0 {
				decisions[i].DefectID = nil
				decisions[i].Verdict = VerdictNew
			} else {
				decisions[i].Verdict = matchedVerdict(edges[i][0].candidate.Defect.Status)
			}
			continue
		}
		if best.score < thresholds.RejectBelow || strings.HasPrefix(decisions[i].Reason, reasonAIDifferent) {
			decisions[i].Verdict = VerdictNew
			decisions[i].MatchTier = MatchUnclaimed
			if decisions[i].Reason == "" || decisions[i].Reason == reasonGrayBand {
				decisions[i].Reason = reasonLowConfidence
			}
			continue
		}
		if thresholds.AutoResolve {
			applyGrayZoneFallback(&best, &decisions[i], thresholds,
				len(edges[i]) == 1 && !assignedDefects[best.candidate.Defect.ID])
			if decisions[i].DefectID != nil && *decisions[i].DefectID > 0 {
				assignedDefects[*decisions[i].DefectID] = true
			}
			continue
		} else {
			decisions[i].Verdict = VerdictProbable
			decisions[i].MatchTier = MatchUnclaimed
		}
		if best.score >= thresholds.AssignBand {
			decisions[i].Reason = reasonAssignedElsewhere
		} else if decisions[i].Reason == "" || decisions[i].Reason == reasonGrayBand ||
			(!strings.HasPrefix(decisions[i].Reason, reasonAIReview) && decisions[i].Reason != reasonAIUnavailable) {
			decisions[i].Reason = reasonGrayBand
		}
	}
}
