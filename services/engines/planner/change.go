package planner

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"code-shield/services/coverage"
	"code-shield/services/engines/profile"
)

const (
	ChangeKindAdded    = "added"
	ChangeKindModified = "modified"
	ChangeKindDeleted  = "deleted"
	ChangeKindRenamed  = "renamed"

	emptyTreeObjectID    = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
	evidenceContextLines = 5
)

var (
	ErrInvalidBase         = errors.New("invalid change base")
	ErrInsufficientHistory = errors.New("insufficient git history for change base")
)

var hunkHeaderPattern = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

type ChangeBaseline struct {
	Version          int       `json:"version"`
	Strategy         string    `json:"strategy"`
	SinceDays        int       `json:"since_days"`
	CutoffAt         time.Time `json:"cutoff_at"`
	BaseCommit       string    `json:"base_commit"`
	BaseCommitTime   time.Time `json:"base_commit_time,omitempty"`
	HeadCommit       string    `json:"head_commit"`
	DiffManifestHash string    `json:"diff_manifest_hash,omitempty"`
}

type ChangePlan struct {
	Version      int            `json:"version"`
	Profile      string         `json:"profile"`
	ProfileHash  string         `json:"profile_hash"`
	PrimaryUnit  string         `json:"primary_unit"`
	Baseline     ChangeBaseline `json:"baseline"`
	ManifestHash string         `json:"manifest_hash"`
	Files        []ChangeFile   `json:"files"`
}

type ChangeOverview struct {
	AddedFiles    int                  `json:"added_files"`
	ModifiedFiles int                  `json:"modified_files"`
	DeletedFiles  int                  `json:"deleted_files"`
	RenamedFiles  int                  `json:"renamed_files"`
	BinaryFiles   int                  `json:"binary_files"`
	ChangedHunks  int                  `json:"changed_hunks"`
	Files         []ChangeOverviewFile `json:"files"`
}

type ChangeOverviewFile struct {
	Path       string   `json:"path"`
	OldPath    string   `json:"old_path,omitempty"`
	NewPath    string   `json:"new_path,omitempty"`
	ChangeKind string   `json:"change_kind"`
	Language   string   `json:"language"`
	Binary     bool     `json:"binary,omitempty"`
	HunkRanges []string `json:"hunk_ranges,omitempty"`
}

type ChangeFile struct {
	Path                 string          `json:"path"`
	OldPath              string          `json:"old_path,omitempty"`
	NewPath              string          `json:"new_path,omitempty"`
	Language             string          `json:"language"`
	ChangeKind           string          `json:"change_kind"`
	Binary               bool            `json:"binary,omitempty"`
	AffectsBuildOrConfig bool            `json:"affects_build_or_config,omitempty"`
	Hunks                []ChangeHunk    `json:"hunks"`
	Evidence             *ChangeEvidence `json:"evidence,omitempty"`
}

type ChangeHunk struct {
	ID          string          `json:"id"`
	OldStart    int             `json:"old_start"`
	OldLines    int             `json:"old_lines"`
	NewStart    int             `json:"new_start"`
	NewLines    int             `json:"new_lines"`
	ContentHash string          `json:"content_hash"`
	Lines       []string        `json:"lines,omitempty"`
	Evidence    *ChangeEvidence `json:"evidence,omitempty"`
}

type ChangeEvidenceAnchor struct {
	Path       string `json:"path"`
	OldPath    string `json:"old_path,omitempty"`
	NewPath    string `json:"new_path,omitempty"`
	HunkID     string `json:"hunk_id,omitempty"`
	BaseCommit string `json:"base_commit"`
	HeadCommit string `json:"head_commit"`
	OldStart   int    `json:"old_start,omitempty"`
	OldLines   int    `json:"old_lines,omitempty"`
	NewStart   int    `json:"new_start,omitempty"`
	NewLines   int    `json:"new_lines,omitempty"`
}

