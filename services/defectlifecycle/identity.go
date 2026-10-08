package defectlifecycle

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"code-shield/models"
	"code-shield/services/coverage"
)

var (
	identityTokenPattern = regexp.MustCompile(`[A-Za-z_$][A-Za-z0-9_$]*|0[xX][0-9a-fA-F]+|\d+(?:\.\d+)?|[^\sA-Za-z0-9_$]+`)
	identifierPattern    = regexp.MustCompile(`^[a-z_$][a-z0-9_$]*$`)
	numericPattern       = regexp.MustCompile(`^\d+(?:\.\d+)?$`)
)

var validationChainKeywords = map[string]bool{
	"find": true, "at": true, "dynamic_cast": true, "copyfrom": true,
	"mutablemessage": true, "mutable": true, "get": true, "set": true,
	"operator": true, "deref": true, "return": true,
}

var identityKeywords = map[string]bool{
	"break": true, "case": true, "catch": true, "class": true, "const": true,
	"continue": true, "default": true, "delete": true, "do": true, "else": true,
	"enum": true, "false": true, "for": true, "func": true, "if": true,
	"interface": true, "new": true, "nullptr": true, "null": true, "private": true,
	"protected": true, "public": true, "return": true, "static": true, "struct": true,
	"switch": true, "throw": true, "true": true, "try": true, "type": true,
	"var": true, "while": true,
}

func NormalizePath(repoRoot, path string) string {
	return strings.ToLower(coverage.NormalizeRepoPath(repoRoot, path))
}

func anchorConfidence(finding models.AnalysisFinding) string {
	if strings.TrimSpace(finding.TriggerLine) == "" {
		return AnchorLow
	}
	if strings.TrimSpace(finding.ScopeSymbol) == "" {
		return AnchorMedium
	}
	return AnchorHigh
}

func identityHash(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:])
}

func observationGroupUID(reportID uint, group ObservationGroup, ordinal int) string {
	base := group.Identity.K2
	if base == "" {
		base = group.Identity.K1
	}
	if base == "" {
		base = identityHash(group.Identity.NormPath, group.Identity.ScopeKey, group.Identity.StmtShape)
	}
	return fmt.Sprintf("obs-%d-%s-%04d", reportID, identityHash(base)[:16], ordinal)
}

func statementTokens(raw string) []string {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		return nil
	}
	commentPattern := regexp.MustCompile(`(?s)/\*.*?\*/|//[^\n]*|#[^\n]*`)
	cleaned = commentPattern.ReplaceAllString(cleaned, " ")
	literalPattern := regexp.MustCompile(`(?s)"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|` + "`(?:\\\\.|[^`\\\\])*`")
	cleaned = literalPattern.ReplaceAllString(cleaned, " $LIT ")

	matches := identityTokenPattern.FindAllString(cleaned, -1)
	tokens := make([]string, 0, len(matches))
	for _, token := range matches {
		lower := strings.ToLower(token)
		if identityKeywords[lower] {
			tokens = append(tokens, lower)
			continue
		}
		switch {
		case lower == "$lit":
			tokens = append(tokens, "$LIT")
		case strings.HasPrefix(lower, "0x"):
			tokens = append(tokens, "$LIT")
		case numericPattern.MatchString(lower):
			tokens = append(tokens, "$LIT")
		case identifierPattern.MatchString(lower):
			tokens = append(tokens, "$ID")
		default:
			tokens = append(tokens, lower)
		}
	}
	return tokens
}

func statementShape(raw string) string {
	tokens := statementTokens(raw)
	if len(tokens) == 0 {
		return ""
	}
	return identityHash(strings.Join(tokens, " "))
}

func firstCodeStatement(finding models.AnalysisFinding) string {
	if strings.TrimSpace(finding.TriggerLine) != "" {
		return finding.TriggerLine
	}
	for _, line := range strings.Split(finding.CodeSnippet, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "//") && !strings.HasPrefix(trimmed, "#") {
			return trimmed
		}
	}
	return ""
}

