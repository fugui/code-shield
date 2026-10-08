package planner

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"code-shield/services/coverage"
	"code-shield/services/engines"
	"code-shield/services/engines/chunker"
	"code-shield/services/engines/profile"
)

const (
	DecisionProceed         = "proceed"
	DecisionProceedDegraded = "proceed_degraded"
	DecisionSkipped         = "skipped"
	DecisionFailed          = "failed"

	ReasonPrimaryScopeReady   = "PRIMARY_SCOPE_READY"
	ReasonNoLanguageScope     = "NO_LANGUAGE_SCOPE"
	ReasonNoKeywordMatch      = "NO_KEYWORD_MATCH"
	ReasonNoKeywordOccurrence = "NO_KEYWORD_OCCURRENCE"
	ReasonNoTestScope         = "NO_TEST_SCOPE"
	ReasonEntityPlanFailed    = "ENTITY_PLAN_FAILED"
	ReasonPrimaryScopeFailed  = "PRIMARY_SCOPE_FAILED"
	ReasonPartialPrimaryScope = "PRIMARY_SCOPE_PARTIAL_FAILED"
	ReasonContextDegraded     = "CONTEXT_DEGRADED"
	ReasonNoDiff              = "NO_DIFF"
	ReasonInvalidBase         = "INVALID_BASE"
	ReasonInsufficientHistory = "INSUFFICIENT_HISTORY"
)

type ScopeDecision struct {
	Decision               string          `json:"decision"`
	Reason                 string          `json:"reason"`
	ScopeProfile           string          `json:"scope_profile"`
	PrimaryUnit            string          `json:"primary_unit"`
	CandidateFiles         int             `json:"candidate_files"`
	PrimaryUnits           int             `json:"primary_units"`
	EntityUnits            int             `json:"entity_units"`
	KeywordOccurrenceUnits int             `json:"keyword_occurrence_units"`
	FailedFiles            int             `json:"failed_files"`
	UnknownFiles           int             `json:"unknown_files"`
	FailedUnits            int             `json:"failed_units"`
	UnknownUnits           int             `json:"unknown_units"`
	ChangeHunks            int             `json:"change_hunks"`
	PlanManifestHash       string          `json:"plan_manifest_hash"`
	ChangeOverview         *ChangeOverview `json:"change_overview,omitempty"`
	Message                string          `json:"message,omitempty"`
	ChangeBaseline         *ChangeBaseline `json:"change_baseline,omitempty"`
}

type KeywordOccurrencePlan struct {
	Version       int                 `json:"version"`
	Profile       string              `json:"profile"`
	PrimaryUnit   string              `json:"primary_unit"`
	ManifestHash  string              `json:"manifest_hash"`
	OccurrenceIDs []string            `json:"occurrence_ids"`
	Occurrences   []KeywordOccurrence `json:"occurrences"`
}

type KeywordOccurrence struct {
	OccurrenceID   string `json:"occurrence_id"`
	Keyword        string `json:"keyword"`
	FilePath       string `json:"file_path"`
	Line           int    `json:"line"`
	Column         int    `json:"column"`
	EnclosingScope string `json:"enclosing_scope,omitempty"`
	ContextHash    string `json:"context_hash"`
}

type Input struct {
	CodesPath     string
	Profile       profile.ScanProfile
	ProfileHash   string
	Config        engines.ChunkConfig
	ScanStartedAt time.Time
	TargetScope   string
	ChangePlan    *ChangePlan
}

type PrimaryPlan interface {
	PrimaryCount() int
	PrimaryManifestHash() string
	PrimaryUnits() []coverage.PlanUnit
}

type FilePlan struct {
	Version      int      `json:"version"`
	Profile      string   `json:"profile"`
	PrimaryUnit  string   `json:"primary_unit"`
	ManifestHash string   `json:"manifest_hash"`
	Files        []string `json:"files"`
}

