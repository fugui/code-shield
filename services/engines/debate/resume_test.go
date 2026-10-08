package debate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"code-shield/models"
	"code-shield/services/engines"
	"code-shield/services/engines/chunked"
	"code-shield/services/engines/chunker"
)

func TestBundleResumeMetadataHashesPromptContentLikeFile(t *testing.T) {
	tempDir := t.TempDir()
	promptPath := filepath.Join(tempDir, "analysis_prompt.md")
	promptContent := "immutable prompt content"
	if err := os.WriteFile(promptPath, []byte(promptContent), 0644); err != nil {
		t.Fatalf("write prompt file: %v", err)
	}

	fileHash := chunked.BuildChunkResumeMetadata("", nil, promptPath).PromptHash
	contentHash := HashPromptContent(promptContent)
	if contentHash == "" || contentHash != fileHash {
		t.Fatalf("content hash = %q, file hash = %q", contentHash, fileHash)
	}
	metadata := BuildBundleResumeMetadata(tempDir, nil, promptContent, "plan-hash")
	if metadata.PromptHash != contentHash {
		t.Fatalf("metadata prompt hash = %q, want %q", metadata.PromptHash, contentHash)
	}
	if BuildBundleResumeMetadata(tempDir, nil, "", "plan-hash").PromptHash != "" {
		t.Fatal("empty prompt content should produce empty prompt hash")
	}
}

func TestBundleResumeCheckpointRoundTrip(t *testing.T) {
	dir := t.TempDir()
	bundle := chunker.SemanticBundle{
		Name:     "src/service",
		AllFiles: []string{"src/service/b.h", "src/service/a.cc"},
	}
	findings := []models.AnalysisFinding{{Title: "finding", Severity: "严重"}}
	logs := []models.TaskDebateLog{{ChunkName: bundle.Name, CandidateID: "H-001", Verdict: "CONFIRMED"}}
	metadata := BundleResumeMetadata{EngineConfigHash: "config"}

	snapshotHash := "snapshot"
	if err := WriteBundleResumeCheckpoint(dir, 42, "debate_full", 7, bundle, findings, logs, 11, 22, metadata, snapshotHash, BundleArtifactMetrics{ArtifactComplete: boolPtr(true), ArtifactState: "observed"}); err != nil {
		t.Fatalf("WriteBundleResumeCheckpoint() error = %v", err)
	}

	matches, err := filepath.Glob(filepath.Join(dir, "resume-*.json"))
	if err != nil {
		t.Fatalf("glob checkpoints error = %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 checkpoint, got %d", len(matches))
	}

	loaded := LoadBundleResumeCheckpoints(dir)
	cp, ok := loaded[bundle.Name]
	if !ok {
		t.Fatalf("checkpoint for bundle %q was not loaded", bundle.Name)
	}
	if cp.ResumeSchemaVersion != bundleResumeSchemaVersion || cp.ReportID != 42 || cp.EngineMode != "debate_full" {
		t.Fatalf("unexpected checkpoint header: %+v", cp)
	}
	if len(cp.Result.Findings) != 1 || cp.Result.Findings[0].Title != "finding" {
		t.Fatalf("unexpected findings: %+v", cp.Result.Findings)
	}
	if cp.Result.HunterTokens != 11 || cp.Result.Tier2Tokens != 22 {
		t.Fatalf("unexpected tokens: %+v", cp.Result)
	}
	if !ValidBundleResumeCheckpoint(cp, 42, "debate_full", bundle, metadata, snapshotHash) {
		t.Fatal("ValidBundleResumeCheckpoint() = false, want true")
	}
}

