package chunked

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"code-shield/models"
	"code-shield/services/coverage"
	"code-shield/services/engines"
)

const chunkCheckpointSchemaVersion = 2

// ChunkResumeMetadata contains the lightweight identity of the analysis
// environment. It intentionally avoids model fingerprints in Phase 1, but file
// snapshot hashes already prevent reusing findings after source content changes.
type ChunkResumeMetadata struct {
	CommitHash       string
	EngineConfigHash string
	PromptHash       string
}

type ChunkCheckpoint struct {
	SchemaVersion    int                          `json:"schema_version"`
	ReportID         uint                         `json:"report_id"`
	EngineMode       string                       `json:"engine_mode"`
	ChunkName        string                       `json:"chunk_name"`
	Files            []string                     `json:"files"`
	CommitHash       string                       `json:"commit_hash,omitempty"`
	SnapshotHash     string                       `json:"snapshot_hash"`
	EngineConfigHash string                       `json:"engine_config_hash"`
	PromptHash       string                       `json:"prompt_hash,omitempty"`
	Result           engines.ChunkExecutionResult `json:"result"`
	CompletedAt      time.Time                    `json:"completed_at"`
}

type ChunkCheckpointContext struct {
	ReportID     uint
	EngineMode   string
	CodesPath    string
	EngineConfig json.RawMessage
	PromptPath   string
}

func ChunkOutputPath(chunkDir string, reportID uint, chunkName string) string {
	return filepath.Join(chunkDir, fmt.Sprintf("chunk-%d-%s.json", reportID, SafeChunkName(chunkName)))
}

func ChunkCheckpointPath(chunkDir string, reportID uint, chunkName string) string {
	return filepath.Join(chunkDir, fmt.Sprintf("checkpoint-%d-%s.json", reportID, SafeChunkName(chunkName)))
}

// BuildChunkResumeMetadata hashes the source tree state indirectly through each
// chunk snapshot, while also pinning task-type config and prompt content.
func BuildChunkResumeMetadata(codesPath string, engineConfig json.RawMessage, promptPath string) ChunkResumeMetadata {
	metadata := ChunkResumeMetadata{
		EngineConfigHash: hashBytes(engineConfig),
	}

	if actualCommit, _, _, err := coverage.VerifyWorkspace(codesPath, ""); err == nil {
		metadata.CommitHash = actualCommit
	} else {
		log.Printf("[ChunkCheckpoint] Warning: resolve commit hash failed repo=%q err=%v\n", codesPath, err)
	}
	if promptPath != "" {
		metadata.PromptHash = hashFile(promptPath)
	}
	return metadata
}

func ChunkSnapshotHash(codesPath string, files []string) (string, error) {
	hashes := make(map[string]string, len(files))
	for _, file := range files {
		hash, err := coverage.FileHash(codesPath, file)
		if err != nil {
			return "", fmt.Errorf("hash chunk file %q: %w", file, err)
		}
		hashes[file] = hash
	}

	sortedFiles := append([]string(nil), files...)
	sort.Strings(sortedFiles)
	digest := sha256.New()
	for _, file := range sortedFiles {
		_, _ = fmt.Fprintf(digest, "%s\x1f%s\n", file, hashes[file])
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func WriteChunkCheckpoint(
	ctx ChunkCheckpointContext,
	req engines.ChunkExecutionRequest,
	result engines.ChunkExecutionResult,
) error {
	metadata := BuildChunkResumeMetadata(ctx.CodesPath, ctx.EngineConfig, ctx.PromptPath)
	snapshotHash, err := ChunkSnapshotHash(ctx.CodesPath, req.Files)
	if err != nil {
		return err
	}

	checkpoint := ChunkCheckpoint{
		SchemaVersion:    chunkCheckpointSchemaVersion,
		ReportID:         ctx.ReportID,
		EngineMode:       ctx.EngineMode,
		ChunkName:        req.Name,
		Files:            append([]string(nil), req.Files...),
		CommitHash:       metadata.CommitHash,
		SnapshotHash:     snapshotHash,
		EngineConfigHash: metadata.EngineConfigHash,
		PromptHash:       metadata.PromptHash,
		Result: engines.ChunkExecutionResult{
			Findings:   normalizeFindings(result.Findings),
			Attempts:   result.Attempts,
			Retries:    result.Retries,
			OutputPath: result.OutputPath,
		},
		CompletedAt: time.Now(),
	}

	artifactComplete := true
	artifactState := "legacy_success"
	if result.ArtifactComplete != nil {
		artifactComplete = *result.ArtifactComplete
		artifactState = result.ArtifactState
	}
	if artifactState == "" {
		artifactState = "observed"
	}
	checkpoint.Result.ArtifactComplete = &artifactComplete
	checkpoint.Result.ArtifactState = artifactState
	checkpoint.Result.ArtifactQualityDegraded = result.ArtifactQualityDegraded
	checkpoint.Result.UnresolvedIssueCount = result.UnresolvedIssueCount
	checkpoint.Result.NormalizedIssueCount = result.NormalizedIssueCount
	checkpoint.Result.SchemaRepairAttempts = result.SchemaRepairAttempts
	checkpoint.Result.SchemaRepairSuccesses = result.SchemaRepairSuccesses
	checkpoint.Result.CandidateQuarantineCount = result.CandidateQuarantineCount

	data, err := json.MarshalIndent(checkpoint, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal chunk checkpoint: %w", err)
	}
	return writeJSONAtomic(ChunkCheckpointPath(filepath.Dir(req.OutputPath), ctx.ReportID, req.Name), data)
}

func LoadChunkResumeCheckpoints(chunkDir string) map[string]ChunkCheckpoint {
	checkpoints := make(map[string]ChunkCheckpoint)
	if chunkDir == "" {
		return checkpoints
	}

	matches, err := filepath.Glob(filepath.Join(chunkDir, "checkpoint-*.json"))
	if err != nil {
		log.Printf("[ChunkCheckpoint] Failed to list checkpoints in %q: %v\n", chunkDir, err)
		return checkpoints
	}

	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			log.Printf("[ChunkCheckpoint] Checkpoint read failed path=%q err=%v\n", path, err)
			continue
		}
		var checkpoint ChunkCheckpoint
		if err := json.Unmarshal(data, &checkpoint); err != nil {
			log.Printf("[ChunkCheckpoint] Checkpoint decode failed path=%q err=%v\n", path, err)
			continue
		}
		if checkpoint.ChunkName == "" {
			log.Printf("[ChunkCheckpoint] Checkpoint ignored path=%q reason=empty chunk name\n", path)
			continue
		}
		if checkpoint.SchemaVersion != chunkCheckpointSchemaVersion {
			log.Printf("[ChunkCheckpoint] Checkpoint ignored path=%q reason=schema_version=%d\n", path, checkpoint.SchemaVersion)
			continue
		}
		checkpoints[checkpoint.ChunkName] = checkpoint
	}
	return checkpoints
}

