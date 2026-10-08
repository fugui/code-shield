package debate

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"code-shield/services/engines"
	"code-shield/services/engines/chunker"
)

type JudgeEvidenceKind string

const (
	JudgeEvidenceTarget     JudgeEvidenceKind = "target_source"
	JudgeEvidenceRelated    JudgeEvidenceKind = "related_source"
	JudgeEvidenceCaller     JudgeEvidenceKind = "caller"
	JudgeEvidenceDefinition JudgeEvidenceKind = "definition"
	JudgeEvidenceMacro      JudgeEvidenceKind = "macro"
	JudgeEvidenceAbsence    JudgeEvidenceKind = "absence_probe"
)

type JudgeEvidenceRef struct {
	EvidenceID string            `json:"evidence_id,omitempty"`
	Kind       JudgeEvidenceKind `json:"kind"`
	Path       string            `json:"path,omitempty"`
	LineRange  string            `json:"line_range,omitempty"`
	Snippet    string            `json:"snippet,omitempty"`
	ProbeID    string            `json:"probe_id,omitempty"`
	Reason     string            `json:"reason"`
}

type JudgeAbsenceProbe struct {
	ProbeID       string `json:"probe_id"`
	Query         string `json:"query"`
	SearchScope   string `json:"search_scope"`
	SearchedFiles int    `json:"searched_files"`
	Result        string `json:"result"`
	Note          string `json:"note,omitempty"`
}

type JudgeCandidateView struct {
	CandidateID string `json:"candidate_id"`
	FilePath    string `json:"file_path"`
	LineRange   string `json:"line_range"`
	TriggerLine string `json:"trigger_line"`
	ScopeSymbol string `json:"scope_symbol"`
	Category    string `json:"category"`
	Title       string `json:"title"`
	CodeSnippet string `json:"code_snippet"`
	Claim       string `json:"claim"`
}

type JudgeCaseEvidencePack struct {
	CandidateID          string                 `json:"candidate_id"`
	Claim                JudgeCandidateView     `json:"claim"`
	Defense              *ChallengerDefenseCase `json:"defense,omitempty"`
	WorkspaceRoot        string                 `json:"workspace_root"`
	TargetFiles          []string               `json:"target_files"`
	AllowedEvidencePaths []string               `json:"allowed_evidence_paths"`
	PrimaryEvidence      []JudgeEvidenceRef     `json:"primary_evidence"`
	RelatedEvidence      []JudgeEvidenceRef     `json:"related_evidence"`
	MacroEvidence        []JudgeEvidenceRef     `json:"macro_evidence,omitempty"`
	AbsenceProbes        []JudgeAbsenceProbe    `json:"absence_probes,omitempty"`
	NegativeRules        []string               `json:"negative_rules,omitempty"`
	EvidenceDegraded     bool                   `json:"evidence_degraded"`
	EvidenceNotes        []string               `json:"evidence_notes,omitempty"`
}

type JudgeEvidenceVerification struct {
	SourceVerified        bool `json:"source_verified"`
	ReachabilityVerified  bool `json:"reachability_verified"`
	AbsenceVerified       bool `json:"absence_verified"`
	VerifiedEvidenceCount int  `json:"verified_evidence_count"`
	RejectedEvidenceCount int  `json:"rejected_evidence_count"`
}

type judgeSearchMatch struct {
	Path    string
	LineNo  int
	Content string
}

var judgeIdentifierPattern = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

var judgeCommonIdentifiers = map[string]struct{}{
	"auto": {}, "bool": {}, "char": {}, "class": {}, "const": {}, "double": {},
	"else": {}, "false": {}, "float": {}, "for": {}, "if": {}, "int": {},
	"long": {}, "nullptr": {}, "private": {}, "protected": {}, "public": {},
	"return": {}, "short": {}, "signed": {}, "sizeof": {}, "static": {},
	"struct": {}, "switch": {}, "template": {}, "this": {}, "true": {},
	"typename": {}, "union": {}, "unsigned": {}, "virtual": {}, "void": {},
	"while": {}, "include": {}, "define": {}, "namespace": {}, "using": {},
}