func TestBundleResumeCheckpointInvalidFilesAreIgnored(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "resume-broken.json"), []byte("{invalid"), 0644); err != nil {
		t.Fatalf("write broken checkpoint error = %v", err)
	}

	bundle := chunker.SemanticBundle{Name: "src", AllFiles: []string{"src/a.cc"}}
	valid := BundleResumeCheckpoint{
		ResumeSchemaVersion: bundleResumeSchemaVersion,
		ReportID:            1,
		EngineMode:          "chunked_fast",
		BundleIndex:         2,
		BundleName:          bundle.Name,
		Files:               bundle.AllFiles,
		Metadata:            BundleResumeMetadata{EngineConfigHash: "config"},
		SnapshotHash:        "snapshot",
	}
	if err := WriteBundleResumeCheckpoint(dir, valid.ReportID, valid.EngineMode, valid.BundleIndex, bundle, nil, nil, 0, 0, BundleResumeMetadata{EngineConfigHash: "config"}, "snapshot", BundleArtifactMetrics{ArtifactComplete: boolPtr(true), ArtifactState: "observed"}); err != nil {
		t.Fatalf("WriteBundleResumeCheckpoint() error = %v", err)
	}

	loaded := LoadBundleResumeCheckpoints(dir)
	if len(loaded) != 1 {
		t.Fatalf("expected only valid checkpoint to load, got %d", len(loaded))
	}
	cp := loaded[bundle.Name]
	if ValidBundleResumeCheckpoint(cp, 1, "debate_full", bundle, BundleResumeMetadata{EngineConfigHash: "config"}, "snapshot") {
		t.Fatal("engine mode mismatch must invalidate checkpoint")
	}
	bundle.AllFiles = []string{"src/b.cc"}
	if ValidBundleResumeCheckpoint(cp, 1, "chunked_fast", bundle, BundleResumeMetadata{EngineConfigHash: "config"}, "snapshot") {
		t.Fatal("file list mismatch must invalidate checkpoint")
	}
}

func TestBundleResumeCheckpointRejectsLegacyV2ByDefault(t *testing.T) {
	tempDir := t.TempDir()
	bundle := chunker.SemanticBundle{Name: "legacy", AllFiles: []string{"a.cc"}}
	checkpoint := BundleResumeCheckpoint{
		ResumeSchemaVersion: 2,
		ReportID:            1,
		EngineMode:          "debate_full",
		BundleName:          bundle.Name,
		Files:               bundle.AllFiles,
		SnapshotHash:        "snapshot",
		Metadata:            BundleResumeMetadata{EngineConfigHash: "config"},
	}
	data, err := json.Marshal(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(BundleResumeCheckpointPath(tempDir, 1, bundle.Name), data, 0644); err != nil {
		t.Fatal(err)
	}
	if loaded := LoadBundleResumeCheckpoints(tempDir); len(loaded) != 0 {
		t.Fatalf("legacy v2 checkpoint must be rejected by default, loaded=%d", len(loaded))
	}
}

func TestBundleResumeCheckpointRejectsPlanManifestMismatch(t *testing.T) {
	dir := t.TempDir()
	bundle := chunker.SemanticBundle{Name: "src", AllFiles: []string{"src/a.cc"}}
	metadata := BundleResumeMetadata{
		PlanManifestHash: "sha256:plan-1",
		EngineConfigHash: "config",
	}
	snapshotHash := "snapshot"
	if err := WriteBundleResumeCheckpoint(dir, 1, "debate_full", 0, bundle, nil, nil, 0, 0, metadata, snapshotHash, BundleArtifactMetrics{ArtifactComplete: boolPtr(true)}); err != nil {
		t.Fatalf("write checkpoint: %v", err)
	}

	cp := LoadBundleResumeCheckpoints(dir)[bundle.Name]
	if !ValidBundleResumeCheckpoint(cp, 1, "debate_full", bundle, metadata, snapshotHash) {
		t.Fatal("checkpoint with matching plan manifest should be valid")
	}

	changedMetadata := metadata
	changedMetadata.PlanManifestHash = "sha256:plan-2"
	if ValidBundleResumeCheckpoint(cp, 1, "debate_full", bundle, changedMetadata, snapshotHash) {
		t.Fatal("changed plan manifest must invalidate checkpoint")
	}

	legacyMetadata := metadata
	legacyMetadata.PlanManifestHash = ""
	if ValidBundleResumeCheckpoint(cp, 1, "debate_full", bundle, legacyMetadata, snapshotHash) {
		t.Fatal("checkpoint written with plan manifest must require plan identity")
	}
}

func TestBundleResumeCheckpointBindsChangeBaseline(t *testing.T) {
	dir := t.TempDir()
	bundle := chunker.SemanticBundle{Name: "src", AllFiles: []string{"src/a.cc"}}
	metadata := BundleResumeMetadata{
		PlanManifestHash: "sha256:plan",
		EngineConfigHash: "config",
		ChangeBaseCommit: "sha256:base",
		ChangeHeadCommit: "sha256:head",
		DiffManifestHash: "sha256:diff",
	}
	if err := WriteBundleResumeCheckpoint(
		dir, 1, "debate_full", 0, bundle, nil, nil, 0, 0, metadata, "snapshot",
		BundleArtifactMetrics{ArtifactComplete: boolPtr(true)},
	); err != nil {
		t.Fatal(err)
	}
	cp := LoadBundleResumeCheckpoints(dir)[bundle.Name]
	if !ValidBundleResumeCheckpoint(cp, 1, "debate_full", bundle, metadata, "snapshot") {
		t.Fatal("checkpoint with complete change baseline should be valid")
	}

	tests := []struct {
		name   string
		mutate func(*BundleResumeMetadata)
	}{
		{name: "base mismatch", mutate: func(metadata *BundleResumeMetadata) { metadata.ChangeBaseCommit = "sha256:base-2" }},
		{name: "head mismatch", mutate: func(metadata *BundleResumeMetadata) { metadata.ChangeHeadCommit = "sha256:head-2" }},
		{name: "manifest mismatch", mutate: func(metadata *BundleResumeMetadata) { metadata.DiffManifestHash = "sha256:diff-2" }},
		{name: "current incomplete", mutate: func(metadata *BundleResumeMetadata) { metadata.DiffManifestHash = "" }},
		{name: "checkpoint incomplete", mutate: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			current := metadata
			checkpoint := cp
			if test.mutate != nil {
				test.mutate(&current)
			} else {
				checkpoint.Metadata.DiffManifestHash = ""
			}
			if ValidBundleResumeCheckpoint(checkpoint, 1, "debate_full", bundle, current, "snapshot") {
				t.Fatalf("%s must invalidate checkpoint", test.name)
			}
		})
	}
}

