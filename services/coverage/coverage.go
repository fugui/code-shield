package coverage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	StatusPlanned          = "planned"
	StatusSuccess          = "success"
	StatusFailed           = "failed"
	StatusExcluded         = "excluded"
	StatusUnchangedSkipped = "unchanged_skipped"
	StatusSkipped          = "skipped"
	StatusUnknown          = "unknown"
)

const (
	StateUnknown       = "UNKNOWN"
	StateComplete      = "COMPLETE"
	StatePartial       = "PARTIAL"
	StateFailed        = "FAILED"
	StateNotApplicable = "NOT_APPLICABLE"
)

const (
	ReasonNoPlannedFiles     = "NO_PLANNED_FILES"
	ReasonManifestMissing    = "FILE_MANIFEST_MISSING"
	ReasonNotSourceFile      = "NOT_SOURCE_FILE"
	ReasonTargetScope        = "TARGET_SCOPE"
	ReasonGeneratedFile      = "GENERATED_FILE"
	ReasonExcludePath        = "EXCLUDE_PATH"
	ReasonContentKeyword     = "CONTENT_KEYWORD"
	ReasonContentFilterError = "CONTENT_FILTER_ERROR"
)

type PlannedFile struct {
	Path        string   `json:"path"`
	Reason      string   `json:"reason,omitempty"`
	DiffTouched bool     `json:"diff_touched"`
	Deleted     bool     `json:"deleted,omitempty"`
	HunkRanges  []string `json:"hunk_ranges,omitempty"`
}

type ScanPlan struct {
	Selected         []PlannedFile `json:"selected"`
	Excluded         []PlannedFile `json:"excluded"`
	UnchangedSkipped []PlannedFile `json:"unchanged_skipped"`
	Unknown          []PlannedFile `json:"unknown,omitempty"`
}

type File struct {
	Path              string   `json:"path"`
	Hash              string   `json:"hash,omitempty"`
	ChunkPolicyID     string   `json:"chunk_policy_id,omitempty"`
	ChunkUID          string   `json:"chunk_uid,omitempty"`
	ChunkSnapshotHash string   `json:"chunk_snapshot_hash,omitempty"`
	Status            string   `json:"status"`
	Reason            string   `json:"reason,omitempty"`
	Error             string   `json:"error,omitempty"`
	ErrorClass        string   `json:"error_class,omitempty"`
	DiffTouched       bool     `json:"diff_touched"`
	Deleted           bool     `json:"deleted,omitempty"`
	HunkRanges        []string `json:"hunk_ranges,omitempty"`
}

type Chunk struct {
	UID          string        `json:"uid"`
	Status       string        `json:"status"`
	Error        string        `json:"error,omitempty"`
	ErrorClass   string        `json:"error_class,omitempty"`
	Files        []string      `json:"files"`
	PlannedFiles []PlannedFile `json:"planned_files,omitempty"`
	PlanOnly     bool          `json:"plan_only,omitempty"`
}

type ExecutionFailure struct {
	ChunkUID      string `json:"chunk_uid,omitempty"`
	ChunkName     string `json:"chunk_name,omitempty"`
	PrimaryUnitID string `json:"primary_unit_id,omitempty"`
	FilePath      string `json:"file_path,omitempty"`
	Stage         string `json:"stage,omitempty"`
	ErrorClass    string `json:"error_class,omitempty"`
	ErrorMessage  string `json:"error_message,omitempty"`
}

type ExecutionSummary struct {
	CoverageState         string             `json:"coverage_state"`
	TotalChunks           int                `json:"total_chunks"`
	ProcessedChunks       int                `json:"processed_chunks"`
	SuccessChunks         int                `json:"success_chunks"`
	FailedChunks          int                `json:"failed_chunks"`
	MissingAssessments    []string           `json:"missing_assessments"`
	FailedFileDetails     []ExecutionFailure `json:"failed_files"`
	FailedChunkDetails    []ExecutionFailure `json:"failed_chunk_items"`
	CoverageComplete      bool               `json:"coverage_complete"`
	CoverageDegraded      bool               `json:"coverage_degraded"`
	CoverageNotApplicable bool               `json:"coverage_not_applicable"`
}