func scopeIdentity(normPath, rawScope string) (string, string, int, string) {
	scope := NormalizeScopeSymbol(rawScope)
	kind := "FUNCTION"
	arity := scopeArity(rawScope)
	switch {
	case strings.Contains(scope, "<lambda>"):
		kind = "LAMBDA"
	case strings.Contains(scope, "::"):
		kind = "METHOD"
	case scope == "":
		kind = "FILE_SCOPE"
	}
	if scope == "" {
		return "@file:" + normPath, kind, arity, scopeKeyHash("@file", normPath, kind, arity)
	}
	return scope, kind, arity, scopeKeyHash(scope, "", kind, arity)
}

func scopeArity(rawScope string) int {
	open := strings.Index(rawScope, "(")
	close := strings.LastIndex(rawScope, ")")
	if open < 0 || close <= open {
		return 0
	}
	arguments := strings.TrimSpace(rawScope[open+1 : close])
	if arguments == "" {
		return 0
	}
	depth := 0
	count := 1
	for _, char := range arguments {
		switch char {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				count++
			}
		}
	}
	return count
}

func scopeKeyHash(symbolPath, fileScope string, kind string, arity int) string {
	symbol := symbolPath
	if fileScope != "" {
		symbol = fileScope
	}
	return identityHash(symbol, kind, fmt.Sprintf("%d", arity))
}

func defectClassMajor(category string) string {
	major := strings.ToLower(strings.TrimSpace(category))
	if major == "" {
		return "uncategorized"
	}
	if idx := strings.IndexAny(major, ":/|"); idx > 0 {
		major = strings.TrimSpace(major[:idx])
	}
	return major
}

func BuildIdentities(repoRoot string, repoID, taskTypeID uint, findings []models.AnalysisFinding) []Identity {
	identities := make([]Identity, 0, len(findings))
	for _, finding := range findings {
		normPath := NormalizePath(repoRoot, finding.FilePath)
		rawStatement := firstCodeStatement(finding)
		shape := statementShape(rawStatement)
		cleanToken := CleanSourceToken(rawStatement)
		symbolPath := strings.TrimSpace(NormalizeScopeSymbol(finding.ScopeSymbol))
		scopeKind := "FUNCTION"
		arity := 0
		scopeBodyHash := ""
		startLine, endLine := ParseLineNumberRange(finding.LineNumber)
		confidence := anchorConfidence(finding)

		if normPath != "" && repoRoot != "" {
			if anchor, err := EnrichSourceAnchor(repoRoot, normPath, finding.LineNumber, rawStatement); err == nil {
				normPath = NormalizePath("", anchor.NormalizedPath)
				symbolPath = anchor.NormalizedScope
				cleanToken = anchor.PhysicalToken
				shape = statementShape(anchor.PhysicalToken)
				startLine, endLine = anchor.StartLine, anchor.EndLine
				scopeBodyHash = anchor.ScopeBodyHash
				confidence = AnchorHigh
			}
		}

		symbolPath, scopeKind, arity, scopeKey := scopeIdentity(normPath, symbolPath)
		class := defectClassMajor(finding.Category)
		identity := Identity{
			RepoID: repoID, TaskTypeID: taskTypeID, NormPath: normPath,
			SymbolPath: symbolPath, ScopeKind: scopeKind, Arity: arity,
			ScopeKey: scopeKey, StmtShape: shape,
			OccurrenceIndex: 0, DefectClassMajor: class,
			LineStart: startLine, LineEnd: endLine, CleanToken: cleanToken,
			ScopeBodyHash: scopeBodyHash, Severity: finding.Severity, Confidence: confidence,
		}
		identity.ResourceIdentity = extractIdentityToken(identity.CleanToken)
		identity.ValidationChain = validationChainIdentity(identity.CleanToken)
		identity.RootFamily = rootFamilyHash(identity.StmtShape, identity.ResourceIdentity, identity.ValidationChain)
		identities = append(identities, identity)
	}

	assignOccurrenceIndexes(identities)

	for i := range identities {
		identity := &identities[i]
		identity.K1 = identityHash(
			fmt.Sprintf("%d", identity.RepoID), fmt.Sprintf("%d", identity.TaskTypeID),
			identity.NormPath, identity.ScopeKey, identity.StmtShape,
			fmt.Sprintf("%d", identity.OccurrenceIndex), identity.DefectClassMajor,
		)
		identity.F2 = identityHash(
			fmt.Sprintf("%d", identity.RepoID), fmt.Sprintf("%d", identity.TaskTypeID),
			identity.ScopeKey, identity.StmtShape,
			fmt.Sprintf("%d", identity.OccurrenceIndex), identity.DefectClassMajor,
		)
		identity.F3 = identityHash(fmt.Sprintf("%d", identity.RepoID), fmt.Sprintf("%d", identity.TaskTypeID), identity.NormPath, identity.ScopeKey, identity.DefectClassMajor)
	}
	assignContextShapes(identities)
	for i := range identities {
		identity := &identities[i]
		identity.K2 = identityHash(identity.K1, identity.PrevShape, identity.NextShape)
		identity.F1 = identityHash(identity.K2, identity.CleanToken)
	}
	return identities
}