func TestBundleResumeCheckpointPersistsQuarantine(t *testing.T) {
	tempDir := t.TempDir()
	bundle := chunker.SemanticBundle{Name: "quarantined", AllFiles: []string{"a.cc"}}
	complete := false
	if err := WriteBundleResumeCheckpoint(
		tempDir, 1, "debate_full", 1, bundle, nil, nil, 0, 0,
		BundleResumeMetadata{EngineConfigHash: "config"}, "snapshot",
		BundleArtifactMetrics{
			ArtifactComplete:         &complete,
			ArtifactState:            "observed_incomplete",
			CandidateQuarantineCount: 1,
		},
	); err != nil {
		t.Fatal(err)
	}

	loaded := LoadBundleResumeCheckpoints(tempDir)[bundle.Name]
	if loaded.Result.Artifact.ArtifactComplete == nil || *loaded.Result.Artifact.ArtifactComplete {
		t.Fatalf("quarantine state not persisted: %+v", loaded.Result)
	}
	if loaded.Result.Artifact.CandidateQuarantineCount != 1 {
		t.Fatalf("quarantine count = %d, want 1", loaded.Result.Artifact.CandidateQuarantineCount)
	}
}

func TestBundleResumeCheckpointRejectsChangedSource(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, "a.cc"), []byte("int a() { return 1; }\n"), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	bundle := chunker.SemanticBundle{Name: "src", AllFiles: []string{"a.cc"}}
	metadata := BuildBundleResumeMetadata(tempDir, nil, "", "")
	snapshotHash, err := chunked.ChunkSnapshotHash(tempDir, bundle.AllFiles)
	if err != nil {
		t.Fatalf("snapshot hash: %v", err)
	}
	if err := WriteBundleResumeCheckpoint(
		tempDir,
		42,
		"debate_full",
		1,
		bundle,
		nil,
		nil,
		0,
		0,
		metadata,
		snapshotHash,
		BundleArtifactMetrics{ArtifactComplete: boolPtr(true), ArtifactState: "observed"},
	); err != nil {
		t.Fatalf("write checkpoint: %v", err)
	}

	cp := LoadBundleResumeCheckpoints(tempDir)[bundle.Name]
	if !ValidBundleResumeCheckpoint(cp, 42, "debate_full", bundle, metadata, snapshotHash) {
		t.Fatal("checkpoint should be valid before source changes")
	}

	if err := os.WriteFile(filepath.Join(tempDir, "a.cc"), []byte("int a() { return 2; }\n"), 0644); err != nil {
		t.Fatalf("change source: %v", err)
	}
	changedHash, err := chunked.ChunkSnapshotHash(tempDir, bundle.AllFiles)
	if err != nil {
		t.Fatalf("changed snapshot hash: %v", err)
	}
	if ValidBundleResumeCheckpoint(cp, 42, "debate_full", bundle, metadata, changedHash) {
		t.Fatal("checkpoint must be invalid after source content changes")
	}
}