var scopePattern = regexp.MustCompile(`(?i)(?:func\s+(?:\([^)]*\)\s*)?([A-Za-z0-9_]+)|(?:class|struct|interface)\s+([A-Za-z0-9_]+)|([A-Za-z0-9_]+)\s*::\s*~?[A-Za-z0-9_]+\s*\(|(?:void|int|bool|auto|static)\s+([A-Za-z0-9_]+)\s*\()`)

func Build(input Input) (ScopeDecision, *coverage.ScanPlan, PrimaryPlan, error) {
	switch input.Profile.Name {
	case profile.NameFullReview, profile.NameOccurrenceReview, profile.NameKeywordReview:
		return buildFileScope(input)
	case profile.NameChangeReview:
		return buildChangeScope(input)
	case profile.NameEntityReview, profile.NameTestEntityReview:
		return buildEntityScope(input)
	default:
		return ScopeDecision{}, nil, nil, fmt.Errorf("unsupported planner profile: %q", input.Profile.Name)
	}
}

func buildChangeScope(input Input) (ScopeDecision, *coverage.ScanPlan, PrimaryPlan, error) {
	changePlan := input.ChangePlan
	if changePlan == nil {
		return ScopeDecision{
			Decision:     DecisionFailed,
			Reason:       ReasonInvalidBase,
			ScopeProfile: input.Profile.Name,
			PrimaryUnit:  "change_hunk",
			Message:      "ChangePlan was not precomputed",
		}, nil, nil, nil
	}
	if changePlan.Profile != input.Profile.Name || changePlan.ProfileHash != input.ProfileHash {
		return ScopeDecision{
			Decision:     DecisionFailed,
			Reason:       ReasonInvalidBase,
			ScopeProfile: input.Profile.Name,
			PrimaryUnit:  "change_hunk",
			Message:      "ChangePlan profile identity does not match scan profile",
		}, nil, nil, nil
	}

	decision := ScopeDecision{
		Decision:         DecisionProceed,
		Reason:           ReasonPrimaryScopeReady,
		ScopeProfile:     input.Profile.Name,
		PrimaryUnit:      "change_hunk",
		CandidateFiles:   len(changePlan.Files),
		PrimaryUnits:     changePlan.PrimaryCount(),
		ChangeHunks:      changePlan.PrimaryCount(),
		PlanManifestHash: changePlan.PrimaryManifestHash(),
		ChangeOverview:   changePlan.Overview(),
		ChangeBaseline:   &changePlan.Baseline,
	}

	scanPlan := &coverage.ScanPlan{
		Selected: make([]coverage.PlannedFile, 0, len(changePlan.Files)),
	}
	for _, file := range changePlan.Files {
		hunkRanges := make([]string, 0, len(file.Hunks))
		for _, hunk := range file.Hunks {
			start, end := hunk.NewStart, hunk.NewStart+hunk.NewLines-1
			if hunk.NewLines == 0 {
				start, end = hunk.OldStart, hunk.OldStart+hunk.OldLines-1
			}
			if hunk.NewLines == 0 || hunk.OldLines == 0 {
				hunkRanges = append(hunkRanges, strconv.Itoa(start))
				continue
			}
			hunkRanges = append(hunkRanges, fmt.Sprintf("%d-%d", start, end))
		}
		scanPlan.Selected = append(scanPlan.Selected, coverage.PlannedFile{
			Path:        file.Path,
			DiffTouched: true,
			Deleted:     file.ChangeKind == ChangeKindDeleted,
			HunkRanges:  hunkRanges,
		})
	}

	if len(changePlan.Files) == 0 {
		decision.Decision = DecisionSkipped
		decision.Reason = ReasonNoDiff
		return decision, scanPlan, nil, nil
	}
	return decision, scanPlan, changePlan, nil
}