type ChangeRename struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type ChangeEvidence struct {
	Anchor               ChangeEvidenceAnchor `json:"anchor"`
	BaseFileSnippet      []string             `json:"base_file_snippet,omitempty"`
	HeadFileSnippet      []string             `json:"head_file_snippet,omitempty"`
	ChangedHunk          []string             `json:"changed_hunk,omitempty"`
	EnclosingScope       string               `json:"enclosing_scope,omitempty"`
	RenameMap            []ChangeRename       `json:"rename_map,omitempty"`
	DeletedFileContext   []string             `json:"deleted_file_context,omitempty"`
	BuildOrConfigContext string               `json:"build_or_config_context,omitempty"`
	TestRelationship     string               `json:"test_relationship,omitempty"`
}

type ChangeBundle struct {
	Name         string              `json:"name"`
	ManifestHash string              `json:"manifest_hash"`
	Files        []ChangeFile        `json:"files"`
	PrimaryUnits []coverage.PlanUnit `json:"primary_units"`
}

func ResolveChangeBaseline(
	ctx context.Context,
	repoPath string,
	basePolicy profile.BasePolicy,
	scanStartedAt time.Time,
) (ChangeBaseline, error) {
	baseline := ChangeBaseline{Version: 1}
	if basePolicy.Strategy != "since_days" || basePolicy.SinceDays <= 0 {
		return baseline, fmt.Errorf("%w: unsupported base policy", ErrInvalidBase)
	}
	if scanStartedAt.IsZero() {
		return baseline, fmt.Errorf("%w: scan started time is empty", ErrInvalidBase)
	}
	if _, err := os.Stat(repoPath); err != nil {
		return baseline, fmt.Errorf("%w: repository path is unavailable", ErrInvalidBase)
	}

	headCommit, err := gitOutput(ctx, repoPath, "rev-parse", "HEAD")
	if err != nil {
		return baseline, fmt.Errorf("%w: resolve HEAD: %w", ErrInvalidBase, err)
	}
	baseline.Strategy = basePolicy.Strategy
	baseline.SinceDays = basePolicy.SinceDays
	baseline.CutoffAt = scanStartedAt.UTC().AddDate(0, 0, -basePolicy.SinceDays)
	baseline.HeadCommit = headCommit

	baseCommit, baseCommitTime, found, resolveErr := resolveCutoffCommit(ctx, repoPath, baseline.CutoffAt)
	if resolveErr != nil {
		return baseline, resolveErr
	}
	if !found {
		shallow, shallowErr := isShallowRepository(ctx, repoPath)
		if shallowErr != nil {
			return baseline, fmt.Errorf("%w: inspect history depth: %w", ErrInsufficientHistory, shallowErr)
		}
		if shallow {
			return baseline, fmt.Errorf("%w: shallow clone cannot confirm cutoff boundary", ErrInsufficientHistory)
		}
		baseline.BaseCommit = emptyTreeObjectID
		return baseline, nil
	}

	baseline.BaseCommit = baseCommit
	baseline.BaseCommitTime = baseCommitTime
	return baseline, nil
}

func ResolveRepositoryHead(ctx context.Context, repoPath string) (string, error) {
	return gitOutput(ctx, repoPath, "rev-parse", "HEAD")
}

func resolveCutoffCommit(
	ctx context.Context,
	repoPath string,
	cutoff time.Time,
) (string, time.Time, bool, error) {
	const batchSize = 256
	for offset := 0; ; offset += batchSize {
		output, err := gitOutput(ctx, repoPath,
			"rev-list", "--timestamp",
			"--skip="+strconv.Itoa(offset),
			"--max-count="+strconv.Itoa(batchSize),
			"HEAD",
		)
		if err != nil {
			return "", time.Time{}, false, fmt.Errorf("%w: resolve cutoff commit: %w", ErrInvalidBase, err)
		}
		commitID, commitTime, found := parseCommitTimeList(output, cutoff)
		if found {
			return commitID, commitTime, true, nil
		}
		if len(strings.Split(strings.TrimSpace(output), "\n")) < batchSize {
			return "", time.Time{}, false, nil
		}
	}
}