func NewJudgeCandidateViews(candidates []HunterCandidate) []JudgeCandidateView {
	views := make([]JudgeCandidateView, 0, len(candidates))
	for _, candidate := range candidates {
		claim := candidate.TriggerCondition
		if strings.TrimSpace(claim) == "" {
			claim = candidate.AttackHypothesis
		}
		if strings.TrimSpace(claim) == "" {
			claim = candidate.Title
		}
		views = append(views, JudgeCandidateView{
			CandidateID: candidate.CandidateID,
			FilePath:    candidate.FilePath,
			LineRange:   candidate.LineRange,
			TriggerLine: candidate.TriggerLine,
			ScopeSymbol: candidate.ScopeSymbol,
			Category:    candidate.GetCategory(),
			Title:       candidate.Title,
			CodeSnippet: candidate.CodeSnippet,
			Claim:       claim,
		})
	}
	return views
}

func BuildJudgeEvidencePacks(
	ctx *engines.EngineContext,
	bundle chunker.SemanticBundle,
	candidates []HunterCandidate,
	defenses map[string]ChallengerDefenseCase,
) ([]JudgeCaseEvidencePack, error) {
	if ctx == nil {
		return nil, fmt.Errorf("engine context is nil")
	}
	if strings.TrimSpace(ctx.CodesPath) == "" {
		return nil, fmt.Errorf("engine context workspace is empty")
	}

	root, err := filepath.Abs(ctx.CodesPath)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root: %w", err)
	}
	views := NewJudgeCandidateViews(candidates)
	packs := make([]JudgeCaseEvidencePack, 0, len(views))
	for _, view := range views {
		pack, buildErr := buildJudgeEvidencePack(ctx.Ctx, root, bundle, view, defenses[view.CandidateID])
		if buildErr != nil {
			return nil, fmt.Errorf("build evidence pack for %s: %w", view.CandidateID, buildErr)
		}
		packs = append(packs, pack)
	}
	return packs, nil
}

func buildJudgeEvidencePack(
	parent context.Context,
	root string,
	bundle chunker.SemanticBundle,
	view JudgeCandidateView,
	defense ChallengerDefenseCase,
) (JudgeCaseEvidencePack, error) {
	pack := JudgeCaseEvidencePack{
		CandidateID:   view.CandidateID,
		Claim:         view,
		WorkspaceRoot: root,
		TargetFiles:   append([]string(nil), bundle.AllFiles...),
		Defense:       &defense,
		NegativeRules: append([]string(nil), bundle.NegativeRules...),
	}

	targetPath, start, end, targetErr := resolveEvidenceTarget(root, view)
	if targetErr != nil {
		pack.EvidenceDegraded = true
		pack.EvidenceNotes = append(pack.EvidenceNotes, fmt.Sprintf("target source unavailable: %v", targetErr))
	} else {
		snippet, lineRange, snippetErr := extractSourceContext(targetPath, start, end)
		if snippetErr != nil {
			pack.EvidenceDegraded = true
			pack.EvidenceNotes = append(pack.EvidenceNotes, fmt.Sprintf("target source unavailable: %v", snippetErr))
		} else {
			pack.PrimaryEvidence = append(pack.PrimaryEvidence, JudgeEvidenceRef{
				EvidenceID: fmt.Sprintf("%s-target", view.CandidateID),
				Kind:       JudgeEvidenceTarget,
				Path:       view.FilePath,
				LineRange:  lineRange,
				Snippet:    snippet,
				Reason:     "Candidate anchor and surrounding source context.",
			})
		}
	}

	identifiers := evidenceIdentifiers(view)
	matches, searchErr := searchEvidenceMatches(parent, root, bundle, identifiers)
	if searchErr != nil {
		pack.EvidenceDegraded = true
		pack.EvidenceNotes = append(pack.EvidenceNotes, fmt.Sprintf("symbol search unavailable: %v", searchErr))
	}

	seen := map[string]struct{}{}
	for _, match := range matches {
		key := match.Path + ":" + fmt.Sprint(match.LineNo)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		kind := JudgeEvidenceCaller
		if sameRelativePath(match.Path, view.FilePath) {
			kind = JudgeEvidenceDefinition
		}
		pack.RelatedEvidence = append(pack.RelatedEvidence, JudgeEvidenceRef{
			EvidenceID: fmt.Sprintf("%s-symbol-%03d", view.CandidateID, len(pack.RelatedEvidence)+1),
			Kind:       kind,
			Path:       match.Path,
			LineRange:  fmt.Sprintf("%d", match.LineNo),
			Snippet:    truncateEvidenceSnippet(match.Content),
			Reason:     fmt.Sprintf("Deterministic symbol match: %s", strings.Join(identifiers, ", ")),
		})
		if len(pack.RelatedEvidence) >= 10 {
			break
		}
	}

	if len(pack.RelatedEvidence) == 0 && len(identifiers) > 0 {
		pack.AbsenceProbes = append(pack.AbsenceProbes, JudgeAbsenceProbe{
			ProbeID:       fmt.Sprintf("%s-absence-repository", view.CandidateID),
			Query:         strings.Join(identifiers, ", "),
			SearchScope:   "repository",
			SearchedFiles: len(bundle.AllFiles),
			Result:        "No deterministic symbol match was found.",
			Note:          "The absence result is based on the evidence builder search, not on model opinion.",
		})
	}

	allowed := make(map[string]struct{})
	for _, path := range append(append([]string{view.FilePath}, bundle.AllFiles...), candidateEvidencePaths(pack)...) {
		if strings.TrimSpace(path) == "" {
			continue
		}
		allowed[path] = struct{}{}
	}
	for path := range allowed {
		pack.AllowedEvidencePaths = append(pack.AllowedEvidencePaths, path)
	}
	sort.Strings(pack.AllowedEvidencePaths)
	return pack, nil
}

