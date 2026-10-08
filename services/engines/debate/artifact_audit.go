package debate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"

	"code-shield/models"
	"code-shield/services/engines/chunker"

	"gorm.io/datatypes"
)

type ArtifactRepairAttempt struct {
	Attempt      int      `json:"attempt"`
	Source       string   `json:"source"`
	Raw          string   `json:"raw,omitempty"`
	Tokens       int64    `json:"tokens,omitempty"`
	Error        string   `json:"error,omitempty"`
	Accepted     bool     `json:"accepted"`
	Drifted      bool     `json:"drifted,omitempty"`
	NonRetryable bool     `json:"non_retryable,omitempty"`
	SyntaxFixes  []string `json:"syntax_fixes,omitempty"`
}

type HunterArtifactRepairAttempt = ArtifactRepairAttempt

type ArtifactRepairMetrics struct {
	OriginalArtifactHash   string                  `json:"original_artifact_hash,omitempty"`
	Driver                 string                  `json:"driver,omitempty"`
	ResourceID             string                  `json:"resource_id,omitempty"`
	ResponseFormatMode     string                  `json:"response_format_mode,omitempty"`
	RepairTokens           int64                   `json:"repair_tokens"`
	FinalStatus            string                  `json:"final_status"`
	SchemaID               string                  `json:"schema_id"`
	SchemaHash             string                  `json:"schema_hash"`
	LocalRepairs           int                     `json:"local_repairs"`
	SyntaxRepairs          int                     `json:"syntax_repairs"`
	LLMRepairs             int                     `json:"llm_repairs"`
	LLMRepairAttempts      int                     `json:"llm_repair_attempts"`
	LLMRepairSuccesses     int                     `json:"llm_repair_successes"`
	SchemaRepairIssues     []ArtifactIssue         `json:"schema_repair_issues"`
	RepairDrifted          bool                    `json:"repair_drifted"`
	RepairDriftUnchecked   bool                    `json:"repair_drift_unchecked"`
	RepairDriftUnitRef     string                  `json:"repair_drift_unit_ref,omitempty"`
	BaselineSignatureKnown bool                    `json:"baseline_signature_known"`
	RepairedSignatureKnown bool                    `json:"repaired_signature_known"`
	BaselineSignatureHash  string                  `json:"baseline_signature_hash,omitempty"`
	RepairedSignatureHash  string                  `json:"repaired_signature_hash,omitempty"`
	UnverifiedRepair       bool                    `json:"unverified_repair,omitempty"`
	RepairOutcome          string                  `json:"repair_outcome,omitempty"`
	RepairAttempts         []ArtifactRepairAttempt `json:"repair_attempts,omitempty"`
}

type HunterArtifactRepairMetrics = ArtifactRepairMetrics