func BuildChangePlan(
	ctx context.Context,
	repoPath string,
	scanProfile profile.ScanProfile,
	profileHash string,
	baseline ChangeBaseline,
) (*ChangePlan, error) {
	if baseline.BaseCommit == "" || baseline.HeadCommit == "" {
		return nil, fmt.Errorf("%w: baseline is incomplete", ErrInvalidBase)
	}
	diffRange := baseline.BaseCommit + ".." + baseline.HeadCommit
	rawOut, err := gitOutput(
		ctx, repoPath, "diff", "--find-renames", "--no-color", "--raw", "-z", diffRange,
	)
	if err != nil {
		return nil, fmt.Errorf("%w: read raw diff: %w", ErrInvalidBase, err)
	}
	entries, err := parseRawDiffEntries([]byte(rawOut))
	if err != nil {
		return nil, err
	}

	unifiedOut, err := gitOutput(
		ctx, repoPath, "diff", "--find-renames", "--no-color", "-U0", diffRange,
	)
	if err != nil {
		return nil, fmt.Errorf("read unified diff: %w", err)
	}
	hunks, binaryFiles, err := parseUnifiedDiff(unifiedOut)
	if err != nil {
		return nil, err
	}

	plan := &ChangePlan{
		Version:     1,
		Profile:     scanProfile.Name,
		ProfileHash: profileHash,
		PrimaryUnit: "change_hunk",
		Baseline:    baseline,
	}
	for _, entry := range entries {
		primaryPath := entry.NewPath
		if primaryPath == "" {
			primaryPath = entry.OldPath
		}
		language := changeLanguage(primaryPath)
		if language == "" || pathExcluded(primaryPath, scanProfile.ExcludePaths) {
			continue
		}
		file := ChangeFile{
			Path:                 primaryPath,
			OldPath:              entry.OldPath,
			NewPath:              entry.NewPath,
			Language:             language,
			ChangeKind:           entry.ChangeKind,
			Binary:               binaryFiles[primaryPath],
			AffectsBuildOrConfig: affectsBuildOrConfig(primaryPath),
		}
		file.Hunks = append(file.Hunks, hunks[primaryPath]...)
		for index := range file.Hunks {
			file.Hunks[index].ID = fmt.Sprintf("%s#h%d", file.Path, index+1)
		}
		plan.Files = append(plan.Files, file)
	}
	sort.SliceStable(plan.Files, func(left, right int) bool {
		return plan.Files[left].Path < plan.Files[right].Path
	})
	if err := attachChangeEvidence(ctx, repoPath, plan); err != nil {
		return nil, err
	}

	plan.ManifestHash = manifestHash("change-plan|"+profileHash, plan)
	return plan, nil
}

func (plan *ChangePlan) PrimaryCount() int {
	count := 0
	for _, file := range plan.Files {
		if len(file.Hunks) == 0 {
			count++
			continue
		}
		count += len(file.Hunks)
	}
	return count
}

func (plan *ChangePlan) PrimaryManifestHash() string {
	return plan.ManifestHash
}

func (plan *ChangePlan) PrimaryUnits() []coverage.PlanUnit {
	units := make([]coverage.PlanUnit, 0, plan.PrimaryCount())
	for _, file := range plan.Files {
		if len(file.Hunks) == 0 {
			evidence, _ := json.Marshal(file.Evidence)
			units = append(units, coverage.PlanUnit{
				ID:          file.Path,
				Kind:        coverage.PlanUnitChangeHunk,
				Path:        file.Path,
				DisplayName: file.Path,
				Evidence:    evidence,
			})
			continue
		}
		for _, hunk := range file.Hunks {
			start, end := hunk.NewStart, hunk.NewStart+hunk.NewLines-1
			if hunk.NewLines == 0 {
				start, end = hunk.OldStart, hunk.OldStart+hunk.OldLines-1
			}
			displayName := fmt.Sprintf("%s:%d-%d", file.Path, start, end)
			if start == end {
				displayName = fmt.Sprintf("%s:%d", file.Path, start)
			}
			evidence, _ := json.Marshal(hunk.Evidence)
			units = append(units, coverage.PlanUnit{
				ID:          hunk.ID,
				Kind:        coverage.PlanUnitChangeHunk,
				Path:        file.Path,
				DisplayName: displayName,
				StartLine:   start,
				EndLine:     end,
				Evidence:    evidence,
			})
		}
	}
	return units
}