func candidateEvidencePaths(pack JudgeCaseEvidencePack) []string {
	paths := make([]string, 0, len(pack.PrimaryEvidence)+len(pack.RelatedEvidence)+len(pack.MacroEvidence))
	for _, group := range [][]JudgeEvidenceRef{pack.PrimaryEvidence, pack.RelatedEvidence, pack.MacroEvidence} {
		for _, ref := range group {
			if strings.TrimSpace(ref.Path) != "" {
				paths = append(paths, ref.Path)
			}
		}
	}
	return paths
}

func resolveEvidenceTarget(root string, view JudgeCandidateView) (string, int, int, error) {
	if err := validateRelativeSourcePath(view.FilePath); err != nil {
		return "", 0, 0, err
	}
	start, end, err := parseLineRange(view.LineRange)
	if err != nil {
		return "", 0, 0, err
	}
	path, err := safeWorkspacePath(root, view.FilePath)
	if err != nil {
		return "", 0, 0, err
	}
	return path, start, end, nil
}

func safeWorkspacePath(root string, relative string) (string, error) {
	if err := validateRelativeSourcePath(relative); err != nil {
		return "", err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	absRoot, err = filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", err
	}
	absPath := filepath.Join(absRoot, filepath.FromSlash(relative))
	resolved, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absRoot, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes workspace root: %s", relative)
	}
	return resolved, nil
}

func parseLineRange(value string) (int, int, error) {
	trimmed := strings.TrimSpace(value)
	if !lineRangePattern.MatchString(trimmed) {
		return 0, 0, fmt.Errorf("line_range must be \"42\" or \"42-50\", got %q", value)
	}
	parts := strings.SplitN(trimmed, "-", 2)
	start, err := parsePositiveInt(parts[0])
	if err != nil {
		return 0, 0, err
	}
	end := start
	if len(parts) == 2 {
		end, err = parsePositiveInt(parts[1])
		if err != nil {
			return 0, 0, err
		}
	}
	if end < start {
		start, end = end, start
	}
	return start, end, nil
}

func parsePositiveInt(value string) (int, error) {
	var parsed int
	_, err := fmt.Sscanf(strings.TrimSpace(value), "%d", &parsed)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid line number %q", value)
	}
	return parsed, nil
}