func artifactRepairHash(raw []byte) string {
	digest := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func newArtifactRepairMetrics(raw []byte, contract OutputContract) *ArtifactRepairMetrics {
	return &ArtifactRepairMetrics{
		OriginalArtifactHash: artifactRepairHash(raw),
		ResponseFormatMode:   "prompt_contract",
		FinalStatus:          "failed",
		SchemaID:             contract.SchemaID,
		SchemaHash:           contract.SchemaHash,
	}
}

func newHunterRepairMetrics(raw []byte, contract OutputContract) *ArtifactRepairMetrics {
	return newArtifactRepairMetrics(raw, contract)
}

func NewSynthesisArtifactRepairContract() OutputContract {
	contract := OutputContract{
		Profile:  OutputProfileSynthesis,
		Stage:    "synthesis",
		SchemaID: SynthesisReportArtifactSchema,
	}
	contract.SchemaHash = outputContractSchemaHash(contract)
	return contract
}

func NewSynthesisArtifactRepairMetrics(raw []byte) *ArtifactRepairMetrics {
	return newArtifactRepairMetrics(raw, NewSynthesisArtifactRepairContract())
}

func (metrics *ArtifactRepairMetrics) AddIssue(issue ArtifactIssue) {
	metrics.addIssue(issue)
}

func (metrics *ArtifactRepairMetrics) AddLocalRepair(raw []byte, accepted bool, repairErr error) {
	metrics.addLocalRepair(raw, accepted, repairErr)
}

func (metrics *ArtifactRepairMetrics) AddLLMRepair(raw string, tokens int64, accepted bool, drifted, nonRetryable bool, repairErr error) {
	metrics.addLLMRepair(raw, tokens, accepted, drifted, nonRetryable, repairErr)
}

func (metrics *ArtifactRepairMetrics) SetFinalStatus(status string) {
	metrics.setFinalStatus(status)
}

func PersistArtifactRepairAudit(
	ctx context.Context,
	bundleID string,
	reportID, repoID, taskTypeID uint,
	metrics *ArtifactRepairMetrics,
) {
	persistStageArtifactRepairAudit(ctx, chunker.SemanticBundle{}, bundleID, reportID, repoID, taskTypeID, metrics)
}

func (metrics *ArtifactRepairMetrics) addIssue(issue ArtifactIssue) {
	if metrics == nil {
		return
	}
	metrics.SchemaRepairIssues = append(metrics.SchemaRepairIssues, issue)
}

func (metrics *ArtifactRepairMetrics) addDirectAttempt(raw string, accepted bool, repairErr error) {
	if metrics == nil {
		return
	}
	metrics.addDirectAttemptWithTokens(raw, 0, accepted, repairErr)
}

func (metrics *ArtifactRepairMetrics) addDirectAttemptWithTokens(raw string, tokens int64, accepted bool, repairErr error) {
	if metrics == nil {
		return
	}
	metrics.RepairAttempts = append(metrics.RepairAttempts, ArtifactRepairAttempt{
		Attempt:  len(metrics.RepairAttempts) + 1,
		Source:   "direct",
		Raw:      raw,
		Tokens:   tokens,
		Accepted: accepted,
		Error:    errorText(repairErr),
	})
}

func (metrics *ArtifactRepairMetrics) addLocalRepair(raw []byte, accepted bool, repairErr error) {
	if metrics == nil {
		return
	}
	metrics.LocalRepairs++
	metrics.RepairTokens += int64(len(raw) / 4)
	localTokens := int64(len(raw) / 4)
	metrics.RepairAttempts = append(metrics.RepairAttempts, ArtifactRepairAttempt{
		Attempt:  len(metrics.RepairAttempts) + 1,
		Source:   "local",
		Raw:      string(raw),
		Tokens:   localTokens,
		Accepted: accepted,
		Error:    errorText(repairErr),
	})
}

func (metrics *ArtifactRepairMetrics) addSyntaxRepair(raw []byte, accepted bool, repairErr error, fixes ...string) {
	if metrics == nil {
		return
	}
	metrics.SyntaxRepairs++
	metrics.LocalRepairs++
	localTokens := int64(len(raw) / 4)
	metrics.RepairTokens += localTokens
	metrics.RepairAttempts = append(metrics.RepairAttempts, ArtifactRepairAttempt{
		Attempt:     len(metrics.RepairAttempts) + 1,
		Source:      "local syntax",
		Raw:         string(raw),
		Tokens:      localTokens,
		Accepted:    accepted,
		Error:       errorText(repairErr),
		SyntaxFixes: append([]string(nil), fixes...),
	})
}

func (metrics *ArtifactRepairMetrics) addLLMRepair(raw string, tokens int64, accepted bool, drifted, nonRetryable bool, repairErr error) {
	if metrics == nil {
		return
	}
	metrics.LLMRepairAttempts++
	metrics.LLMRepairs++
	if accepted {
		metrics.LLMRepairSuccesses++
	}
	if drifted {
		metrics.RepairDrifted = true
	}
	metrics.RepairTokens += tokens
	metrics.RepairAttempts = append(metrics.RepairAttempts, ArtifactRepairAttempt{
		Attempt:      len(metrics.RepairAttempts) + 1,
		Source:       "llm",
		Raw:          raw,
		Tokens:       tokens,
		Accepted:     accepted,
		Drifted:      drifted,
		NonRetryable: nonRetryable,
		Error:        errorText(repairErr),
	})
}

func (metrics *ArtifactRepairMetrics) setFinalStatus(status string) {
	if metrics != nil {
		metrics.FinalStatus = status
	}
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func persistStageArtifactRepairAudit(
	ctx context.Context,
	bundle chunker.SemanticBundle,
	bundleID string,
	reportID, repoID, taskTypeID uint,
	metrics *HunterArtifactRepairMetrics,
) {
	if metrics == nil || models.DB == nil || reportID == 0 {
		return
	}
	schemaIssues, issuesErr := json.Marshal(metrics.SchemaRepairIssues)
	attempts, attemptsErr := json.Marshal(metrics.RepairAttempts)
	if issuesErr != nil || attemptsErr != nil {
		log.Printf("[ArtifactContract] failed to encode repair audit report=%d bundle=%s issues=%v attempts=%v",
			reportID, bundleID, issuesErr, attemptsErr)
		return
	}
	audit := models.ArtifactRepairAudit{
		ReportID:               reportID,
		RepoID:                 repoID,
		TaskTypeID:             taskTypeID,
		BundleID:               bundleID,
		Driver:                 metrics.Driver,
		ResourceID:             metrics.ResourceID,
		ResponseFormatMode:     metrics.ResponseFormatMode,
		SchemaID:               metrics.SchemaID,
		SchemaHash:             metrics.SchemaHash,
		OriginalArtifactHash:   metrics.OriginalArtifactHash,
		FinalStatus:            metrics.FinalStatus,
		RepairTokens:           metrics.RepairTokens,
		LocalRepairs:           metrics.LocalRepairs,
		SyntaxRepairs:          metrics.SyntaxRepairs,
		LLMRepairs:             metrics.LLMRepairs,
		LLMRepairAttempts:      metrics.LLMRepairAttempts,
		LLMRepairSuccesses:     metrics.LLMRepairSuccesses,
		RepairDrifted:          metrics.RepairDrifted,
		RepairDriftUnchecked:   metrics.RepairDriftUnchecked,
		RepairDriftUnitRef:     metrics.RepairDriftUnitRef,
		BaselineSignatureKnown: metrics.BaselineSignatureKnown,
		RepairedSignatureKnown: metrics.RepairedSignatureKnown,
		BaselineSignatureHash:  metrics.BaselineSignatureHash,
		RepairedSignatureHash:  metrics.RepairedSignatureHash,
		UnverifiedRepair:       metrics.UnverifiedRepair,
		RepairOutcome:          metrics.RepairOutcome,
		SchemaRepairIssues:     sanitizePostgreSQLJSON(schemaIssues),
		RepairAttempts:         sanitizePostgreSQLJSON(attempts),
	}
	if err := models.CreateArtifactRepairAudit(models.DB, &audit); err != nil {
		log.Printf("[ArtifactContract] failed to persist repair audit report=%d bundle=%s err=%v",
			reportID, bundleID, err)
		return
	}
	logArtifactRepairMetrics(metrics, bundleID)
}

func logArtifactRepairMetrics(metrics *ArtifactRepairMetrics, bundleName string) {
	if metrics == nil {
		return
	}
	issueCodes := make([]string, 0, len(metrics.SchemaRepairIssues))
	for _, issue := range metrics.SchemaRepairIssues {
		issueCodes = append(issueCodes, issue.Code)
	}
	log.Printf("[ArtifactContract] repair metrics bundle=%s schema=%s status=%s outcome=%s local=%d syntax=%d llm=%d/%d attempts=%d drifted=%t drift_unchecked=%t issues=%s",
		bundleName, metrics.SchemaID, metrics.FinalStatus, metrics.RepairOutcome, metrics.LocalRepairs,
		metrics.SyntaxRepairs,
		metrics.LLMRepairSuccesses, metrics.LLMRepairs, len(metrics.RepairAttempts),
		metrics.RepairDrifted, metrics.RepairDriftUnchecked, strings.Join(issueCodes, ","))
}

func sanitizePostgreSQLJSON(raw json.RawMessage) datatypes.JSON {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return datatypes.JSON(raw)
	}
	encoded, err := json.Marshal(sanitizeJSONValue(value))
	if err != nil {
		return datatypes.JSON(raw)
	}
	return encoded
}

func sanitizeJSONValue(value any) any {
	switch typed := value.(type) {
	case string:
		return strings.ReplaceAll(typed, "\x00", "")
	case []any:
		for index, item := range typed {
			typed[index] = sanitizeJSONValue(item)
		}
		return typed
	case map[string]any:
		for key, item := range typed {
			delete(typed, key)
			typed[strings.ReplaceAll(key, "\x00", "")] = sanitizeJSONValue(item)
		}
		return typed
	default:
		return value
	}
}

func hunterBusinessSignature(raw []byte) (map[string]string, bool) {
	var root struct {
		Candidates []struct {
			CandidateID      string `json:"candidate_id"`
			FilePath         string `json:"file_path"`
			LineRange        string `json:"line_range"`
			TriggerLine      string `json:"trigger_line"`
			ScopeSymbol      string `json:"scope_symbol"`
			Category         string `json:"category"`
			CategoryCode     string `json:"category_code"`
			Title            string `json:"title"`
			CodeSnippet      string `json:"code_snippet"`
			TriggerCondition string `json:"trigger_condition"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(cleanJSONOutput(raw), &root); err != nil {
		return nil, false
	}
	signature := make(map[string]string, len(root.Candidates))
	for _, candidate := range root.Candidates {
		key := candidate.CandidateID
		signature[key] = fmt.Sprintf("%s\x1f%s\x1f%s\x1f%s\x1f%s\x1f%s\x1f%s\x1f%s\x1f%s",
			candidate.FilePath, candidate.LineRange, candidate.TriggerLine,
			candidate.ScopeSymbol, candidate.Category, candidate.CategoryCode,
			candidate.Title, candidate.CodeSnippet, candidate.TriggerCondition,
		)
	}
	return signature, true
}

func businessSignatureHash(signature map[string]string) string {
	type signatureEntry struct {
		CandidateID string `json:"candidate_id"`
		Signature   string `json:"signature"`
	}
	entries := make([]signatureEntry, 0, len(signature))
	for candidateID, value := range signature {
		entries = append(entries, signatureEntry{CandidateID: candidateID, Signature: value})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].CandidateID < entries[j].CandidateID
	})
	encoded, err := json.Marshal(entries)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func challengerBusinessSignature(raw []byte) (map[string]string, bool) {
	var root struct {
		DefenseCases []struct {
			CandidateID            string `json:"candidate_id"`
			DefenseVerdict         string `json:"defense_verdict"`
			MitigatingFactors      string `json:"mitigating_factors"`
			CounterEvidenceSnippet string `json:"counter_evidence_snippet"`
			DefenseArguments       []struct {
				Dimension string `json:"dimension"`
				Finding   string `json:"finding"`
				Evidence  []struct {
					EvidenceID string `json:"evidence_id"`
					Kind       string `json:"kind"`
					Path       string `json:"path"`
					LineRange  string `json:"line_range"`
					Snippet    string `json:"snippet"`
					ProbeID    string `json:"probe_id"`
					Reason     string `json:"reason"`
				} `json:"evidence"`
			} `json:"defense_arguments"`
		} `json:"defense_cases"`
	}
	if err := json.Unmarshal(cleanJSONOutput(raw), &root); err != nil {
		return nil, false
	}
	signature := make(map[string]string, len(root.DefenseCases))
	for _, defenseCase := range root.DefenseCases {
		var sb strings.Builder
		sb.WriteString(defenseCase.DefenseVerdict)
		sb.WriteString("\x1f")
		sb.WriteString(defenseCase.MitigatingFactors)
		sb.WriteString("\x1f")
		sb.WriteString(defenseCase.CounterEvidenceSnippet)
		for _, argument := range defenseCase.DefenseArguments {
			sb.WriteString("\x1e")
			sb.WriteString(argument.Dimension)
			sb.WriteString("\x1f")
			sb.WriteString(argument.Finding)
			for _, evidence := range argument.Evidence {
				sb.WriteString("\x1d")
				sb.WriteString(evidence.EvidenceID)
				sb.WriteString("\x1f")
				sb.WriteString(evidence.Kind)
				sb.WriteString("\x1f")
				sb.WriteString(evidence.Path)
				sb.WriteString("\x1f")
				sb.WriteString(evidence.LineRange)
				sb.WriteString("\x1f")
				sb.WriteString(evidence.Snippet)
				sb.WriteString("\x1f")
				sb.WriteString(evidence.ProbeID)
				sb.WriteString("\x1f")
				sb.WriteString(evidence.Reason)
			}
		}
		signature[defenseCase.CandidateID] = sb.String()
	}
	return signature, true
}

func compareArtifactBusinessSignature(before map[string]string, beforeKnown bool, after []byte) (drifted, unchecked bool, driftRef string) {
	afterSignature, afterKnown := challengerBusinessSignature(after)
	return compareBusinessSignatureMapsWithRef(before, beforeKnown, afterSignature, afterKnown)
}

func compareBusinessSignatureMaps(before map[string]string, beforeKnown bool, after map[string]string, afterKnown bool) (drifted, unchecked bool) {
	hasDrifted, hasUnchecked, _ := compareBusinessSignatureMapsWithRef(before, beforeKnown, after, afterKnown)
	return hasDrifted, hasUnchecked
}

func compareBusinessSignatureMapsWithRef(
	before map[string]string,
	beforeKnown bool,
	after map[string]string,
	afterKnown bool,
) (drifted, unchecked bool, driftRef string) {
	if !beforeKnown || !afterKnown {
		return false, true, ""
	}
	if len(before) != len(after) {
		return true, false, firstBusinessSignatureDifference(before, after)
	}
	for candidateID, signature := range before {
		if after[candidateID] != signature {
			return true, false, candidateID
		}
	}
	return false, false, ""
}

func firstBusinessSignatureDifference(before, after map[string]string) string {
	for candidateID, signature := range before {
		if after[candidateID] != signature {
			return candidateID
		}
	}
	for candidateID := range after {
		if _, exists := before[candidateID]; !exists {
			return candidateID
		}
	}
	return ""
}

func compareJudgeRepairArtifacts(
	beforeBusiness map[string]string,
	beforeBusinessKnown bool,
	beforeEvidence map[string]string,
	beforeEvidenceKnown bool,
	after []byte,
) (drifted, unchecked, evidenceDrifted bool, driftRef string) {
	afterBusiness, afterBusinessKnown := judgeBusinessSignature(after)
	afterEvidence, afterEvidenceKnown := judgeEvidenceSignature(after)
	if !afterBusinessKnown || !afterEvidenceKnown {
		return false, true, false, ""
	}

	businessDrifted, _, businessDriftRef := compareBusinessSignatureMapsWithRef(
		beforeBusiness, beforeBusinessKnown, afterBusiness, afterBusinessKnown,
	)
	evidenceDrifted, evidenceCompareErr, evidenceDriftRef := compareBusinessSignatureMapsWithRef(
		beforeEvidence, beforeEvidenceKnown, afterEvidence, afterEvidenceKnown,
	)
	if evidenceCompareErr && !beforeEvidenceKnown {
		return businessDrifted, true, false, businessDriftRef
	}
	if !beforeBusinessKnown || !beforeEvidenceKnown {
		driftRef = businessDriftRef
		if businessDriftRef == "" {
			driftRef = evidenceDriftRef
		}
		return businessDrifted || evidenceDrifted, true, evidenceDrifted, driftRef
	}
	driftRef = businessDriftRef
	if businessDriftRef == "" {
		driftRef = evidenceDriftRef
	}
	return businessDrifted || evidenceDrifted, false, evidenceDrifted, driftRef
}

func decodeJudgeBaseline(raw string) (*JudgeOutput, error) {
	var baseline JudgeOutput
	if err := json.Unmarshal(normalizeContractJSON(cleanJSONOutput([]byte(raw))), &baseline); err != nil {
		return nil, err
	}
	return &baseline, nil
}

func judgeBusinessSignature(raw []byte) (map[string]string, bool) {
	var root struct {
		FinalVerdicts []struct {
			CandidateID             string `json:"candidate_id"`
			Verdict                 string `json:"verdict"`
			SeverityPreliminary     string `json:"severity_preliminary"`
			Category                string `json:"category"`
			CategoryCode            string `json:"category_code"`
			ClassificationRationale string `json:"classification_rationale"`
			FilePath                string `json:"file_path"`
			LineRange               string `json:"line_range"`
			LineNumber              string `json:"line_number"`
			TriggerLine             string `json:"trigger_line"`
			ScopeSymbol             string `json:"scope_symbol"`
			Title                   string `json:"title"`
			JudgementRationale      string `json:"judgement_rationale"`
			CodeSnippet             string `json:"code_snippet"`
			Suggestion              string `json:"suggestion"`
		} `json:"final_verdicts"`
	}
	if err := json.Unmarshal(cleanJSONOutput(raw), &root); err != nil {
		return nil, false
	}
	signature := make(map[string]string, len(root.FinalVerdicts))
	for _, verdict := range root.FinalVerdicts {
		signature[verdict.CandidateID] = strings.Join([]string{
			verdict.Verdict, verdict.SeverityPreliminary, verdict.Category,
			verdict.CategoryCode, verdict.ClassificationRationale, verdict.FilePath,
			verdict.LineRange, verdict.LineNumber, verdict.TriggerLine,
			verdict.ScopeSymbol, verdict.Title, verdict.JudgementRationale,
			verdict.CodeSnippet, verdict.Suggestion,
		}, "\x1f")
	}
	if len(signature) == 0 {
		return nil, false
	}
	return signature, true
}

func judgeEvidenceSignature(raw []byte) (map[string]string, bool) {
	var root struct {
		FinalVerdicts []struct {
			CandidateID      string `json:"candidate_id"`
			EvidenceDegraded bool   `json:"evidence_degraded"`
			Evidence         []struct {
				EvidenceID string `json:"evidence_id"`
				Kind       string `json:"kind"`
				Path       string `json:"path"`
				LineRange  string `json:"line_range"`
				Snippet    string `json:"snippet"`
				ProbeID    string `json:"probe_id"`
				Reason     string `json:"reason"`
			} `json:"evidence"`
		} `json:"final_verdicts"`
	}
	if err := json.Unmarshal(cleanJSONOutput(raw), &root); err != nil {
		return nil, false
	}
	signature := make(map[string]string, len(root.FinalVerdicts))
	for _, verdict := range root.FinalVerdicts {
		var sb strings.Builder
		if verdict.EvidenceDegraded {
			sb.WriteString("degraded")
		}
		for _, evidence := range verdict.Evidence {
			sb.WriteString("\x1e")
			sb.WriteString(evidence.EvidenceID)
			sb.WriteString("\x1f")
			sb.WriteString(evidence.Kind)
			sb.WriteString("\x1f")
			sb.WriteString(evidence.Path)
			sb.WriteString("\x1f")
			sb.WriteString(evidence.LineRange)
			sb.WriteString("\x1f")
			sb.WriteString(evidence.Snippet)
			sb.WriteString("\x1f")
			sb.WriteString(evidence.ProbeID)
			sb.WriteString("\x1f")
			sb.WriteString(evidence.Reason)
		}
		signature[verdict.CandidateID] = sb.String()
	}
	if len(signature) == 0 {
		return nil, false
	}
	return signature, true
}