func (plan *ChangePlan) Overview() *ChangeOverview {
	overview := &ChangeOverview{Files: make([]ChangeOverviewFile, 0, len(plan.Files))}
	for _, file := range plan.Files {
		switch file.ChangeKind {
		case ChangeKindAdded:
			overview.AddedFiles++
		case ChangeKindModified:
			overview.ModifiedFiles++
		case ChangeKindDeleted:
			overview.DeletedFiles++
		case ChangeKindRenamed:
			overview.RenamedFiles++
		}
		if file.Binary {
			overview.BinaryFiles++
		}
		overview.ChangedHunks += len(file.Hunks)
		overviewFile := ChangeOverviewFile{
			Path:       file.Path,
			OldPath:    file.OldPath,
			NewPath:    file.NewPath,
			ChangeKind: file.ChangeKind,
			Language:   file.Language,
			Binary:     file.Binary,
			HunkRanges: make([]string, 0, len(file.Hunks)),
		}
		for _, hunk := range file.Hunks {
			start, end := hunk.NewStart, hunk.NewStart+hunk.NewLines-1
			if hunk.NewLines == 0 {
				start, end = hunk.OldStart, hunk.OldStart+hunk.OldLines-1
			}
			if hunk.NewLines == 0 || hunk.OldLines == 0 {
				overviewFile.HunkRanges = append(overviewFile.HunkRanges, strconv.Itoa(start))
				continue
			}
			overviewFile.HunkRanges = append(overviewFile.HunkRanges, fmt.Sprintf("%d-%d", start, end))
		}
		overview.Files = append(overview.Files, overviewFile)
	}
	return overview
}

type rawDiffEntry struct {
	ChangeKind string
	OldPath    string
	NewPath    string
}

func attachChangeEvidence(ctx context.Context, repoPath string, plan *ChangePlan) error {
	for fileIndex := range plan.Files {
		file := &plan.Files[fileIndex]
		basePath := file.Path
		if file.OldPath != "" {
			basePath = file.OldPath
		}
		headPath := file.Path
		if file.NewPath != "" {
			headPath = file.NewPath
		}

		var baseLines, headLines []string
		var baseErr, headErr error
		if !file.Binary {
			baseLines, baseErr = commitFileLines(ctx, repoPath, plan.Baseline.BaseCommit, basePath)
			headLines, headErr = commitFileLines(ctx, repoPath, plan.Baseline.HeadCommit, headPath)
			if baseErr != nil && !errors.Is(baseErr, os.ErrNotExist) {
				return fmt.Errorf("read base evidence for %s: %w", basePath, baseErr)
			}
			if headErr != nil && !errors.Is(headErr, os.ErrNotExist) {
				return fmt.Errorf("read head evidence for %s: %w", headPath, headErr)
			}
		}

		for hunkIndex := range file.Hunks {
			hunk := &file.Hunks[hunkIndex]
			baseSnippet := snippetLines(baseLines, hunk.OldStart, hunk.OldLines, evidenceContextLines)
			headSnippet := snippetLines(headLines, hunk.NewStart, hunk.NewLines, evidenceContextLines)
			scopeLines := headSnippet
			if hunk.NewLines == 0 {
				scopeLines = baseSnippet
			}
			hunk.Evidence = &ChangeEvidence{
				Anchor: ChangeEvidenceAnchor{
					Path:       file.Path,
					OldPath:    file.OldPath,
					NewPath:    file.NewPath,
					HunkID:     hunk.ID,
					BaseCommit: plan.Baseline.BaseCommit,
					HeadCommit: plan.Baseline.HeadCommit,
					OldStart:   hunk.OldStart,
					OldLines:   hunk.OldLines,
					NewStart:   hunk.NewStart,
					NewLines:   hunk.NewLines,
				},
				BaseFileSnippet:      baseSnippet,
				HeadFileSnippet:      headSnippet,
				ChangedHunk:          hunk.Lines,
				EnclosingScope:       enclosingScope(scopeLines),
				RenameMap:            renameMap(file),
				BuildOrConfigContext: buildConfigContext(file),
				TestRelationship:     testRelationship(file.Path),
			}
			if file.ChangeKind == ChangeKindDeleted {
				hunk.Evidence.DeletedFileContext = baseLines
			}
		}

		if len(file.Hunks) == 0 {
			file.Evidence = &ChangeEvidence{
				Anchor: ChangeEvidenceAnchor{
					Path:       file.Path,
					OldPath:    file.OldPath,
					NewPath:    file.NewPath,
					BaseCommit: plan.Baseline.BaseCommit,
					HeadCommit: plan.Baseline.HeadCommit,
				},
				BaseFileSnippet:      limitLines(baseLines, 40),
				HeadFileSnippet:      limitLines(headLines, 40),
				RenameMap:            renameMap(file),
				BuildOrConfigContext: buildConfigContext(file),
				TestRelationship:     testRelationship(file.Path),
			}
			if file.ChangeKind == ChangeKindDeleted {
				file.Evidence.DeletedFileContext = baseLines
			}
		}
	}
	return nil
}