type Coverage struct {
	PolicyVersion      string              `json:"policy_version"`
	ChunkPolicyID      string              `json:"chunk_policy_id,omitempty"`
	CommitHash         string              `json:"commit_hash,omitempty"`
	CommitVerified     bool                `json:"commit_verified"`
	WorktreeClean      bool                `json:"worktree_clean"`
	AnalysisComplete   bool                `json:"analysis_complete"`
	ManifestMissing    bool                `json:"manifest_missing,omitempty"`
	PlanReconciliation *PlanReconciliation `json:"plan_reconciliation,omitempty"`
	Files              []File              `json:"files"`
}

type Summary struct {
	PolicyVersion         string              `json:"policy_version"`
	ChunkPolicyID         string              `json:"chunk_policy_id,omitempty"`
	PlannedFiles          int                 `json:"planned_files"`
	AnalysisComplete      bool                `json:"analysis_complete"`
	ScannedFiles          int                 `json:"scanned_files"`
	SkippedFiles          int                 `json:"skipped_files"`
	ExcludedFiles         int                 `json:"excluded_files"`
	UnchangedSkippedFiles int                 `json:"unchanged_skipped_files"`
	FailedFiles           int                 `json:"failed_files"`
	UnknownFiles          int                 `json:"unknown_files"`
	SuccessChunks         int                 `json:"success_chunks"`
	FailedChunks          int                 `json:"failed_chunks"`
	SkippedChunks         int                 `json:"skipped_chunks"`
	UnknownChunks         int                 `json:"unknown_chunks"`
	CommitVerified        bool                `json:"commit_verified"`
	WorktreeClean         bool                `json:"worktree_clean"`
	CoverageComplete      bool                `json:"coverage_complete"`
	CoverageDegraded      bool                `json:"coverage_degraded"`
	ComparabilityDegraded bool                `json:"comparability_degraded"`
	Degraded              bool                `json:"degraded"`
	CoverageNotApplicable bool                `json:"coverage_not_applicable"`
	NoScopeReason         string              `json:"no_scope_reason,omitempty"`
	ManifestMissing       bool                `json:"manifest_missing,omitempty"`
	PlanReconciliation    *PlanReconciliation `json:"plan_reconciliation,omitempty"`
	PlannedPrimaryUnits   int                 `json:"planned_primary_units,omitempty"`
	MatchedPrimaryUnits   int                 `json:"matched_primary_units,omitempty"`
	MissingAssessments    []string            `json:"missing_assessments,omitempty"`
	UnmatchedAssessments  []string            `json:"unmatched_assessments,omitempty"`
	CoverageReasons       []string            `json:"coverage_degrade_reasons,omitempty"`
	ComparabilityReasons  []string            `json:"comparability_degrade_reasons,omitempty"`
	FailedFileItems       []ExecutionFailure  `json:"failed_file_items,omitempty"`
	FailedChunkItems      []ExecutionFailure  `json:"failed_chunk_items,omitempty"`
}

func (summary Summary) State() string {
	switch {
	case summary.ManifestMissing:
		return "FAILED"
	case summary.CoverageNotApplicable:
		return "NOT_APPLICABLE"
	case summary.CoverageComplete:
		return "COMPLETE"
	case summary.CoverageDegraded:
		return "PARTIAL"
	default:
		return "UNKNOWN"
	}
}

func TerminalTaskStatus(summary ExecutionSummary, stageFailed bool) string {
	if stageFailed || summary.CoverageState == StateFailed ||
		(summary.TotalChunks > 0 && summary.FailedChunks >= summary.TotalChunks) {
		return "failed"
	}
	if summary.CoverageState == StatePartial || summary.FailedChunks > 0 ||
		len(summary.MissingAssessments) > 0 || summary.CoverageDegraded ||
		summary.CoverageState == StateUnknown {
		return "degraded"
	}
	if summary.CoverageState == StateComplete || summary.CoverageState == StateNotApplicable {
		return "success"
	}
	return "degraded"
}

