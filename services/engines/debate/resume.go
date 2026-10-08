package debate

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
	"code-shield/services/engines"
	"code-shield/services/engines/chunked"
	"code-shield/services/engines/chunker"
)

// bundleResumeSchemaVersion 是轻量断点续扫的唯一兼容性安全边界。
// 凡影响 prompt、输出契约、findings/debate log 转换、bundle 拆分或 token 统计的改动必须递增。
const (
	bundleResumeSchemaVersion = 5
	bundleResumeLegacyVersion = 3
)

type BundleResumeMetadata struct {
	PlanManifestHash string `json:"plan_manifest_hash,omitempty"`
	CommitHash       string `json:"commit_hash,omitempty"`
	EngineConfigHash string `json:"engine_config_hash"`
	PromptHash       string `json:"prompt_hash,omitempty"`
	ChangeBaseCommit string `json:"change_base_commit,omitempty"`
	ChangeHeadCommit string `json:"change_head_commit,omitempty"`
	DiffManifestHash string `json:"diff_manifest_hash,omitempty"`
}

func (metadata BundleResumeMetadata) HasChangeBaseline() bool {
	return metadata.ChangeBaseCommit != "" || metadata.ChangeHeadCommit != "" || metadata.DiffManifestHash != ""
}

func (metadata BundleResumeMetadata) ChangeBaselineComplete() bool {
	return metadata.ChangeBaseCommit != "" && metadata.ChangeHeadCommit != "" && metadata.DiffManifestHash != ""
}

func BuildBundleResumeMetadata(codesPath string, engineConfig json.RawMessage, promptContent string, planManifestHash string) BundleResumeMetadata {
	metadata := chunked.BuildChunkResumeMetadata(codesPath, engineConfig, "")
	return BundleResumeMetadata{
		PlanManifestHash: planManifestHash,
		CommitHash:       metadata.CommitHash,
		EngineConfigHash: metadata.EngineConfigHash,
		PromptHash:       HashPromptContent(promptContent),
	}
}

func HashPromptContent(promptContent string) string {
	if promptContent == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(promptContent))
	return hex.EncodeToString(sum[:])
}

// BundleResumeResult 保存一个 bundle 完整成功后的可复用结果。
type BundleResumeResult struct {
	Findings     []models.AnalysisFinding `json:"findings"`
	DebateLogs   []models.TaskDebateLog   `json:"debate_logs"`
	HunterTokens int64                    `json:"hunter_tokens"`
	Tier2Tokens  int64                    `json:"tier2_tokens"`

	Artifact BundleArtifactMetrics `json:"artifact,omitempty"`
}

type BundleArtifactMetrics struct {
	ArtifactComplete         *bool                        `json:"artifact_complete"`
	ArtifactState            string                       `json:"artifact_state,omitempty"`
	ArtifactQualityDegraded  bool                         `json:"artifact_quality_degraded,omitempty"`
	UnresolvedIssueCount     int                          `json:"unresolved_issue_count,omitempty"`
	NormalizedIssueCount     int                          `json:"normalized_issue_count,omitempty"`
	SchemaRepairAttempts     int                          `json:"schema_repair_attempts,omitempty"`
	SchemaRepairSuccesses    int                          `json:"schema_repair_successes,omitempty"`
	CandidateQuarantineCount int                          `json:"candidate_quarantine_count,omitempty"`
	SchemaRepairIssues       []string                     `json:"schema_repair_issues,omitempty"`
	Category                 engines.CategoryStageMetrics `json:"category,omitempty"`
}

func NewBundleArtifactMetrics(detail engines.ChunkDetails) BundleArtifactMetrics {
	complete := true
	state := "legacy_success"
	if detail.ArtifactComplete != nil {
		complete = *detail.ArtifactComplete
		state = detail.ArtifactState
	}
	if state == "" {
		state = "observed"
	}
	return BundleArtifactMetrics{
		ArtifactComplete:         &complete,
		ArtifactState:            state,
		ArtifactQualityDegraded:  detail.ArtifactQualityDegraded,
		UnresolvedIssueCount:     detail.UnresolvedIssueCount,
		NormalizedIssueCount:     detail.NormalizedIssueCount,
		SchemaRepairAttempts:     detail.SchemaRepairAttempts,
		SchemaRepairSuccesses:    detail.SchemaRepairSuccesses,
		CandidateQuarantineCount: detail.CandidateQuarantineCount,
		SchemaRepairIssues:       detail.SchemaRepairIssues,
		Category:                 detail.Category,
	}
}