func extractSourceContext(path string, start, end int) (string, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return "", "", err
	}
	if start > len(lines) || end > len(lines) || start > end {
		return "", "", fmt.Errorf("line range %d-%d is outside file with %d lines", start, end, len(lines))
	}

	contextStart := start - 80
	if contextStart < 1 {
		contextStart = 1
	}
	contextEnd := end + 80
	if contextEnd > len(lines) {
		contextEnd = len(lines)
	}
	return strings.Join(lines[contextStart-1:contextEnd], "\n"), fmt.Sprintf("%d-%d", contextStart, contextEnd), nil
}

func evidenceIdentifiers(view JudgeCandidateView) []string {
	seen := map[string]struct{}{}
	identifiers := make([]string, 0, 8)
	add := func(value string) {
		trimmed := strings.ToLower(strings.TrimSpace(value))
		if len(trimmed) < 4 {
			return
		}
		if _, ok := judgeCommonIdentifiers[trimmed]; ok {
			return
		}
		if _, ok := seen[trimmed]; ok {
			return
		}
		seen[trimmed] = struct{}{}
		identifiers = append(identifiers, trimmed)
	}
	scopeParts := strings.Split(view.ScopeSymbol, "::")
	if len(scopeParts) > 0 {
		add(scopeParts[len(scopeParts)-1])
	}
	if len(scopeParts) > 1 {
		add(scopeParts[len(scopeParts)-2])
	}
	for _, identifier := range judgeIdentifierPattern.FindAllString(view.TriggerLine, -1) {
		add(identifier)
		if len(identifiers) >= 8 {
			break
		}
	}
	return identifiers
}

func searchEvidenceMatches(parent context.Context, root string, bundle chunker.SemanticBundle, identifiers []string) ([]judgeSearchMatch, error) {
	if len(identifiers) == 0 {
		return nil, nil
	}
	ctx := parent
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if _, lookErr := exec.LookPath("rg"); lookErr == nil {
		return searchWithRipgrep(ctx, root, identifiers)
	}
	return scanBundleFiles(ctx, root, bundle, identifiers)
}

func searchWithRipgrep(ctx context.Context, root string, identifiers []string) ([]judgeSearchMatch, error) {
	args := []string{
		"--line-number", "--no-heading", "--color", "never", "--max-count", "20", "--smart-case", "--hidden",
	}
	for _, skip := range []string{
		"!.git/**", "!vendor/**", "!node_modules/**", "!build/**", "!dist/**",
		"!thirdparty/**", "!third_party/**", "!3rdparty/**", "!__pycache__/**",
	} {
		args = append(args, "--glob", skip)
	}
	for _, identifier := range identifiers {
		args = append(args, "--fixed-strings", "--regexp", identifier)
	}
	cmd := exec.CommandContext(ctx, "rg", args...)
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, fmt.Errorf("ripgrep failed: %v: %s", err, strings.TrimSpace(stderr.String()))
	}

	var matches []judgeSearchMatch
	scanner := bufio.NewScanner(bytes.NewReader(stdout.Bytes()))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		lineNo, err := parsePositiveInt(parts[1])
		if err != nil {
			continue
		}
		matches = append(matches, judgeSearchMatch{
			Path:    filepath.ToSlash(filepath.Clean(parts[0])),
			LineNo:  lineNo,
			Content: parts[2],
		})
		if len(matches) >= 200 {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return matches, nil
}

func scanBundleFiles(ctx context.Context, root string, bundle chunker.SemanticBundle, identifiers []string) ([]judgeSearchMatch, error) {
	var matches []judgeSearchMatch
	for _, relative := range bundle.AllFiles {
		select {
		case <-ctx.Done():
			return matches, ctx.Err()
		default:
		}
		path, err := safeWorkspacePath(root, relative)
		if err != nil {
			continue
		}
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		lineNo := 0
		for scanner.Scan() {
			lineNo++
			content := scanner.Text()
			lower := strings.ToLower(content)
			for _, identifier := range identifiers {
				if strings.Contains(lower, identifier) {
					matches = append(matches, judgeSearchMatch{
						Path:    relative,
						LineNo:  lineNo,
						Content: content,
					})
					break
				}
			}
			if len(matches) >= 200 {
				break
			}
		}
		_ = file.Close()
		if len(matches) >= 200 {
			break
		}
	}
	return matches, nil
}