func (plan *ChangePlan) BuildBundles(maxUnits int) ([]ChangeBundle, error) {
	if maxUnits <= 0 {
		maxUnits = 8
	}
	bundles := make([]ChangeBundle, 0)
	for fileIndex := range plan.Files {
		source := plan.Files[fileIndex]
		if len(source.Hunks) <= maxUnits {
			bundles = append(bundles, newChangeBundle(plan, source, fmt.Sprintf("%s", source.Path)))
			continue
		}
		for start := 0; start < len(source.Hunks); start += maxUnits {
			end := start + maxUnits
			if end > len(source.Hunks) {
				end = len(source.Hunks)
			}
			part := source
			part.Hunks = append([]ChangeHunk(nil), source.Hunks[start:end]...)
			bundles = append(bundles, newChangeBundle(
				plan, part, fmt.Sprintf("%s#part-%03d", source.Path, start/maxUnits+1),
			))
		}
	}
	return bundles, nil
}

func newChangeBundle(plan *ChangePlan, file ChangeFile, name string) ChangeBundle {
	bundle := ChangeBundle{Name: name, Files: []ChangeFile{file}}
	evidenceByID := make(map[string]json.RawMessage)
	for _, unit := range plan.PrimaryUnits() {
		evidenceByID[unit.ID] = unit.Evidence
	}
	if len(file.Hunks) == 0 {
		bundle.PrimaryUnits = []coverage.PlanUnit{{
			ID:   file.Path,
			Kind: coverage.PlanUnitChangeHunk,
			Path: file.Path,
		}}
	} else {
		bundle.PrimaryUnits = make([]coverage.PlanUnit, 0, len(file.Hunks))
		for _, hunk := range file.Hunks {
			start, end := hunk.NewStart, hunk.NewStart+hunk.NewLines-1
			if hunk.NewLines == 0 {
				start, end = hunk.OldStart, hunk.OldStart+hunk.OldLines-1
			}
			bundle.PrimaryUnits = append(bundle.PrimaryUnits, coverage.PlanUnit{
				ID:        hunk.ID,
				Kind:      coverage.PlanUnitChangeHunk,
				Path:      file.Path,
				StartLine: start,
				EndLine:   end,
				Evidence:  evidenceByID[hunk.ID],
			})
		}
	}
	bundle.ManifestHash = manifestHash("change-bundle|"+plan.ManifestHash, bundle)
	return bundle
}