// StableChunkID derives a policy-aware identity from the sorted file paths.
func StableChunkID(policyID string, paths []string) string {
	sorted := append([]string(nil), paths...)
	sort.Strings(sorted)
	return shortHash("chunk|" + policyID + "|" + strings.Join(sorted, "\x1f"))
}

func ChunkPolicyID(taskTypeKey string, engineConfig []byte) string {
	digest := sha256.Sum256(append([]byte(taskTypeKey+"|"), engineConfig...))
	return "policy-" + hex.EncodeToString(digest[:])[:16]
}

func SnapshotHash(chunkUID string, hashes map[string]string, paths []string) string {
	sorted := append([]string(nil), paths...)
	sort.Strings(sorted)
	parts := make([]string, 0, len(sorted))
	for _, path := range sorted {
		parts = append(parts, path+"|"+hashes[path])
	}
	return "snap-" + shortHash(chunkUID+"|"+strings.Join(parts, "\x1f"))
}

func shortHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])[:16]
}

// NormalizeRepoPath returns a slash-separated, relative, stable path.
func NormalizeRepoPath(repoRoot, path string) string {
	clean := filepath.ToSlash(strings.TrimSpace(path))
	if clean == "" {
		return ""
	}
	clean = filepath.Clean(strings.TrimPrefix(clean, "./"))
	if filepath.IsAbs(clean) && repoRoot != "" {
		if rel, err := filepath.Rel(repoRoot, clean); err == nil && !strings.HasPrefix(rel, "..") {
			clean = rel
		}
	}
	return filepath.ToSlash(filepath.Clean(strings.TrimPrefix(clean, "./")))
}