func truncateEvidenceSnippet(value string) string {
	value = strings.TrimSpace(value)
	const max = 2048
	if len(value) <= max {
		return value
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max]) + " ...[truncated]"
}

func sameRelativePath(left, right string) bool {
	return filepath.ToSlash(filepath.Clean(left)) == filepath.ToSlash(filepath.Clean(right))
}

func VerifyJudgeEvidencePacks(packs []JudgeCaseEvidencePack, out *JudgeOutput) (map[string]JudgeEvidenceVerification, error) {
	if out == nil {
		return nil, fmt.Errorf("judge output is nil")
	}
	byID := make(map[string]JudgeCaseEvidencePack, len(packs))
	for _, pack := range packs {
		byID[pack.CandidateID] = pack
	}
	verified := make(map[string]JudgeEvidenceVerification, len(out.FinalVerdicts))
	for i := range out.FinalVerdicts {
		verdict := &out.FinalVerdicts[i]
		pack, ok := byID[verdict.CandidateID]
		if !ok {
			return nil, fmt.Errorf("invalid final_verdict %s: evidence pack not found", verdict.CandidateID)
		}
		result, err := VerifyJudgeEvidence(pack, verdict)
		if err != nil {
			return nil, err
		}
		verdict.EvidenceDegraded = false
		verified[verdict.CandidateID] = result
	}
	return verified, nil
}

// NormalizeJudgeEvidencePacks materializes known evidence IDs before schema
// validation. The verifier later performs the actual source-file checks.
func NormalizeJudgeEvidencePacks(packs []JudgeCaseEvidencePack, out *JudgeOutput) {
	if out == nil {
		return
	}
	byID := make(map[string]JudgeCaseEvidencePack, len(packs))
	for _, pack := range packs {
		byID[pack.CandidateID] = pack
	}
	for i := range out.FinalVerdicts {
		pack, ok := byID[out.FinalVerdicts[i].CandidateID]
		if !ok {
			continue
		}
		for j := range out.FinalVerdicts[i].Evidence {
			out.FinalVerdicts[i].Evidence[j] =
				materializeEvidenceRef(pack, out.FinalVerdicts[i].Evidence[j])
		}
	}
}

func VerifyChallengerEvidence(packs []JudgeCaseEvidencePack, out *ChallengerOutput) (map[string]JudgeEvidenceVerification, error) {
	if out == nil {
		return nil, fmt.Errorf("challenger output is nil")
	}
	byID := make(map[string]JudgeCaseEvidencePack, len(packs))
	for _, pack := range packs {
		byID[pack.CandidateID] = pack
	}
	verified := make(map[string]JudgeEvidenceVerification, len(out.DefenseCases))
	for i := range out.DefenseCases {
		defense := &out.DefenseCases[i]
		pack, ok := byID[defense.CandidateID]
		if !ok {
			return nil, fmt.Errorf("invalid defense_case %s: evidence pack not found", defense.CandidateID)
		}

		var refs []JudgeEvidenceRef
		for _, argument := range defense.DefenseArguments {
			for i := range argument.Evidence {
				argument.Evidence[i] = materializeEvidenceRef(pack, argument.Evidence[i])
				refs = append(refs, argument.Evidence[i])
			}
		}
		if defense.DefenseVerdict != "CHALLENGE_FAILED" && len(refs) == 0 {
			return nil, fmt.Errorf("invalid defense_case %s: %s requires verified evidence", defense.CandidateID, defense.DefenseVerdict)
		}
		result, err := verifyEvidenceRefs(pack, refs)
		if err != nil {
			return nil, fmt.Errorf("invalid defense_case %s: %w", defense.CandidateID, err)
		}
		verified[defense.CandidateID] = result
	}
	return verified, nil
}