func parseRawDiffEntries(raw []byte) ([]rawDiffEntry, error) {
	fields := bytes.Split(raw, []byte{0})
	entries := make([]rawDiffEntry, 0)
	for index := 0; index < len(fields); index++ {
		field := strings.TrimSpace(string(fields[index]))
		if field == "" {
			continue
		}
		if !strings.HasPrefix(field, ":") {
			return nil, fmt.Errorf("invalid raw diff record at field %d", index)
		}
		parts := strings.Fields(field)
		if len(parts) != 5 {
			return nil, fmt.Errorf("invalid raw diff metadata %q", field)
		}
		status := parts[4]
		index++
		if index >= len(fields) {
			return nil, fmt.Errorf("raw diff metadata %q has no path", field)
		}
		oldPath := string(fields[index])
		newPath := oldPath
		if status[0] == 'R' || status[0] == 'C' {
			index++
			if index >= len(fields) {
				return nil, fmt.Errorf("raw diff metadata %q has no destination path", field)
			}
			newPath = string(fields[index])
		}
		kind := ChangeKindModified
		switch status[0] {
		case 'A':
			kind = ChangeKindAdded
		case 'D':
			kind = ChangeKindDeleted
		case 'R', 'C':
			kind = ChangeKindRenamed
		}
		entries = append(entries, rawDiffEntry{ChangeKind: kind, OldPath: oldPath, NewPath: newPath})
	}
	return entries, nil
}

func parseUnifiedDiff(raw string) (map[string][]ChangeHunk, map[string]bool, error) {
	hunks := make(map[string][]ChangeHunk)
	binaryFiles := make(map[string]bool)
	var oldPath, newPath string
	var addedContent, removedContent strings.Builder
	currentHunkPath := ""
	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "diff --git "):
			oldPath, newPath = "", ""
			currentHunkPath = ""
			addedContent.Reset()
			removedContent.Reset()
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				oldPath = strings.TrimPrefix(parts[len(parts)-2], "a/")
				newPath = strings.TrimPrefix(parts[len(parts)-1], "b/")
			}
		case strings.HasPrefix(line, "Binary files "):
			if newPath != "" && newPath != "/dev/null" {
				binaryFiles[newPath] = true
			} else if oldPath != "" {
				binaryFiles[oldPath] = true
			}
		case strings.HasPrefix(line, "--- "):
			oldPath = diffHeaderPath(line)
		case strings.HasPrefix(line, "+++ "):
			newPath = diffHeaderPath(line)
		case hunkHeaderPattern.MatchString(line):
			matches := hunkHeaderPattern.FindStringSubmatch(line)
			oldStart, _ := strconv.Atoi(matches[1])
			oldLines, _ := strconv.Atoi(matches[2])
			newStart, _ := strconv.Atoi(matches[3])
			newLines, _ := strconv.Atoi(matches[4])
			primaryPath := newPath
			if primaryPath == "" || primaryPath == "/dev/null" {
				primaryPath = oldPath
			}
			contentHash := hunkContentHash(addedContent.String(), removedContent.String())
			hunk := ChangeHunk{
				OldStart:    oldStart,
				OldLines:    oldLines,
				NewStart:    newStart,
				NewLines:    newLines,
				ContentHash: contentHash,
				Lines:       []string{line},
			}
			hunks[primaryPath] = append(hunks[primaryPath], hunk)
			currentHunkPath = primaryPath
			addedContent.Reset()
			removedContent.Reset()
		case strings.HasPrefix(line, "+"):
			addedContent.WriteString(line[1:])
			addedContent.WriteByte(0)
			appendHunkLine(hunks, currentHunkPath, line)
		case strings.HasPrefix(line, "-"):
			removedContent.WriteString(line[1:])
			removedContent.WriteByte(0)
			appendHunkLine(hunks, currentHunkPath, line)
		default:
			if strings.HasPrefix(line, " ") {
				appendHunkLine(hunks, currentHunkPath, line)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("parse unified diff: %w", err)
	}
	return hunks, binaryFiles, nil
}

func appendHunkLine(hunks map[string][]ChangeHunk, path, line string) {
	if path == "" || len(hunks[path]) == 0 {
		return
	}
	index := len(hunks[path]) - 1
	hunks[path][index].Lines = append(hunks[path][index].Lines, line)
}

func commitFileLines(ctx context.Context, repoPath, commit, path string) ([]string, error) {
	if commit == "" || path == "" || path == "/dev/null" {
		return nil, os.ErrNotExist
	}
	output, err := gitOutput(ctx, repoPath, "show", commit+":"+path)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") || strings.Contains(err.Error(), "path does not exist") ||
			strings.Contains(err.Error(), "exists on disk, but not in") {
			return nil, os.ErrNotExist
		}
		return nil, err
	}
	return strings.Split(output, "\n"), nil
}

