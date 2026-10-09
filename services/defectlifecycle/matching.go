package defectlifecycle

import (
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

	AliasK2       = "K2"
	AliasF1       = "F1"
	AliasK1       = "K1"
	AliasF2       = "F2"
	AliasF3       = "F3"
	AliasPath     = "PATH"
	AliasRename   = "RENAME"
	AliasHuman    = "HUMAN"
	AliasRoot     = "ROOT"
	AliasResource = "RESOURCE"
	AliasChain    = "CHAIN"

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
	DefectID   uint
	Decision   string
	Confidence float64
	Reason     string
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
			strongAlias := hasSource(candidate, AliasK2) || hasSource(candidate, AliasF1)
			if detail["evidence_veto"] == 1 && !strongAlias {
				if !(isPassOnlyObservationGroup(observation) && passAnchorGate(observation.Identity, candidate, detail)) {
					result.Observations[i].ScoreDetail = detail
					continue
				}
			}
			if detail["evidence_gate"] == 1 && score < input.Thresholds.RejectBelow {
				score = input.Thresholds.RejectBelow + 0.01
				detail["gate_lifted"] = 1
			}
			if score < input.Thresholds.RejectBelow && !strongAlias {
				if !(isPassOnlyObservationGroup(observation) && passAnchorGate(observation.Identity, candidate, detail)) {
					continue
				}
			}
			scored = append(scored, scoredCandidate{candidate: candidate, score: score, detail: detail})
		}
		sort.SliceStable(scored, func(left, right int) bool {
			return compareCandidates(observation.Identity, scored[left], scored[right])
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

	bindStrongAliases(observations, edges, result.Observations, input.Thresholds)
	assignCandidates(observations, edges, result.Observations, input.Budget.MaxAssignmentWidth, input.Thresholds)
	bindPassObservations(observations, edges, result.Observations)
	arbitrateUnclaimed(input, observations, edges, result.Observations, &result.AICalls)
	finalizeUnclaimed(edges, result.Observations, input.Thresholds)
	return result
}

func bindStrongAliases(observations []ObservationGroup, edges [][]scoredCandidate, decisions []ObservationDecision, thresholds MatchThresholds) {
	for i := range decisions {
		if decisions[i].DefectID != nil {
			continue
		}
		for _, edge := range edges[i] {
			if !hasSource(edge.candidate, AliasK2) && !hasSource(edge.candidate, AliasF1) {
				continue
			}
			edge.detail["strong_alias_equal"] = 1
			edge.score = math.Max(edge.score, thresholds.StrongSame)
			decisions[i].DefectID = &edge.candidate.Defect.ID
			decisions[i].Score = edge.score
			decisions[i].Confidence = edge.score
			decisions[i].ScoreDetail = edge.detail
			decisions[i].Verdict = matchedVerdict(edge.candidate.Defect.Status)
			decisions[i].MatchTier = MatchStrong
			decisions[i].Reason = "STRONG_ALIAS"
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
		defectsByID[defect.ID] = defect
	}
	values := []struct {
		aliasType string
		value     string
	}{
		{AliasK2, identity.K2}, {AliasF1, identity.F1}, {AliasK1, identity.K1},
		{AliasF2, identity.F2}, {AliasF3, identity.F3}, {AliasPath, identity.NormPath},
		{AliasRoot, identity.RootFamily}, {AliasResource, identity.ResourceIdentity}, {AliasChain, identity.ValidationChain},
	}
	if target, ok := input.RenameTargets[identity.NormPath]; ok {
		values = append(values, struct {
			aliasType string
			value     string
		}{AliasRename, target})
	}

	namespaceKey := func(value string) string {
		return value + "\x00" + fmt.Sprint(identity.RepoID) + "\x00" + fmt.Sprint(identity.TaskTypeID)
	}
	candidates := make(map[uint]*Candidate)
	budgetExceeded := false
	addAlias := func(alias models.DefectAlias) {
		if len(candidates) >= limit {
			budgetExceeded = true
			return
		}
		candidate, exists := candidates[alias.DefectID]
		if !exists {
			candidate = &Candidate{Defect: defectsByID[alias.DefectID], Sources: map[string]struct{}{}}
			candidates[alias.DefectID] = candidate
		}
		candidate.Sources[alias.AliasType] = struct{}{}
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
	case hasSource(candidate, AliasK2):
		return 0
	case hasSource(candidate, AliasF1):
		return 1
	case hasSource(candidate, AliasHuman):
		return 2
	case hasSource(candidate, AliasK1):
		return 3
	case hasSource(candidate, AliasF2):
		return 4
	case hasSource(candidate, AliasRename):
		return 5
	case hasSource(candidate, AliasResource), hasSource(candidate, AliasChain):
		return 5
	default:
		return 6
	}
}

func hasSource(candidate Candidate, source string) bool {
	_, ok := candidate.Sources[source]
	return ok
}

func hardGatePass(identity Identity, candidate Candidate) bool {
	if identity.NormPath == candidate.Defect.NormPath {
		return true
	}
	if hasSource(candidate, AliasRename) {
		return true
	}
	if identity.ScopeKey != "" && identity.ScopeKey == candidate.Defect.ScopeKey && hasSource(candidate, AliasF2) {
		return true
	}
	if hasSource(candidate, AliasHuman) {
		return true
	}
	return hasSource(candidate, AliasRoot) || hasSource(candidate, AliasResource) || hasSource(candidate, AliasChain)
}

func passAnchorGate(identity Identity, candidate Candidate, detail map[string]float64) bool {
	if hasSource(candidate, AliasK2) || hasSource(candidate, AliasF1) {
		return true
	}
	if detail["stmt_shape_equal"] == 1 {
		return true
	}
	return detail["scope_key_equal"] == 1 &&
		(detail["line_proximity"] == 1 || detail["clean_token_similarity"] >= 0.8)
}

func scoreCandidate(identity Identity, candidate Candidate) (float64, map[string]float64) {
	detail := map[string]float64{}
	if hasSource(candidate, AliasK2) || hasSource(candidate, AliasF1) {
		detail["context_key_equal"] = 1
	}
	if identity.StmtShape == candidate.Defect.StmtShape && identity.StmtShape != "" {
		detail["stmt_shape_equal"] = 1
		detail["shape_tokens_similarity"] = 1
	}
	detail["clean_token_similarity"] = CalculateTokenJaccard(identity.CleanToken, candidate.Defect.CleanToken)
	detail["context_similarity"] = (boolScore(identity.PrevShape == candidate.Defect.PrevShape) + boolScore(identity.NextShape == candidate.Defect.NextShape)) / 2
	if identity.DefectClassMajor == candidate.Defect.DefectClassMajor && identity.DefectClassMajor != "" {
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
		{"context_key_equal", 0.26}, {"stmt_shape_equal", 0.16},
		{"clean_token_similarity", 0.14}, {"shape_tokens_similarity", 0.10},
		{"context_similarity", 0.10}, {"defect_class_major_equal", 0.08},
		{"line_proximity", 0.08}, {"scope_key_equal", 0.04},
		{"blob_change_consistency", 0.02}, {"severity_compatibility", 0.02},
	} {
		score += item.weight * detail[item.name]
	}
	return roundScore(score), detail
}

func applyEvidenceGate(identity Identity, candidate Candidate, detail map[string]float64) {
	anchorGate := (detail["stmt_shape_equal"] == 1 && detail["line_proximity"] >= 0.5) ||
		(detail["scope_key_equal"] == 1 && detail["clean_token_similarity"] >= 0.8) ||
		(detail["scope_key_equal"] == 1 && detail["line_proximity"] == 1)
	rootAliasGate := hasSource(candidate, AliasRoot)
	resourceAliasGate := hasSource(candidate, AliasResource)
	chainAliasGate := hasSource(candidate, AliasChain)
	candidateResource := extractIdentityToken(candidate.Defect.CleanToken)
	candidateChain := validationChainIdentity(candidate.Defect.CleanToken)
	derivedRootEqual := identity.RootFamily != "" &&
		identity.RootFamily == rootFamilyHash(candidate.Defect.StmtShape, candidateResource, candidateChain)
	derivedResourceEqual := identity.ResourceIdentity != "" &&
		identity.ResourceIdentity == candidateResource && detail["defect_class_major_equal"] == 1
	derivedChainEqual := identity.ValidationChain != "" && identity.ValidationChain == candidateChain
	rootGate := detail["defect_class_major_equal"] == 1 &&
		(detail["stmt_shape_equal"] == 1 || detail["scope_key_equal"] == 1 || rootAliasGate || derivedRootEqual)
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
	sameStatement := detail["stmt_shape_equal"] == 1
	sameScope := detail["scope_key_equal"] == 1
	if !sameClass && !rootAliasGate && !resourceAliasGate && !chainAliasGate &&
		!derivedRootEqual && !derivedResourceEqual && !derivedChainEqual {
		detail["veto_reason"] = 1
	}
	if sameClass && !sameStatement && !sameScope && !resourceGate && !chainGate {
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

func compareCandidates(identity Identity, left, right scoredCandidate) bool {
	if left.score != right.score {
		return left.score > right.score
	}
	if left.candidate.Defect.StmtShape != right.candidate.Defect.StmtShape {
		return left.candidate.Defect.StmtShape < right.candidate.Defect.StmtShape
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
		if decision.DefectID == nil {
			for _, edge := range edges[i] {
				if !passAnchorGate(observations[i].Identity, edge.candidate, edge.detail) {
					continue
				}
				decision.DefectID = &edge.candidate.Defect.ID
				decision.Score = edge.score
				decision.Confidence = edge.score
				decision.ScoreDetail = edge.detail
				decision.Reason = reasonPassCleared
				break
			}
		}
		if decision.DefectID == nil {
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

func assignCandidates(observations []ObservationGroup, edges [][]scoredCandidate, decisions []ObservationDecision, width int, thresholds MatchThresholds) {
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
		for _, observationIndex := range qualified {
			obsEdges[observationIndex] = make(map[uint]scoredCandidate)
			for _, edge := range edges[observationIndex] {
				if edge.score < thresholds.AssignBand {
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

		assignments := greedyAssignment(observations, qualified, obsEdges)
		for observationIndex, defectID := range assignments {
			best := obsEdges[observationIndex][defectID]
			decision := &decisions[observationIndex]
			decision.DefectID = &best.candidate.Defect.ID
			decision.Score = best.score
			decision.Confidence = best.score
			decision.ScoreDetail = best.detail
			decision.Verdict = matchedVerdict(best.candidate.Defect.Status)
			decision.Reason = "GLOBAL_ASSIGNMENT"
			decision.MatchTier = MatchAssign
			if hasSource(best.candidate, AliasHuman) {
				decision.MatchTier = MatchHuman
			} else if hasSource(best.candidate, AliasK2) || hasSource(best.candidate, AliasF1) {
				decision.MatchTier = MatchStrong
			} else if best.score >= thresholds.StrongSame {
				decision.MatchTier = MatchStruct
			}
		}
	}
}

func greedyAssignment(observations []ObservationGroup, observationIndexes []int, obsEdges map[int]map[uint]scoredCandidate) map[int]uint {
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
			observations[assignmentEdges[left].observationIndex].Identity,
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
		if best == nil || compareCandidates(Identity{}, edges[index], *best) {
			best = &edges[index]
		}
	}
	return best
}

func matchedVerdict(status string) string {
	switch status {
	case "ACTIVE", "COVERAGE_GAP":
		return VerdictExisted
	case "VERIFIED_PENDING", "RESOLVED", "DORMANT", "OBSOLETE":
		return VerdictReopened
	default:
		return VerdictProbable
	}
}

func arbitrateUnclaimed(input MatchingInput, observations []ObservationGroup, edges [][]scoredCandidate, decisions []ObservationDecision, aiCalls *int) {
	if input.Arbitrator == nil {
		return
	}
	for i := range decisions {
		if decisions[i].DefectID != nil || len(edges[i]) == 0 || *aiCalls >= input.Budget.MaxAICalls {
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
		accepted := false
		autoFallback := true
		for _, decision := range arbitrations {
			if decision.DefectID == 0 {
				continue
			}
			for _, edge := range edges[i] {
				if edge.candidate.Defect.ID != decision.DefectID {
					continue
				}
				if decision.Decision == ArbitrationSame && decision.Confidence >= input.Thresholds.AIConfidence {
					accepted = true
					autoFallback = false
					decisions[i].DefectID = &edge.candidate.Defect.ID
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
	if decision.DefectID != nil || !thresholds.AutoResolve {
		return
	}
	if allowMerge && edge.score >= thresholds.FallbackMergeScore {
		decision.DefectID = &edge.candidate.Defect.ID
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
		if decision.DefectID != nil {
			assigned[*decision.DefectID] = true
		}
	}
	return assigned
}

func finalizeUnclaimed(edges [][]scoredCandidate, decisions []ObservationDecision, thresholds MatchThresholds) {
	assignedDefects := assignedDefectIDs(decisions)
	for i := range decisions {
		if decisions[i].DefectID != nil {
			continue
		}
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
				if decisions[i].DefectID != nil {
					assignedDefects[*decisions[i].DefectID] = true
				}
			} else {
				decisions[i].Verdict = VerdictProbable
				decisions[i].MatchTier = MatchUnclaimed
			}
			continue
		}
		if fallbackApplied {
			if decisions[i].DefectID == nil {
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
			if decisions[i].DefectID != nil {
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