// NormalizeChallengerEvidencePacks materializes known evidence IDs before
// schema validation so a model can omit or paraphrase repeated evidence fields
// without causing a false contract failure.
func NormalizeChallengerEvidencePacks(packs []JudgeCaseEvidencePack, out *ChallengerOutput) {
	if out == nil {
		return
	}
	byID := make(map[string]JudgeCaseEvidencePack, len(packs))
	for _, pack := range packs {
		byID[pack.CandidateID] = pack
	}
	for i := range out.DefenseCases {
		pack, ok := byID[out.DefenseCases[i].CandidateID]
		if !ok {
			continue
		}
		for j := range out.DefenseCases[i].DefenseArguments {
			for k := range out.DefenseCases[i].DefenseArguments[j].Evidence {
				out.DefenseCases[i].DefenseArguments[j].Evidence[k] =
					materializeEvidenceRef(pack, out.DefenseCases[i].DefenseArguments[j].Evidence[k])
			}
		}
	}
}

func verifyEvidenceRefs(pack JudgeCaseEvidencePack, refs []JudgeEvidenceRef) (JudgeEvidenceVerification, error) {
	result := JudgeEvidenceVerification{}
	for i := range refs {
		ref := &refs[i]
		if err := verifyEvidenceRef(pack, ref); err != nil {
			result.RejectedEvidenceCount++
			return result, fmt.Errorf("evidence[%d]: %w", i, err)
		}
		result.VerifiedEvidenceCount++
		if ref.Kind != JudgeEvidenceAbsence {
			result.SourceVerified = true
		}
		switch ref.Kind {
		case JudgeEvidenceCaller, JudgeEvidenceDefinition, JudgeEvidenceMacro:
			result.ReachabilityVerified = true
		case JudgeEvidenceAbsence:
			result.AbsenceVerified = true
		}
	}
	return result, nil
}

func VerifyJudgeEvidence(pack JudgeCaseEvidencePack, verdict *JudgeFinalVerdict) (JudgeEvidenceVerification, error) {
	if verdict == nil {
		return JudgeEvidenceVerification{}, fmt.Errorf("final_verdict is nil")
	}
	if verdict.CandidateID != pack.CandidateID {
		return JudgeEvidenceVerification{}, fmt.Errorf("final_verdict candidate_id %q does not match evidence pack %q", verdict.CandidateID, pack.CandidateID)
	}
	if len(verdict.Evidence) == 0 {
		return JudgeEvidenceVerification{}, fmt.Errorf("final_verdict %s: evidence is empty", verdict.CandidateID)
	}

	result := JudgeEvidenceVerification{}
	for i := range verdict.Evidence {
		ref := &verdict.Evidence[i]
		*ref = materializeEvidenceRef(pack, *ref)
		if err := verifyEvidenceRef(pack, ref); err != nil {
			result.RejectedEvidenceCount++
			return result, fmt.Errorf("final_verdict %s evidence[%d]: %w", verdict.CandidateID, i, err)
		}
		result.VerifiedEvidenceCount++
		if ref.Kind != JudgeEvidenceAbsence {
			result.SourceVerified = true
		}
		switch ref.Kind {
		case JudgeEvidenceCaller, JudgeEvidenceDefinition, JudgeEvidenceMacro:
			result.ReachabilityVerified = true
		case JudgeEvidenceAbsence:
			result.AbsenceVerified = true
		}
	}

	switch verdict.Verdict {
	case "CONFIRMED":
		if !result.SourceVerified || !result.ReachabilityVerified {
			return result, fmt.Errorf("CONFIRMED requires verified source and reachability evidence")
		}
	case "CONDITIONAL":
		if !result.SourceVerified {
			return result, fmt.Errorf("CONDITIONAL requires verified source evidence")
		}
	case "REJECTED":
	default:
		return result, fmt.Errorf("invalid verdict %q", verdict.Verdict)
	}
	return result, nil
}