func snippetLines(lines []string, start, count, context int) []string {
	if start <= 0 {
		return nil
	}
	first := start - context
	if first < 1 {
		first = 1
	}
	last := start + count + context - 1
	if count <= 0 {
		last = start + context - 1
	}
	if last > len(lines) {
		last = len(lines)
	}
	if first > last {
		return nil
	}
	return append([]string(nil), lines[first-1:last]...)
}

func limitLines(lines []string, limit int) []string {
	if len(lines) <= limit {
		return append([]string(nil), lines...)
	}
	return append([]string(nil), lines[:limit]...)
}

func renameMap(file *ChangeFile) []ChangeRename {
	if file.ChangeKind != ChangeKindRenamed || file.OldPath == "" || file.NewPath == "" || file.OldPath == file.NewPath {
		return nil
	}
	return []ChangeRename{{From: file.OldPath, To: file.NewPath}}
}

func buildConfigContext(file *ChangeFile) string {
	if file.AffectsBuildOrConfig {
		return "build_or_config_change"
	}
	return ""
}

func testRelationship(path string) string {
	normalized := strings.ToLower(filepath.ToSlash(path))
	name := strings.ToLower(filepath.Base(normalized))
	if strings.Contains(normalized, "/tests/") || strings.HasPrefix(name, "test_") ||
		strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, "_test.cpp") ||
		strings.HasSuffix(name, "_test.py") || strings.HasSuffix(name, "test.java") {
		return "test_file"
	}
	return "production_file"
}

func diffHeaderPath(line string) string {
	path := strings.TrimSpace(strings.TrimPrefix(line, "--- "))
	path = strings.TrimSpace(strings.TrimPrefix(path, "+++ "))
	path = strings.TrimPrefix(path, "a/")
	path = strings.TrimPrefix(path, "b/")
	return path
}

func parseCommitTimeList(raw string, cutoff time.Time) (string, time.Time, bool) {
	var candidateID string
	var candidateTime time.Time
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		timestamp, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			continue
		}
		commitTime := time.Unix(timestamp, 0).UTC()
		if !commitTime.Before(cutoff) {
			continue
		}
		candidateID = fields[1]
		candidateTime = commitTime
		break
	}
	return candidateID, candidateTime, candidateID != ""
}

func hunkContentHash(added, removed string) string {
	sum := sha256.Sum256([]byte(removed + "\x00" + added))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func changeLanguage(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".c", ".cc", ".cpp", ".cxx", ".h", ".hh", ".hpp", ".hxx":
		return profile.LanguageCPP
	case ".py":
		return profile.LanguagePython
	case ".java":
		return profile.LanguageJava
	default:
		return ""
	}
}

func pathExcluded(path string, patterns []string) bool {
	normalized := filepath.ToSlash(path)
	for _, pattern := range patterns {
		prefix := strings.Trim(filepath.ToSlash(pattern), "/")
		if prefix != "" && strings.HasPrefix(normalized, prefix+"/") {
			return true
		}
		if prefix != "" && normalized == prefix {
			return true
		}
	}
	return false
}

func affectsBuildOrConfig(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	switch name {
	case "makefile", "cmakelists.txt", "meson.build", "setup.py", "pyproject.toml", "pom.xml", "build.gradle":
		return true
	default:
		return strings.HasSuffix(name, ".mk")
	}
}

func gitOutput(ctx context.Context, repoPath string, arguments ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", repoPath}, arguments...)...)
	command.Env = append(os.Environ(), "TZ=UTC", "LC_ALL=C")
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("%s: %w", strings.TrimSpace(stderr.String()), err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func isShallowRepository(ctx context.Context, repoPath string) (bool, error) {
	gitDir, err := gitOutput(ctx, repoPath, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return false, err
	}
	if _, err := os.Stat(filepath.Join(gitDir, "shallow")); err == nil {
		return true, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	return false, nil
}

func changePlanJSON(plan *ChangePlan) ([]byte, error) {
	return json.Marshal(plan)
}