// Build creates an immutable scan-coverage fact from planned chunks.
func Build(repoRoot, commitHash, policyVersion, chunkPolicyID string, chunks []Chunk) Coverage {
	pathChunks := make(map[string][]int)
	plannedOwners := make(map[string]int)
	plannedFiles := make(map[string]PlannedFile)
	conflicts := make(map[string]bool)
	orderedPaths := make([]string, 0)
	seen := make(map[string]bool)

	for chunkIdx, chunk := range chunks {
		for _, planned := range chunk.PlannedFiles {
			path := NormalizeRepoPath(repoRoot, planned.Path)
			if path == "" {
				continue
			}
			if !seen[path] {
				seen[path] = true
				orderedPaths = append(orderedPaths, path)
			}
			if _, exists := plannedOwners[path]; exists {
				conflicts[path] = true
			}
			plannedOwners[path] = chunkIdx
			plannedFiles[path] = planned
		}
		for _, rawPath := range chunk.Files {
			path := NormalizeRepoPath(repoRoot, rawPath)
			if path == "" {
				continue
			}
			if !seen[path] {
				seen[path] = true
				orderedPaths = append(orderedPaths, path)
			}
			pathChunks[path] = append(pathChunks[path], chunkIdx)
		}
	}
	sort.Strings(orderedPaths)

	hashes := make(map[string]string, len(orderedPaths))
	for _, path := range orderedPaths {
		hash, err := FileHash(repoRoot, path)
		if err == nil {
			hashes[path] = hash
		}
	}

	chunkIDs := make(map[int]string, len(chunks))
	chunkSnapshots := make(map[int]string, len(chunks))
	for i := range chunks {
		paths := make([]string, 0, len(chunks[i].Files))
		for _, path := range chunks[i].Files {
			if normalized := NormalizeRepoPath(repoRoot, path); normalized != "" {
				paths = append(paths, normalized)
			}
		}
		for _, planned := range chunks[i].PlannedFiles {
			if normalized := NormalizeRepoPath(repoRoot, planned.Path); normalized != "" {
				paths = append(paths, normalized)
			}
		}
		id := chunks[i].UID
		if id == "" {
			id = StableChunkID(chunkPolicyID, paths)
		}
		chunkIDs[i] = id
		chunkSnapshots[i] = SnapshotHash(id, hashes, paths)
	}

	files := make([]File, 0, len(orderedPaths))
	for _, path := range orderedPaths {
		ownerIndexes := pathChunks[path]
		status := StatusUnknown
		var reason string
		var errorText string
		var errorClass string
		var diffTouched bool
		var deleted bool
		var hunkRanges []string
		var owner *Chunk
		var ownerIndex int
		plannedChunkIndex, hasPlanned := plannedOwners[path]
		if len(ownerIndexes) == 1 && !conflicts[path] {
			ownerIndex = ownerIndexes[0]
			owner = &chunks[ownerIndex]
			status = normalizeStatus(owner.Status)
			errorText = owner.Error
			errorClass = owner.ErrorClass
		}
		if hasPlanned && !conflicts[path] {
			planned := plannedFiles[path]
			status = normalizeStatus(chunks[plannedChunkIndex].Status)
			reason = planned.Reason
			diffTouched = planned.DiffTouched
			deleted = planned.Deleted
			hunkRanges = planned.HunkRanges
			if status == StatusUnknown {
				status = StatusPlanned
			}
		}
		if len(ownerIndexes) > 1 {
			status, errorText, errorClass = aggregateChunkStatuses(chunks, ownerIndexes)
		}

		item := File{
			Path:          path,
			Hash:          hashes[path],
			ChunkPolicyID: chunkPolicyID,
			Status:        status,
			Reason:        reason,
			DiffTouched:   diffTouched,
			Deleted:       deleted,
			HunkRanges:    hunkRanges,
		}
		if owner != nil {
			item.ChunkUID = chunkIDs[ownerIndex]
			item.ChunkSnapshotHash = chunkSnapshots[ownerIndex]
			item.Error = errorText
			item.ErrorClass = errorClass
		}
		if len(ownerIndexes) > 1 && status == StatusFailed {
			item.Error = errorText
			item.ErrorClass = errorClass
		}
		if item.Hash == "" && item.Status == StatusSuccess && !item.Deleted {
			item.Status = StatusUnknown
			if item.Error == "" {
				item.Error = "file hash unavailable"
			}
		}
		files = append(files, item)
	}

	return Coverage{
		PolicyVersion:    policyVersion,
		ChunkPolicyID:    chunkPolicyID,
		CommitHash:       commitHash,
		Files:            files,
		AnalysisComplete: allAnalysisComplete(files),
	}
}

func aggregateChunkStatuses(chunks []Chunk, indexes []int) (status, errorText, errorClass string) {
	status = StatusUnknown
	allSuccess := true
	for _, chunkIdx := range indexes {
		switch normalizeStatus(chunks[chunkIdx].Status) {
		case StatusFailed:
			return StatusFailed, chunks[chunkIdx].Error, chunks[chunkIdx].ErrorClass
		case StatusSuccess:
			continue
		default:
			allSuccess = false
		}
	}
	if allSuccess {
		status = StatusSuccess
	}
	return status, "", ""
}

func normalizeStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case StatusSuccess:
		return StatusSuccess
	case StatusFailed:
		return StatusFailed
	case StatusExcluded:
		return StatusExcluded
	case StatusUnchangedSkipped:
		return StatusUnchangedSkipped
	case StatusSkipped:
		return StatusSkipped
	default:
		return StatusUnknown
	}
}

func allAnalysisComplete(files []File) bool {
	if len(files) == 0 {
		return false
	}
	for _, file := range files {
		if file.Status == StatusExcluded {
			continue
		}
		if file.Status != StatusSuccess {
			return false
		}
	}
	return true
}

