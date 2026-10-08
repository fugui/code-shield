package defectlifecycle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"code-shield/models"
	"code-shield/services/coverage"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func scopeOutcome(file coverage.File) string {
	switch file.Status {
	case coverage.StatusPlanned:
		return ScopePlanned
	case coverage.StatusSuccess:
		return ScopeScanned
	case coverage.StatusFailed:
		return ScopeFailed
	case coverage.StatusExcluded:
		return ScopeExcluded
	case coverage.StatusSkipped:
		return ScopeUnchangedSkipped
	case coverage.StatusUnchangedSkipped:
		return ScopeUnchangedSkipped
	default:
		return ScopeUnknown
	}
}

func scopeHash(input ScanInput, paths []models.ScanScopeEntry) string {
	ordered := make([]string, 0, len(paths))
	for _, item := range paths {
		ordered = append(ordered, item.NormPath+"\x00"+item.BlobHash)
	}
	sort.Strings(ordered)
	exclusions := append([]string(nil), input.Exclusions...)
	sort.Strings(exclusions)
	raw := strings.Join([]string{
		input.AlgorithmVersion,
		models.ResolveGovernanceMode(input.TaskType.GovernanceMode),
		strings.Join(ordered, "\x1e"),
		strings.Join(exclusions, "\x1e"),
	}, "\x1d")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func coverageState(summary coverage.Summary, governanceMode string) string {
	switch {
	case summary.CoverageNotApplicable:
		return CoverageNotApplicable
	case summary.CoverageComplete:
		if models.ResolveGovernanceMode(governanceMode) == models.GovernanceModeChangeFocus {
			return CoverageChangeFocus
		}
		return CoverageComplete
	default:
		return CoveragePartial
	}
}

func buildScopeEntries(input ScanInput) []models.ScanScopeEntry {
	coverageFact := input.Coverage
	if coverageFact == nil {
		coverageFact = &coverage.Coverage{ManifestMissing: true}
	}

	entries := make([]models.ScanScopeEntry, 0, len(coverageFact.Files))
	entriesByPath := make(map[string]int, len(coverageFact.Files))
	for _, file := range coverageFact.Files {
		normPath := NormalizePath(input.RepoRoot, file.Path)
		if normPath == "" {
			continue
		}
		if index, exists := entriesByPath[normPath]; exists {
			if scopeOutcome(file) == ScopeFailed {
				entries[index].Outcome = ScopeFailed
				entries[index].ChunkUID = file.ChunkUID
				entries[index].ErrorClass = file.ErrorClass
				entries[index].FailReason = file.Error
				if entries[index].FailReason == "" {
					entries[index].FailReason = file.Reason
				}
			}
			if entries[index].BlobHash == "" {
				entries[index].BlobHash = strings.ToLower(file.Hash)
			}
			continue
		}
		hunkRanges, _ := json.Marshal(file.HunkRanges)
		entry := models.ScanScopeEntry{
			ReportID:    input.Report.ID,
			RepoID:      input.Repo.ID,
			TaskTypeID:  input.TaskType.ID,
			NormPath:    normPath,
			BlobHash:    strings.ToLower(file.Hash),
			Outcome:     scopeOutcome(file),
			ChunkName:   file.ChunkUID,
			ChunkUID:    file.ChunkUID,
			FailReason:  file.Error,
			DiffTouched: file.DiffTouched,
			HunkRanges:  datatypes.JSON(hunkRanges),
		}
		if entry.Outcome == ScopeFailed && entry.ErrorClass == "" {
			entry.ErrorClass = "unknown"
		}
		if entry.FailReason == "" {
			entry.FailReason = file.Reason
		}
		entriesByPath[normPath] = len(entries)
		entries = append(entries, entry)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].NormPath < entries[j].NormPath
	})
	return entries
}

func mergeChunkExecutionScopeEntries(input ScanInput, entries []models.ScanScopeEntry) []models.ScanScopeEntry {
	if input.DB == nil || input.Report.ID == 0 {
		return entries
	}

	executions := []models.TaskChunkExecution{}
	if err := input.DB.Where("report_id = ?", input.Report.ID).Order("attempt").Find(&executions).Error; err != nil {
		return entries
	}

	byPath := make(map[string]int, len(entries))
	for index := range entries {
		byPath[entries[index].NormPath] = index
	}
	latestFinal := make(map[string]models.TaskChunkExecution, len(executions))
	for _, execution := range executions {
		if !execution.IsFinal {
			continue
		}
		normPath := NormalizePath(input.RepoRoot, execution.FilePath)
		if normPath == "" {
			continue
		}
		if current, exists := latestFinal[normPath]; exists && current.FinishedAt != nil &&
			execution.FinishedAt != nil && current.FinishedAt.After(*execution.FinishedAt) {
			continue
		}
		latestFinal[normPath] = execution
	}

	for normPath, execution := range latestFinal {
		entry := models.ScanScopeEntry{
			ReportID:      input.Report.ID,
			RepoID:        input.Repo.ID,
			TaskTypeID:    input.TaskType.ID,
			NormPath:      normPath,
			Outcome:       ScopeFailed,
			ChunkName:     execution.ChunkName,
			ChunkUID:      execution.ChunkUID,
			PrimaryUnitID: execution.PrimaryUnitID,
			Stage:         execution.Stage,
			ErrorClass:    execution.ErrorClass,
			FailReason:    execution.ErrorMessage,
		}
		if index, exists := byPath[normPath]; exists {
			entries[index].ChunkUID = entry.ChunkUID
			entries[index].PrimaryUnitID = entry.PrimaryUnitID
			entries[index].Stage = entry.Stage
			entries[index].ErrorClass = entry.ErrorClass
			entries[index].FailReason = entry.FailReason
			if execution.Status == models.StatusFailed {
				entries[index].Outcome = ScopeFailed
			}
			continue
		}
		if execution.Status != models.StatusFailed {
			continue
		}
		byPath[normPath] = len(entries)
		entries = append(entries, entry)
	}

	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].NormPath < entries[j].NormPath
	})
	return entries
}

func persistScope(tx *gorm.DB, entries []models.ScanScopeEntry) error {
	if len(entries) == 0 {
		return nil
	}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "report_id"}, {Name: "norm_path"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"repo_id", "task_type_id", "blob_hash", "outcome", "chunk_name",
			"chunk_uid", "primary_unit_id", "stage", "error_class", "fail_reason",
			"diff_touched", "hunk_ranges", "updated_at",
		}),
	}).CreateInBatches(&entries, 200).Error
}