func ValidateChunkResumeCheckpoint(
	checkpoint ChunkCheckpoint,
	reportID uint,
	engineMode string,
	chunkName string,
	files []string,
	current ChunkResumeMetadata,
	snapshotHash string,
) bool {
	if checkpoint.SchemaVersion != chunkCheckpointSchemaVersion {
		log.Printf("[ChunkCheckpoint] Checkpoint invalid chunk=%q reason=schema_version=%d\n", chunkName, checkpoint.SchemaVersion)
		return false
	}
	if checkpoint.ReportID != reportID || checkpoint.EngineMode != engineMode || checkpoint.ChunkName != chunkName {
		log.Printf("[ChunkCheckpoint] Checkpoint invalid chunk=%q reason=identity_mismatch\n", chunkName)
		return false
	}
	if !sameChunkFiles(checkpoint.Files, files) {
		log.Printf("[ChunkCheckpoint] Checkpoint invalid chunk=%q reason=files_mismatch\n", chunkName)
		return false
	}
	if checkpoint.SnapshotHash == "" || checkpoint.SnapshotHash != snapshotHash {
		log.Printf("[ChunkCheckpoint] Checkpoint invalid chunk=%q reason=snapshot_mismatch\n", chunkName)
		return false
	}
	if checkpoint.EngineConfigHash != current.EngineConfigHash {
		log.Printf("[ChunkCheckpoint] Checkpoint invalid chunk=%q reason=engine_config_mismatch\n", chunkName)
		return false
	}
	if checkpoint.PromptHash != current.PromptHash {
		log.Printf("[ChunkCheckpoint] Checkpoint invalid chunk=%q reason=prompt_mismatch\n", chunkName)
		return false
	}
	if checkpoint.CommitHash != "" && current.CommitHash != "" && checkpoint.CommitHash != current.CommitHash {
		log.Printf("[ChunkCheckpoint] Checkpoint invalid chunk=%q reason=commit_mismatch\n", chunkName)
		return false
	}
	return true
}

func normalizeFindings(findings []models.AnalysisFinding) []models.AnalysisFinding {
	if findings == nil {
		return []models.AnalysisFinding{}
	}
	return append([]models.AnalysisFinding(nil), findings...)
}

func sameChunkFiles(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	leftSorted := append([]string(nil), left...)
	rightSorted := append([]string(nil), right...)
	sort.Strings(leftSorted)
	sort.Strings(rightSorted)
	for i := range leftSorted {
		if leftSorted[i] != rightSorted[i] {
			return false
		}
	}
	return true
}

func SafeChunkName(name string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		case r == '/' || r == '\\' || r == '.' || r == ' ':
			return '-'
		default:
			return '_'
		}
	}, name)
	if safe == "" {
		return "chunk"
	}
	return safe
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func hashFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return hashBytes(data)
}

func writeJSONAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, fmt.Sprintf(".%s.tmp-*", strings.TrimSuffix(filepath.Base(path), ".json")))
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	_, writeErr := tmp.Write(data)
	syncErr := tmp.Sync()
	closeErr := tmp.Close()
	if writeErr == nil && syncErr == nil && closeErr == nil {
		if renameErr := os.Rename(tmpName, path); renameErr == nil {
			return nil
		} else {
			_ = os.Remove(tmpName)
			return renameErr
		}
	}

	_ = os.Remove(tmpName)
	if writeErr != nil {
		return writeErr
	}
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}