func buildFileScope(input Input) (ScopeDecision, *coverage.ScanPlan, PrimaryPlan, error) {
	plan, err := chunker.PlanFiles(input.CodesPath, input.Config, input.TargetScope)
	if err != nil {
		return ScopeDecision{
			Decision:     DecisionFailed,
			Reason:       ReasonPrimaryScopeFailed,
			ScopeProfile: input.Profile.Name,
			PrimaryUnit:  primaryUnit(input.Profile),
			Message:      err.Error(),
		}, nil, nil, nil
	}

	decision := ScopeDecision{
		Decision:       DecisionProceed,
		Reason:         ReasonPrimaryScopeReady,
		ScopeProfile:   input.Profile.Name,
		PrimaryUnit:    primaryUnit(input.Profile),
		CandidateFiles: len(plan.Selected),
		PrimaryUnits:   len(plan.Selected),
		UnknownFiles:   len(plan.Unknown),
		UnknownUnits:   len(plan.Unknown),
	}
	planManifestHash := manifestHash("file-plan|"+input.ProfileHash, plan.Selected)
	filePlan := &FilePlan{
		Version:      1,
		Profile:      input.Profile.Name,
		PrimaryUnit:  decision.PrimaryUnit,
		ManifestHash: planManifestHash,
		Files:        make([]string, 0, len(plan.Selected)),
	}
	for _, planned := range plan.Selected {
		filePlan.Files = append(filePlan.Files, planned.Path)
	}
	decision.PlanManifestHash = planManifestHash

	if len(plan.Selected) == 0 {
		if decision.UnknownFiles > 0 {
			decision.Decision = DecisionFailed
			decision.Reason = ReasonPrimaryScopeFailed
			decision.Message = "all primary files are unknown"
		} else if len(input.Profile.ContentKeywords) > 0 && countReason(plan.Excluded, coverage.ReasonContentKeyword) > 0 {
			decision.Decision = DecisionSkipped
			decision.Reason = ReasonNoKeywordMatch
		} else if input.TargetScope == "test" {
			decision.Decision = DecisionSkipped
			decision.Reason = ReasonNoTestScope
		} else {
			decision.Decision = DecisionSkipped
			decision.Reason = ReasonNoLanguageScope
		}
		return decision, &plan, filePlan, nil
	}

	if decision.UnknownFiles > 0 {
		decision.Decision = DecisionProceedDegraded
		decision.Reason = ReasonPartialPrimaryScope
	}

	var keywordPlan *KeywordOccurrencePlan
	if profile.IsOccurrenceReview(input.Profile.Name) && input.Profile.PrimaryUnit == profile.PrimaryUnitKeywordOccurrence {
		keywordPlan, err = buildKeywordOccurrences(input, plan.Selected)
		if err != nil {
			return ScopeDecision{
				Decision:       DecisionFailed,
				Reason:         ReasonPrimaryScopeFailed,
				ScopeProfile:   input.Profile.Name,
				PrimaryUnit:    input.Profile.PrimaryUnit,
				CandidateFiles: len(plan.Selected),
				UnknownFiles:   len(plan.Unknown),
				Message:        err.Error(),
			}, &plan, nil, nil
		}
		decision.PrimaryUnits = len(keywordPlan.Occurrences)
		decision.KeywordOccurrenceUnits = len(keywordPlan.Occurrences)
		decision.PlanManifestHash = keywordPlan.ManifestHash
		if len(keywordPlan.Occurrences) == 0 {
			decision.Decision = DecisionSkipped
			decision.Reason = ReasonNoKeywordOccurrence
		}
	}

	if keywordPlan != nil {
		return decision, &plan, keywordPlan, nil
	}
	return decision, &plan, filePlan, nil
}