func (m BundleArtifactMetrics) normalize() BundleArtifactMetrics {
	if m.ArtifactComplete != nil {
		if m.ArtifactState == "" {
			m.ArtifactState = "observed"
		}
		return m
	}

	complete := true
	m.ArtifactComplete = &complete
	if m.ArtifactState == "" {
		m.ArtifactState = "legacy_success"
	}
	return m
}

// BundleResumeCheckpoint 是单个 bundle 的最终结果快照。
type BundleResumeCheckpoint struct {
	ResumeSchemaVersion int                  `json:"resume_schema_version"`
	ReportID            uint                 `json:"report_id"`
	EngineMode          string               `json:"engine_mode"`
	BundleIndex         int                  `json:"bundle_index"`
	BundleName          string               `json:"bundle_name"`
	Files               []string             `json:"files"`
	Metadata            BundleResumeMetadata `json:"metadata"`
	SnapshotHash        string               `json:"snapshot_hash"`
	Result              BundleResumeResult   `json:"result"`
	CompletedAt         time.Time            `json:"completed_at"`
	AppVersion          string               `json:"app_version,omitempty"`
}

// WriteBundleResumeCheckpoint 原子保存一个 bundle 的最终结果。写盘失败不影响本次扫描成功。
func WriteBundleResumeCheckpoint(
	chunkDir string,
	reportID uint,
	engineMode string,
	bundleIndex int,
	bundle chunker.SemanticBundle,
	findings []models.AnalysisFinding,
	debateLogs []models.TaskDebateLog,
	hunterTokens int64,
	tier2Tokens int64,
	metadata BundleResumeMetadata,
	snapshotHash string,
	artifact BundleArtifactMetrics,
) error {
	if chunkDir == "" {
		return nil
	}

	if findings == nil {
		findings = []models.AnalysisFinding{}
	}
	if debateLogs == nil {
		debateLogs = []models.TaskDebateLog{}
	}

	cp := BundleResumeCheckpoint{
		ResumeSchemaVersion: bundleResumeSchemaVersion,
		ReportID:            reportID,
		EngineMode:          engineMode,
		BundleIndex:         bundleIndex,
		BundleName:          bundle.Name,
		Files:               append([]string(nil), bundle.AllFiles...),
		Metadata:            metadata,
		SnapshotHash:        snapshotHash,
		Result: BundleResumeResult{
			Findings:     findings,
			DebateLogs:   debateLogs,
			HunterTokens: hunterTokens,
			Tier2Tokens:  tier2Tokens,
		},
		CompletedAt: time.Now(),
	}
	cp.Result.Artifact = artifact

	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal bundle resume checkpoint: %w", err)
	}

	path := BundleResumeCheckpointPath(chunkDir, bundleIndex, bundle.Name)
	if err := writeJSONAtomic(path, data); err != nil {
		return fmt.Errorf("write bundle resume checkpoint: %w", err)
	}
	return nil
}

// LoadBundleResumeCheckpoints 读取目录内全部可反序列化的 checkpoint。
// 损坏文件不会进入 map；后续匹配时会被当作不存在并重扫。
func LoadBundleResumeCheckpoints(chunkDir string) map[string]BundleResumeCheckpoint {
	checkpoints := make(map[string]BundleResumeCheckpoint)
	if chunkDir == "" {
		return checkpoints
	}

	matches, err := filepath.Glob(filepath.Join(chunkDir, "resume-*.json"))
	if err != nil {
		log.Printf("[BundleResume] Failed to list checkpoints in %s: %v\n", chunkDir, err)
		return checkpoints
	}

	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			log.Printf("[BundleResume] Checkpoint read failed path=%s err=%v\n", path, err)
			continue
		}
		var cp BundleResumeCheckpoint
		if err := json.Unmarshal(data, &cp); err != nil {
			log.Printf("[BundleResume] Checkpoint invalid path=%s reason=decode err=%v\n", path, err)
			continue
		}
		if cp.ResumeSchemaVersion != bundleResumeSchemaVersion {
			if cp.ResumeSchemaVersion != bundleResumeLegacyVersion ||
				!models.AppConfig.AcceptLegacyBundleV2Checkpoints() {
				log.Printf("[BundleResume] Checkpoint invalid path=%s reason=schema version=%d\n", path, cp.ResumeSchemaVersion)
				continue
			}
			log.Printf("[BundleResume] Accepting legacy v%d checkpoint path=%s policy=accept_success_as_complete\n", cp.ResumeSchemaVersion, path)
		}
		if cp.BundleName == "" {
			log.Printf("[BundleResume] Checkpoint invalid path=%s reason=empty bundle name\n", path)
			continue
		}
		checkpoints[cp.BundleName] = cp
	}
	return checkpoints
}