func rootFamilyHash(stmtShape, resourceIdentity, validationChain string) string {
	return identityHash("ROOT", stmtShape, resourceIdentity, validationChain)
}

func validationChainIdentity(cleanToken string) string {
	tokens := validationChainTokens(cleanToken)
	if len(tokens) == 0 {
		return ""
	}
	return identityHash(tokens...)
}

func extractIdentityToken(cleanToken string) string {
	tokens := identityTokenPattern.FindAllString(strings.ToLower(cleanToken), -1)
	for _, token := range tokens {
		if len(token) < 4 {
			continue
		}
		if identityKeywords[token] {
			continue
		}
		if identifierPattern.MatchString(token) {
			return token
		}
	}
	return ""
}

func validationChainTokens(cleanToken string) []string {
	found := make([]string, 0, 4)
	seen := make(map[string]bool)
	for _, token := range identityTokenPattern.FindAllString(strings.ToLower(cleanToken), -1) {
		if !validationChainKeywords[token] || seen[token] {
			continue
		}
		seen[token] = true
		found = append(found, token)
	}
	return found
}

func assignOccurrenceIndexes(identities []Identity) {
	groups := make(map[string][]int)
	for i, identity := range identities {
		if identity.Confidence != AnchorHigh {
			continue
		}
		key := strings.Join([]string{
			identity.NormPath, identity.ScopeKey, identity.StmtShape, identity.DefectClassMajor,
		}, "\x1f")
		groups[key] = append(groups[key], i)
	}
	for _, indexes := range groups {
		sort.SliceStable(indexes, func(i, j int) bool {
			if identities[indexes[i]].LineStart != identities[indexes[j]].LineStart {
				return identities[indexes[i]].LineStart < identities[indexes[j]].LineStart
			}
			return identities[indexes[i]].LineEnd < identities[indexes[j]].LineEnd
		})
		occurrence := 1
		for _, index := range indexes {
			identities[index].OccurrenceIndex = occurrence
		}
	}
}

func assignContextShapes(identities []Identity) {
	indices := make(map[string][]int)
	for i, identity := range identities {
		if identity.Confidence != AnchorHigh {
			continue
		}
		key := identity.NormPath + "\x1f" + identity.ScopeKey
		indices[key] = append(indices[key], i)
	}
	for _, group := range indices {
		sort.SliceStable(group, func(i, j int) bool {
			if identities[group[i]].LineStart != identities[group[j]].LineStart {
				return identities[group[i]].LineStart < identities[group[j]].LineStart
			}
			return identities[group[i]].LineEnd < identities[group[j]].LineEnd
		})
		for position, index := range group {
			identities[index].PrevShape = ""
			identities[index].NextShape = ""
			if position > 0 {
				identities[index].PrevShape = identities[group[position-1]].StmtShape
			}
			if position+1 < len(group) {
				identities[index].NextShape = identities[group[position+1]].StmtShape
			}
		}
	}
}