// materializeEvidenceRef replaces a model-repeated evidence object with the
// server-built evidence when the model copied a known evidence_id. This keeps
// path/line/snippet authoritative and prevents harmless prose drift from
// rejecting an otherwise verifiable verdict.
func materializeEvidenceRef(pack JudgeCaseEvidencePack, ref JudgeEvidenceRef) JudgeEvidenceRef {
	if strings.TrimSpace(ref.EvidenceID) == "" {
		// A single absence probe is unambiguous; tolerate the common omission.
		if strings.EqualFold(string(ref.Kind), string(JudgeEvidenceAbsence)) && strings.TrimSpace(ref.ProbeID) == "" && len(pack.AbsenceProbes) == 1 {
			ref.ProbeID = pack.AbsenceProbes[0].ProbeID
		}
		return ref
	}

	evidenceGroups := [][]JudgeEvidenceRef{
		pack.PrimaryEvidence,
		pack.RelatedEvidence,
		pack.MacroEvidence,
	}
	for _, group := range evidenceGroups {
		for _, candidate := range group {
			if candidate.EvidenceID != ref.EvidenceID {
				continue
			}
			return JudgeEvidenceRef{
				EvidenceID: candidate.EvidenceID,
				Kind:       candidate.Kind,
				Path:       candidate.Path,
				LineRange:  candidate.LineRange,
				Snippet:    candidate.Snippet,
				Reason:     candidate.Reason,
			}
		}
	}
	return ref
}

func verifyEvidenceRef(pack JudgeCaseEvidencePack, ref *JudgeEvidenceRef) error {
	switch ref.Kind {
	case JudgeEvidenceTarget, JudgeEvidenceRelated, JudgeEvidenceCaller, JudgeEvidenceDefinition, JudgeEvidenceMacro:
		if strings.TrimSpace(ref.Path) == "" {
			return fmt.Errorf("path is empty")
		}
		if err := validateRelativeSourcePath(ref.Path); err != nil {
			return err
		}
		if !allowedEvidencePath(pack, ref.Path) {
			return fmt.Errorf("path %q is not in the evidence allowlist", ref.Path)
		}
		start, end, err := parseLineRange(ref.LineRange)
		if err != nil {
			return err
		}
		path, err := safeWorkspacePath(pack.WorkspaceRoot, ref.Path)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		totalLines := countLines(content)
		if start > totalLines || end > totalLines || start > end {
			return fmt.Errorf("line range %d-%d is outside file with %d lines", start, end, totalLines)
		}
		if strings.TrimSpace(ref.Snippet) == "" {
			return fmt.Errorf("snippet is empty")
		}
		if !snippetMatches(content, ref.Snippet) {
			return fmt.Errorf("snippet does not match source file %q", ref.Path)
		}
		return nil
	case JudgeEvidenceAbsence:
		if strings.TrimSpace(ref.ProbeID) == "" {
			return fmt.Errorf("probe_id is empty")
		}
		for _, probe := range pack.AbsenceProbes {
			if probe.ProbeID == ref.ProbeID {
				return nil
			}
		}
		return fmt.Errorf("absence probe %q not found", ref.ProbeID)
	default:
		return fmt.Errorf("evidence kind %q is not allowed", ref.Kind)
	}
}

func allowedEvidencePath(pack JudgeCaseEvidencePack, candidate string) bool {
	normalized := filepath.ToSlash(filepath.Clean(candidate))
	for _, path := range pack.AllowedEvidencePaths {
		if filepath.ToSlash(filepath.Clean(path)) == normalized {
			return true
		}
	}
	return false
}

func countLines(content []byte) int {
	if len(content) == 0 {
		return 0
	}
	count := bytes.Count(content, []byte{'\n'})
	if content[len(content)-1] != '\n' {
		count++
	}
	return count
}

func snippetMatches(content []byte, snippet string) bool {
	normalizedContent := normalizeSnippet(string(content))
	normalizedSnippet := normalizeSnippet(snippet)
	if strings.Contains(normalizedContent, normalizedSnippet) {
		return true
	}
	return strings.Contains(collapseWhitespace(normalizedContent), collapseWhitespace(normalizedSnippet))
}

func normalizeSnippet(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return value
}

func collapseWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