// ValidBundleResumeCheckpoint 校验 checkpoint 是否匹配当前 bundle。
func ValidBundleResumeCheckpoint(
	cp BundleResumeCheckpoint,
	reportID uint,
	engineMode string,
	bundle chunker.SemanticBundle,
	metadata BundleResumeMetadata,
	snapshotHash string,
) bool {
	versionAccepted := cp.ResumeSchemaVersion == bundleResumeSchemaVersion ||
		(cp.ResumeSchemaVersion == bundleResumeLegacyVersion &&
			models.AppConfig.AcceptLegacyBundleV2Checkpoints())
	if !versionAccepted ||
		cp.ReportID != reportID ||
		cp.EngineMode != engineMode ||
		cp.BundleName != bundle.Name ||
		!sameStringSlice(cp.Files, bundle.AllFiles) {
		return false
	}
	if snapshotHash == "" || cp.SnapshotHash != snapshotHash {
		log.Printf("[BundleResume] Checkpoint invalid bundle=%q reason=snapshot_mismatch\n", bundle.Name)
		return false
	}
	if cp.Metadata.EngineConfigHash != metadata.EngineConfigHash {
		log.Printf("[BundleResume] Checkpoint invalid bundle=%q reason=engine_config_mismatch\n", bundle.Name)
		return false
	}
	if metadata.PlanManifestHash != "" && cp.Metadata.PlanManifestHash != metadata.PlanManifestHash {
		log.Printf("[BundleResume] Checkpoint invalid bundle=%q reason=plan_manifest_mismatch\n", bundle.Name)
		return false
	}
	if cp.Metadata.PlanManifestHash != "" && cp.Metadata.PlanManifestHash != metadata.PlanManifestHash {
		log.Printf("[BundleResume] Checkpoint invalid bundle=%q reason=plan_manifest_mismatch\n", bundle.Name)
		return false
	}
	if cp.Metadata.PromptHash != metadata.PromptHash {
		log.Printf("[BundleResume] Checkpoint invalid bundle=%q reason=prompt_mismatch\n", bundle.Name)
		return false
	}
	if cp.Metadata.CommitHash != "" && metadata.CommitHash != "" && cp.Metadata.CommitHash != metadata.CommitHash {
		log.Printf("[BundleResume] Checkpoint invalid bundle=%q reason=commit_mismatch\n", bundle.Name)
		return false
	}
	if metadata.HasChangeBaseline() {
		if !metadata.ChangeBaselineComplete() || !cp.Metadata.ChangeBaselineComplete() {
			log.Printf("[BundleResume] Checkpoint invalid bundle=%q reason=change_baseline_incomplete\n", bundle.Name)
			return false
		}
		if cp.Metadata.ChangeBaseCommit != metadata.ChangeBaseCommit {
			log.Printf("[BundleResume] Checkpoint invalid bundle=%q reason=change_base_mismatch\n", bundle.Name)
			return false
		}
		if cp.Metadata.ChangeHeadCommit != metadata.ChangeHeadCommit {
			log.Printf("[BundleResume] Checkpoint invalid bundle=%q reason=change_head_mismatch\n", bundle.Name)
			return false
		}
		if cp.Metadata.DiffManifestHash != metadata.DiffManifestHash {
			log.Printf("[BundleResume] Checkpoint invalid bundle=%q reason=diff_manifest_mismatch\n", bundle.Name)
			return false
		}
	}
	return true
}

// BundleResumeCheckpointPath 返回 bundle checkpoint 文件路径。
func BundleResumeCheckpointPath(chunkDir string, bundleIndex int, bundleName string) string {
	return filepath.Join(chunkDir, fmt.Sprintf("resume-%d-%s.json", bundleIndex, sanitizeDebateChunkName(bundleName)))
}

func sameStringSlice(left, right []string) bool {
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