func linesOverlapOrNear(leftStart, leftEnd, rightStart, rightEnd int, maxGap int) bool {
	if leftStart <= 0 || rightStart <= 0 {
		return false
	}
	if leftEnd < leftStart {
		leftEnd = leftStart
	}
	if rightEnd < rightStart {
		rightEnd = rightStart
	}
	return leftStart <= rightEnd+maxGap && rightStart <= leftEnd+maxGap
}

func BuildObservationGroups(repoRoot string, reportID uint, findings []models.AnalysisFinding) []ObservationGroup {
	identities := BuildIdentities(repoRoot, 0, 0, findings)
	type candidate struct {
		identity Identity
		finding  models.AnalysisFinding
		ordinal  int
	}
	items := make([]candidate, 0, len(findings))
	for i := range findings {
		items = append(items, candidate{identity: identities[i], finding: findings[i], ordinal: i})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].identity.NormPath != items[j].identity.NormPath {
			return items[i].identity.NormPath < items[j].identity.NormPath
		}
		if items[i].identity.LineStart != items[j].identity.LineStart {
			return items[i].identity.LineStart < items[j].identity.LineStart
		}
		if items[i].identity.LineEnd != items[j].identity.LineEnd {
			return items[i].identity.LineEnd < items[j].identity.LineEnd
		}
		return items[i].ordinal < items[j].ordinal
	})

	var groups []ObservationGroup
	for _, item := range items {
		merged := false
		for groupIndex := range groups {
			group := &groups[groupIndex]
			existing := group.Identity
			incoming := item.identity
			if existing.DefectClassMajor != incoming.DefectClassMajor {
				continue
			}
			strong := existing.Confidence == AnchorHigh && incoming.Confidence == AnchorHigh &&
				existing.K2 != "" && incoming.K2 != "" && existing.K2 == incoming.K2
			sameAnchor := existing.K1 != "" && incoming.K1 != "" && existing.K1 == incoming.K1
			closeEnough := linesOverlapOrNear(existing.LineStart, existing.LineEnd, incoming.LineStart, incoming.LineEnd, 5)
			tokenSimilar := CalculateTokenJaccard(existing.CleanToken, incoming.CleanToken) >= 0.9
			if strong || (sameAnchor && closeEnough && tokenSimilar && incoming.Confidence != AnchorLow) {
				group.Findings = append(group.Findings, item.finding)
				merged = true
				break
			}
		}
		if !merged {
			groups = append(groups, ObservationGroup{
				Representative: item.finding,
				Findings:       []models.AnalysisFinding{item.finding},
				Identity:       item.identity,
			})
		}
	}

	for i := range groups {
		groups[i].UID = observationGroupUID(reportID, groups[i], i)
		groups[i].Representative = selectRepresentative(groups[i].Findings)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		left, right := groups[i].Identity, groups[j].Identity
		if left.NormPath != right.NormPath {
			return left.NormPath < right.NormPath
		}
		if left.LineStart != right.LineStart {
			return left.LineStart < right.LineStart
		}
		return groups[i].UID < groups[j].UID
	})
	return groups
}

func selectRepresentative(findings []models.AnalysisFinding) models.AnalysisFinding {
	if len(findings) == 0 {
		return models.AnalysisFinding{}
	}
	selected := findings[0]
	for _, finding := range findings[1:] {
		if severityWeight(finding.Severity) > severityWeight(selected.Severity) ||
			(severityWeight(finding.Severity) == severityWeight(selected.Severity) && finding.Title < selected.Title) {
			selected = finding
		}
	}
	return selected
}

func severityWeight(severity string) int {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "致命", "fatal", "blocker", "阻塞", "blocking", "p0":
		return 5
	case "严重", "critical", "major_error", "error", "p1", "高", "高危", "高风险", "high", "high_risk":
		return 4
	case "一般", "minor", "warning", "主要", "major", "中", "中危", "中风险", "medium", "medium_risk", "p2":
		return 3
	case "低", "低危", "低风险", "low", "low_risk", "p3":
		return 2
	default:
		return 1
	}
}