func buildKeywordOccurrences(input Input, selected []coverage.PlannedFile) (*KeywordOccurrencePlan, error) {
	keywords := append([]string(nil), input.Profile.ContentKeywords...)
	result := &KeywordOccurrencePlan{
		Version:     1,
		Profile:     input.Profile.Name,
		PrimaryUnit: input.Profile.PrimaryUnit,
	}
	for _, selectedFile := range selected {
		file, err := os.Open(filepath.Join(input.CodesPath, selectedFile.Path))
		if err != nil {
			result.OccurrenceIDs = append(result.OccurrenceIDs, failedOccurrenceID(selectedFile.Path))
			continue
		}

		scanner := bufio.NewScanner(file)
		lineNumber := 0
		var lines []string
		for scanner.Scan() {
			line := scanner.Text()
			lines = append(lines, line)
			lineNumber++
			for _, keyword := range keywords {
				column := strings.Index(line, keyword)
				if column < 0 {
					continue
				}
				occurrence := KeywordOccurrence{
					Keyword:        keyword,
					FilePath:       selectedFile.Path,
					Line:           lineNumber,
					Column:         column + 1,
					EnclosingScope: enclosingScope(lines),
					ContextHash:    contextHash(selectedFile.Path, line),
				}
				occurrence.OccurrenceID = occurrenceID(occurrence)
				result.Occurrences = append(result.Occurrences, occurrence)
				result.OccurrenceIDs = append(result.OccurrenceIDs, occurrence.OccurrenceID)
			}
		}
		closeErr := file.Close()
		if err = scanner.Err(); err == nil {
			err = closeErr
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", selectedFile.Path, err)
		}
	}
	result.ManifestHash = manifestHash("keyword-occurrence-plan|"+input.ProfileHash, result.Occurrences)
	return result, nil
}

func primaryUnit(scanProfile profile.ScanProfile) string {
	if scanProfile.PrimaryUnit == "" {
		return profile.PrimaryUnitFile
	}
	return scanProfile.PrimaryUnit
}

func (plan *KeywordOccurrencePlan) PrimaryCount() int {
	return len(plan.Occurrences)
}

func (plan *KeywordOccurrencePlan) PrimaryManifestHash() string {
	return plan.ManifestHash
}

func (plan *KeywordOccurrencePlan) PrimaryUnits() []coverage.PlanUnit {
	units := make([]coverage.PlanUnit, 0, len(plan.Occurrences))
	for _, occurrence := range plan.Occurrences {
		displayName := fmt.Sprintf("%s @ %s:%d", occurrence.Keyword, occurrence.FilePath, occurrence.Line)
		units = append(units, coverage.PlanUnit{
			ID:          occurrence.OccurrenceID,
			Kind:        coverage.PlanUnitKeywordOccurrence,
			Path:        occurrence.FilePath,
			DisplayName: displayName,
			StartLine:   occurrence.Line,
			EndLine:     occurrence.Line,
		})
	}
	return units
}

func (plan *FilePlan) PrimaryCount() int {
	return len(plan.Files)
}

func (plan *FilePlan) PrimaryManifestHash() string {
	return plan.ManifestHash
}

func (plan *FilePlan) PrimaryUnits() []coverage.PlanUnit {
	units := make([]coverage.PlanUnit, 0, len(plan.Files))
	for _, path := range plan.Files {
		units = append(units, coverage.PlanUnit{
			ID:   path,
			Kind: coverage.PlanUnitFile,
			Path: path,
		})
	}
	return units
}

func countReason(files []coverage.PlannedFile, reason string) int {
	count := 0
	for _, item := range files {
		if item.Reason == reason {
			count++
		}
	}
	return count
}

func enclosingScope(lines []string) string {
	for index := len(lines) - 1; index >= 0; index-- {
		match := scopePattern.FindStringSubmatch(lines[index])
		if match == nil {
			continue
		}
		for _, value := range match[1:] {
			if value != "" {
				return value
			}
		}
	}
	return ""
}

func occurrenceID(occurrence KeywordOccurrence) string {
	raw := occurrence.Keyword + "\x1f" + occurrence.FilePath + "\x1f" + fmt.Sprint(occurrence.Line) + "\x1f" + fmt.Sprint(occurrence.Column)
	return hash("occurrence|" + raw)
}

func failedOccurrenceID(path string) string {
	return hash("occurrence-failed|" + path)
}

func contextHash(path, line string) string {
	return hash("context|" + path + "\x1f" + line)
}

func manifestHash(prefix string, value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return hash(prefix + "\x1f" + string(raw))
}

func hash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return "sha256:" + hex.EncodeToString(sum[:])
}
