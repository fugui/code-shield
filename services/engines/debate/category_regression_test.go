package debate

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"code-shield/models"
)

type taskSnapshotHashes struct {
	TaxonomyHash string
	PromptHash   string
}

type categoryRegressionSample struct {
	TaskType        string            `json:"task_type"`
	CandidateID     string            `json:"candidate_id"`
	CandidateFacts  map[string]string `json:"candidate_facts"`
	ExpectedCode    string            `json:"expected_code"`
	ForbiddenLabels []string          `json:"forbidden_labels"`
	TaxonomyHash    string            `json:"taxonomy_hash"`
	PromptHash      string            `json:"prompt_hash"`
	ModelBackend    string            `json:"model_backend"`
}

const expectedRegressionTaxonomyHash = "sha256:6c10bae97849fd70f37467be8884101c99f5ddc507f8246ed8fd38542d8adbcf"
const expectedRegressionPromptHash = "sha256:72af4c56c62c1dbd8fc2ee9ba28dbae518797ea2739045eb4c850a8c871f06ac"

func TestSemanticRegressionCoversClosePairs(t *testing.T) {
	file, err := os.Open(filepath.Join("..", "..", "..", "testdata", "category-regression.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	byPair := map[string]int{}
	byTask := map[string]int{}
	sawAnchorSample := false
	seenIDs := map[string]struct{}{}
	taskHashes := taskTypeSnapshotHashes(t)
	scanner := bufio.NewScanner(file)
	line := 0
	for scanner.Scan() {
		line++
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var sample categoryRegressionSample
		if err := json.Unmarshal(scanner.Bytes(), &sample); err != nil {
			t.Fatalf("decode regression line %d: %v", line, err)
		}
		if sample.TaskType == "" || sample.CandidateID == "" || sample.ExpectedCode == "" ||
			sample.TaxonomyHash == "" || sample.PromptHash == "" || sample.ModelBackend == "" ||
			len(sample.ForbiddenLabels) == 0 || len(sample.CandidateFacts) == 0 {
			t.Fatalf("incomplete metadata on regression line %d: %#v", line, sample)
		}
		for _, key := range []string{"code", "threading", "allocation", "ownership", "scope", "lifetime", "guard", "failure_path"} {
			if strings.TrimSpace(sample.CandidateFacts[key]) == "" {
				t.Fatalf("empty %s fact on regression line %d", key, line)
			}
		}
		expectedHashes := taskHashes[sample.TaskType]
		if sample.TaxonomyHash != expectedHashes.TaxonomyHash || sample.PromptHash != expectedHashes.PromptHash {
			t.Fatalf("stale regression hashes on line %d: got taxonomy=%q prompt=%q; want taxonomy=%q prompt=%q",
				line, sample.TaxonomyHash, sample.PromptHash, expectedHashes.TaxonomyHash, expectedHashes.PromptHash)
		}
		if _, exists := seenIDs[sample.CandidateID]; exists {
			t.Fatalf("duplicate regression candidate_id %q", sample.CandidateID)
		}
		byTask[sample.TaskType]++
		seenIDs[sample.CandidateID] = struct{}{}
		if sample.CandidateID == "30428/H-010" {
			sawAnchorSample = true
		}
		if prefix, ok := strings.CutPrefix(sample.CandidateID, "regression/"); ok {
			pair, _, _ := strings.Cut(prefix, "-case-")
			byPair[pair]++
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if !sawAnchorSample {
		t.Fatal("30428/H-010 is absent from semantic regression")
	}
	pairs := []string{"race-vs-uaf", "uaf-vs-dangling", "callback-vs-thread", "null-vs-overflow", "uninit-vs-race", "uaf-vs-race"}
	for _, pair := range pairs {
		if byPair[pair] < 20 {
			t.Fatalf("semantic regression pair %q has %d samples, want at least 20", pair, byPair[pair])
		}
	}
	for taskName, count := range byTask {
		if taskName == "coredump_risk" {
			continue
		}
		if count < 20 {
			t.Fatalf("semantic regression task %q has %d samples, want at least 20", taskName, count)
		}
	}
}

func taskTypeSnapshotHashes(t *testing.T) map[string]taskSnapshotHashes {
	t.Helper()
	hashes := make(map[string]taskSnapshotHashes)
	matches, err := filepath.Glob(filepath.Join("..", "..", "..", "tasks", "*", "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, match := range matches {
		raw, err := os.ReadFile(match)
		if err != nil {
			t.Fatal(err)
		}
		var taskType models.TaskType
		if err := json.Unmarshal(raw, &taskType); err != nil {
			t.Fatalf("decode %s: %v", match, err)
		}
		taxonomy := taskType.GetCategoryTaxonomy()
		if taxonomy == nil {
			continue
		}
		taxonomy.Hash = ""
		taxonomyRaw, err := json.Marshal(taxonomy)
		if err != nil {
			t.Fatal(err)
		}
		taxonomySum := sha256.Sum256(taxonomyRaw)
		promptRaw, err := os.ReadFile(filepath.Join(filepath.Dir(match), "analysis_prompt.md"))
		if err != nil {
			t.Fatal(err)
		}
		promptSum := sha256.Sum256(promptRaw)
		hashes[taskType.Name] = taskSnapshotHashes{
			TaxonomyHash: "sha256:" + hex.EncodeToString(taxonomySum[:]),
			PromptHash:   "sha256:" + hex.EncodeToString(promptSum[:]),
		}
	}
	return hashes
}