func FileHash(repoRoot, path string) (string, error) {
	fullPath := path
	if repoRoot != "" && !filepath.IsAbs(path) {
		fullPath = filepath.Join(repoRoot, path)
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// VerifyWorkspace checks the actual HEAD and dirty state of a repository.
func VerifyWorkspace(repoRoot, expectedCommit string) (actualCommit string, commitVerified, worktreeClean bool, err error) {
	if repoRoot == "" {
		return "", false, false, fmt.Errorf("repo root is empty")
	}
	out, err := exec.Command("git", "-C", repoRoot, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", false, false, fmt.Errorf("resolve git HEAD: %w", err)
	}
	actualCommit = strings.TrimSpace(string(out))
	commitVerified = expectedCommit == "" || strings.EqualFold(actualCommit, strings.TrimSpace(expectedCommit))

	statusOut, statusErr := exec.Command("git", "-C", repoRoot, "status", "--porcelain").Output()
	worktreeClean = statusErr == nil && strings.TrimSpace(string(statusOut)) == ""
	return actualCommit, commitVerified, worktreeClean, nil
}

func (c Coverage) Summary() Summary {
	if c.ManifestMissing {
		return Summary{
			PolicyVersion:      c.PolicyVersion,
			ChunkPolicyID:      c.ChunkPolicyID,
			AnalysisComplete:   c.AnalysisComplete,
			CommitVerified:     c.CommitVerified,
			WorktreeClean:      c.WorktreeClean,
			CoverageComplete:   false,
			CoverageDegraded:   true,
			Degraded:           true,
			ManifestMissing:    true,
			PlanReconciliation: c.PlanReconciliation,
			CoverageReasons:    []string{ReasonManifestMissing},
		}
	}

	summary := Summary{
		PolicyVersion:    c.PolicyVersion,
		ChunkPolicyID:    c.ChunkPolicyID,
		AnalysisComplete: c.AnalysisComplete,
		CommitVerified:   c.CommitVerified,
		WorktreeClean:    c.WorktreeClean,
	}
	if c.PlanReconciliation != nil {
		summary.PlanReconciliation = c.PlanReconciliation
		summary.PlannedPrimaryUnits = c.PlanReconciliation.PlannedUnits
		summary.MatchedPrimaryUnits = c.PlanReconciliation.MatchedUnits
		summary.MissingAssessments = append([]string(nil), c.PlanReconciliation.MissingUnits...)
		summary.UnmatchedAssessments = append([]string(nil), c.PlanReconciliation.UnmatchedUnits...)
	}
	chunkStatus := make(map[string]string)
	chunkErrors := make(map[string]string)
	failedFiles := make([]ExecutionFailure, 0)
	failedChunks := make(map[string]ExecutionFailure)
	for _, file := range c.Files {
		summary.PlannedFiles++
		switch file.Status {
		case StatusSuccess:
			summary.ScannedFiles++
		case StatusFailed:
			summary.FailedFiles++
		case StatusExcluded:
			summary.ExcludedFiles++
		case StatusUnchangedSkipped:
			summary.UnchangedSkippedFiles++
		case StatusSkipped:
			summary.SkippedFiles++
		default:
			summary.UnknownFiles++
		}
		if file.ChunkUID != "" {
			if strings.HasPrefix(file.ChunkUID, "coverage-plan-") {
				continue
			}
			chunkStatus[file.ChunkUID] = file.Status
			if file.Error != "" {
				chunkErrors[file.ChunkUID] = file.Error
			}
		}
		switch file.Status {
		case StatusFailed:
			failure := ExecutionFailure{
				ChunkUID:     file.ChunkUID,
				ChunkName:    file.ChunkUID,
				FilePath:     file.Path,
				Stage:        "analysis",
				ErrorClass:   firstNonEmpty(file.ErrorClass, "unknown"),
				ErrorMessage: file.Error,
			}
			if failure.ErrorMessage == "" {
				failure.ErrorMessage = file.Reason
			}
			failedFiles = append(failedFiles, failure)
			if failure.ChunkUID != "" {
				failedChunks[failure.ChunkUID] = failure
			}
		case StatusUnknown:
			failedFiles = append(failedFiles, ExecutionFailure{
				ChunkUID:     file.ChunkUID,
				ChunkName:    file.ChunkUID,
				FilePath:     file.Path,
				Stage:        "analysis",
				ErrorMessage: file.Reason,
			})
		}
	}
	for _, status := range chunkStatus {
		switch status {
		case StatusSuccess:
			summary.SuccessChunks++
		case StatusFailed:
			summary.FailedChunks++
		case StatusSkipped:
			summary.SkippedChunks++
		default:
			summary.UnknownChunks++
		}
	}

	if summary.PlannedFiles == 0 {
		summary.CoverageNotApplicable = true
		summary.NoScopeReason = ReasonNoPlannedFiles
		summary.CoverageComplete = false
		summary.CoverageDegraded = false
		summary.Degraded = summary.ComparabilityDegraded
		return summary
	}

	summary.CoverageComplete = summary.AnalysisComplete &&
		summary.CommitVerified &&
		summary.WorktreeClean &&
		summary.ScannedFiles+summary.ExcludedFiles > 0 &&
		summary.ScannedFiles+summary.ExcludedFiles == summary.PlannedFiles &&
		summary.FailedChunks == 0

	if !summary.AnalysisComplete {
		summary.CoverageReasons = append(summary.CoverageReasons, "ANALYSIS_INCOMPLETE")
	}
	if summary.FailedFiles > 0 || summary.FailedChunks > 0 {
		summary.CoverageReasons = append(summary.CoverageReasons, "CHUNK_FAILED")
	}
	if summary.SkippedFiles > 0 || summary.SkippedChunks > 0 {
		summary.CoverageReasons = append(summary.CoverageReasons, "FILE_NOT_SCANNED")
	}
	if summary.UnchangedSkippedFiles > 0 {
		summary.CoverageReasons = append(summary.CoverageReasons, "UNCHANGED_SKIPPED")
	}
	if summary.UnknownFiles > 0 || summary.UnknownChunks > 0 {
		summary.CoverageReasons = append(summary.CoverageReasons, "FILE_NOT_SCANNED")
		summary.CoverageReasons = append(summary.CoverageReasons, "COVERAGE_UNKNOWN")
	}
	if !summary.WorktreeClean {
		summary.CoverageReasons = append(summary.CoverageReasons, "WORKTREE_DIRTY")
	}
	if !summary.CommitVerified {
		summary.CoverageReasons = append(summary.CoverageReasons, "COMMIT_MISMATCH")
	}
	if len(summary.MissingAssessments) > 0 {
		summary.CoverageReasons = append(summary.CoverageReasons, "MISSING_ASSESSMENT")
	}
	if len(summary.UnmatchedAssessments) > 0 {
		summary.CoverageReasons = append(summary.CoverageReasons, "UNMATCHED_ASSESSMENT")
	}
	if len(summary.MissingAssessments) > 0 || len(summary.UnmatchedAssessments) > 0 {
		summary.CoverageComplete = false
		summary.CoverageDegraded = true
		summary.Degraded = true
	}
	summary.CoverageDegraded = !summary.CoverageComplete
	summary.Degraded = summary.CoverageDegraded || summary.ComparabilityDegraded
	if len(failedFiles) > 0 {
		summary.FailedFileItems = failedFiles
	}
	for _, failure := range failedChunks {
		summary.FailedChunkItems = append(summary.FailedChunkItems, failure)
	}
	return summary
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (c Coverage) StatusForPath(path string) string {
	normalized := NormalizeRepoPath("", path)
	for _, file := range c.Files {
		if strings.EqualFold(filepath.ToSlash(file.Path), normalized) {
			return file.Status
		}
	}
	return ""
}

func (c Coverage) MarshalPretty() ([]byte, error) {
	return json.MarshalIndent(c, "", "  ")
}