func TestDebateEngineSkipsValidResumeCheckpoints(t *testing.T) {
	tempDir := t.TempDir()
	repoDir := filepath.Join(tempDir, "repo")
	if err := os.MkdirAll(filepath.Join(repoDir, "alpha"), 0755); err != nil {
		t.Fatalf("create alpha dir error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoDir, "beta"), 0755); err != nil {
		t.Fatalf("create beta dir error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "alpha", "a.cc"), []byte("int a() { return 1; }\n"), 0644); err != nil {
		t.Fatalf("write alpha source error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "beta", "b.cc"), []byte("int b() { return 2; }\n"), 0644); err != nil {
		t.Fatalf("write beta source error = %v", err)
	}

	bundles := []chunker.SemanticBundle{
		{Name: "alpha", AllFiles: []string{filepath.Join("alpha", "a.cc")}},
		{Name: "beta", AllFiles: []string{filepath.Join("beta", "b.cc")}},
	}
	engineConfig := json.RawMessage(`{"scan_profile":{"version":1,"name":"full_review"}}`)
	reportPath := filepath.Join(tempDir, "report.md")
	chunkDir := filepath.Join(tempDir, "debate-chunks-42-repo")
	for bundleIdx, bundle := range bundles {
		findings := []models.AnalysisFinding{{Title: bundle.Name + " finding", Severity: "一般"}}
		snapshotHash, err := chunked.ChunkSnapshotHash(repoDir, bundle.AllFiles)
		if err != nil {
			t.Fatalf("snapshot hash for %s error = %v", bundle.Name, err)
		}
		if err := WriteBundleResumeCheckpoint(
			chunkDir,
			42,
			"debate_full",
			bundleIdx,
			bundle,
			findings,
			nil,
			3,
			0,
			BuildBundleResumeMetadata(repoDir, engineConfig, "", ""),
			snapshotHash,
			BundleArtifactMetrics{ArtifactComplete: boolPtr(true), ArtifactState: "observed"},
		); err != nil {
			t.Fatalf("write checkpoint for %s error = %v", bundle.Name, err)
		}
	}

	ctx := &engines.EngineContext{
		Ctx:          context.Background(),
		ReportID:     42,
		RepoName:     "repo",
		EngineMode:   "debate_full",
		CodesPath:    repoDir,
		ReportPath:   reportPath,
		EngineConfig: engineConfig,
	}
	result, err := (&DebateEngine{}).Run(ctx)
	if err != nil {
		t.Fatalf("DebateEngine.Run() error = %v", err)
	}
	if len(result.SummaryChunks) != len(bundles) {
		t.Fatalf("expected %d summary chunks, got %d", len(bundles), len(result.SummaryChunks))
	}
	for i, detail := range result.SummaryChunks {
		if detail.Status != "success" || !detail.Resumed {
			t.Fatalf("bundle %q was not reused: status=%q resumed=%v", detail.ChunkName, detail.Status, detail.Resumed)
		}
		if detail.ChunkName != bundles[i].Name {
			t.Fatalf("summary slot %d = %q, want %q", i, detail.ChunkName, bundles[i].Name)
		}
	}
	if len(result.Findings) != len(bundles) {
		t.Fatalf("expected %d reused findings, got %d", len(bundles), len(result.Findings))
	}
}

func boolPtr(value bool) *bool {
	return &value
}
