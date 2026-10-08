package assessment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
)

type DirectedRepairRequest struct {
	RawArtifact  string
	Contract     ArtifactContract
	Issues       []ArtifactIssue
	ValidRefs    []PromptRef
	AllowedEnums map[string][]string
}

type DirectedRepairFunc func(ctx context.Context, request DirectedRepairRequest) (string, int64, error)

type ArtifactRepairAttempt struct {
	Attempt      int    `json:"attempt"`
	Source       string `json:"source"`
	Raw          string `json:"raw,omitempty"`
	Tokens       int64  `json:"tokens,omitempty"`
	Error        string `json:"error,omitempty"`
	Accepted     bool   `json:"accepted"`
	Drifted      bool   `json:"drifted,omitempty"`
	NonRetryable bool   `json:"non_retryable,omitempty"`
}

type ArtifactRepairMetrics struct {
	OriginalArtifactHash string                  `json:"original_artifact_hash,omitempty"`
	Driver               string                  `json:"driver,omitempty"`
	ResourceID           string                  `json:"resource_id,omitempty"`
	ResponseFormatMode   string                  `json:"response_format_mode,omitempty"`
	RepairTokens         int64                   `json:"repair_tokens"`
	FinalStatus          string                  `json:"final_status"`
	SchemaID             string                  `json:"schema_id"`
	SchemaHash           string                  `json:"schema_hash"`
	LocalRepairs         int                     `json:"local_repairs"`
	LLMRepairs           int                     `json:"llm_repairs"`
	LLMRepairAttempts    int                     `json:"llm_repair_attempts"`
	LLMRepairSuccesses   int                     `json:"llm_repair_successes"`
	SchemaRepairIssues   []ArtifactIssue         `json:"schema_repair_issues"`
	RepairDrifted        bool                    `json:"repair_drifted"`
	RepairDriftUnchecked bool                    `json:"repair_drift_unchecked"`
	RepairDriftUnitRef   string                  `json:"repair_drift_unit_ref,omitempty"`
	RepairAttempts       []ArtifactRepairAttempt `json:"repair_attempts,omitempty"`
	FinalArtifact        AssessmentArtifact      `json:"-"`
}

var ErrArtifactEmpty = errors.New("assessment artifact is empty")

type NonRetryableRepairError struct {
	Err error
}

func (err *NonRetryableRepairError) Error() string {
	if err == nil || err.Err == nil {
		return "repair is not retryable"
	}
	return err.Err.Error()
}

func (err *NonRetryableRepairError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}

type ArtifactPipelineOptions struct {
	MaxLLMAttempts int
	Repair         DirectedRepairFunc
	Observation    ArtifactPipelineObservation
}

type ArtifactPipelineObservation struct {
	Driver                  string
	ResourceID              string
	ResponseFormatMode      string
	ResponseFormatFallbacks int
}

func ValidateAndRepairArtifact(
	ctx context.Context,
	raw []byte,
	contract ArtifactContract,
	plan PlanView,
	validator ArtifactValidator,
	options ArtifactPipelineOptions,
) (ArtifactValidationResult, ArtifactRepairMetrics, error) {
	metrics := ArtifactRepairMetrics{
		SchemaID: contract.SchemaID, SchemaHash: contract.SchemaHash,
		OriginalArtifactHash: contentHash(raw), FinalStatus: "failed",
		Driver:             options.Observation.Driver,
		ResourceID:         options.Observation.ResourceID,
		ResponseFormatMode: options.Observation.ResponseFormatMode,
	}
	result, validateErr := ValidateArtifact(raw, contract, plan, validator)
	if validateErr == nil {
		metrics.LocalRepairs += localRepairsCount(result)
		if result.LocalRepaired {
			metrics.RepairAttempts = append(metrics.RepairAttempts, ArtifactRepairAttempt{
				Attempt: 1, Source: "local_cleanup", Raw: string(result.CleanedJSON), Accepted: true,
			})
		}
		metrics.FinalStatus = "success"
		logArtifactRepairMetrics(metrics, plan, false)
		return result, metrics, nil
	}
	if errors.Is(validateErr, ErrArtifactEmpty) {
		metrics.SchemaRepairIssues = append(metrics.SchemaRepairIssues, ArtifactIssue{
			Stage: contract.Stage, Schema: contract.SchemaID, JSONPath: "$", Field: "artifact",
			Code: "ARTIFACT_EMPTY", Message: "empty artifacts are not eligible for schema repair", Recoverable: false,
		})
		metrics.RepairAttempts = append(metrics.RepairAttempts, ArtifactRepairAttempt{
			Attempt: 0, Source: "empty_artifact", Error: validateErr.Error(), NonRetryable: true,
		})
		metrics.FinalStatus = "failed"
		logArtifactRepairMetrics(metrics, plan, true)
		return result, metrics, validateErr
	}
	metrics.LocalRepairs += localRepairsCount(result)
	if result.LocalRepaired {
		metrics.RepairAttempts = append(metrics.RepairAttempts, ArtifactRepairAttempt{
			Attempt: len(metrics.RepairAttempts) + 1, Source: "local_cleanup", Raw: string(result.CleanedJSON),
			Error: validateErr.Error(),
		})
	}
	metrics.SchemaRepairIssues = append(metrics.SchemaRepairIssues, result.Issues...)

	if repaired, changed := NormalizeArtifactLocally(raw, contract, plan.PromptRefs); changed {
		repairedResult, repairedErr := ValidateArtifact(repaired, contract, plan, validator)
		if repairedErr == nil {
			metrics.LocalRepairs++
			metrics.RepairAttempts = append(metrics.RepairAttempts, ArtifactRepairAttempt{
				Attempt: len(metrics.RepairAttempts) + 1, Source: "local", Raw: string(repaired), Accepted: true,
			})
			metrics.FinalArtifact = repairedResult.Artifact
			metrics.FinalStatus = "success"
			logArtifactRepairMetrics(metrics, plan, true)
			return repairedResult, metrics, nil
		}
		metrics.LocalRepairs++
		metrics.RepairAttempts = append(metrics.RepairAttempts, ArtifactRepairAttempt{
			Attempt: len(metrics.RepairAttempts) + 1, Source: "local", Raw: string(repaired),
			Error: repairedErr.Error(),
		})
		result = repairedResult
		metrics.SchemaRepairIssues = append(metrics.SchemaRepairIssues, result.Issues...)
	}

	baseline := baselineArtifact(result.CleanedJSON)
	for attempt := 0; attempt < maxRepairAttempts(options.MaxLLMAttempts); attempt++ {
		if options.Repair == nil {
			break
		}
		select {
		case <-ctx.Done():
			metrics.SchemaRepairIssues = append(metrics.SchemaRepairIssues, ArtifactIssue{
				Stage: contract.Stage, Schema: contract.SchemaID, JSONPath: "$", Field: "repair",
				Code: "REPAIR_CANCELLED", Message: ctx.Err().Error(), Recoverable: false,
			})
			metrics.FinalStatus = "failed"
			return result, metrics, ErrArtifactInvalid
		default:
		}

		metrics.LLMRepairAttempts++
		metrics.LLMRepairs++
		repairAttempt := ArtifactRepairAttempt{Attempt: len(metrics.RepairAttempts) + 1, Source: "llm"}
		repairedRaw, repairTokens, repairErr := options.Repair(ctx, DirectedRepairRequest{
			RawArtifact: strings.TrimSpace(string(result.CleanedJSON)),
			Contract:    contract,
			Issues:      result.Issues,
			ValidRefs:   plan.PromptRefs.Refs,
			AllowedEnums: map[string][]string{
				"outcome":           allowedOutcomes(contract),
				"issues[].category": plan.AllowedCategories,
			},
		})
		if repairErr != nil {
			repairAttempt.Error = repairErr.Error()
			repairAttempt.NonRetryable = isNonRetryableRepairError(repairErr) ||
				errors.Is(repairErr, context.Canceled) || errors.Is(repairErr, context.DeadlineExceeded)
			metrics.RepairAttempts = append(metrics.RepairAttempts, repairAttempt)
			result.Issues = append(result.Issues, ArtifactIssue{
				Stage: contract.Stage, Schema: contract.SchemaID, JSONPath: "$", Field: "repair",
				Code: "LLM_REPAIR_FAILED", Message: fmt.Sprintf("attempt %d failed: %v", repairAttempt.Attempt, repairErr), Recoverable: true,
			})
			if repairAttempt.NonRetryable {
				metrics.FinalStatus = "failed"
				logArtifactRepairMetrics(metrics, plan, true)
				return result, metrics, ErrArtifactInvalid
			}
			continue
		}
		if strings.TrimSpace(repairedRaw) == "" {
			repairAttempt.Error = "repair returned an empty artifact"
			repairAttempt.NonRetryable = true
			metrics.RepairAttempts = append(metrics.RepairAttempts, repairAttempt)
			result.Issues = append(result.Issues, ArtifactIssue{
				Stage: contract.Stage, Schema: contract.SchemaID, JSONPath: "$", Field: "repair",
				Code: "LLM_REPAIR_EMPTY", Message: "repair returned an empty artifact", Recoverable: false,
			})
			metrics.FinalStatus = "failed"
			logArtifactRepairMetrics(metrics, plan, true)
			return result, metrics, ErrArtifactInvalid
		}
		repairAttempt.Raw = repairedRaw
		repairAttempt.Tokens = repairTokens
		metrics.RepairTokens += repairTokens
		metrics.SchemaRepairIssues = append(metrics.SchemaRepairIssues, ArtifactIssue{
			Stage: contract.Stage, Schema: contract.SchemaID, JSONPath: "$", Field: "repair",
			Code: "LLM_REPAIR_ATTEMPTED", Message: fmt.Sprintf("attempt %d consumed about %d tokens", repairAttempt.Attempt, repairTokens), Recoverable: true,
		})

		repairedResult, repairedErr := ValidateArtifact([]byte(repairedRaw), contract, plan, validator)
		if repairedErr != nil {
			repairAttempt.Error = repairedErr.Error()
			metrics.RepairAttempts = append(metrics.RepairAttempts, repairAttempt)
			result = repairedResult
			result.Issues = append(result.Issues, ArtifactIssue{
				Stage: contract.Stage, Schema: contract.SchemaID, JSONPath: "$", Field: "repair",
				Code: "LLM_REPAIR_REJECTED", Message: fmt.Sprintf("attempt %d failed validation", repairAttempt.Attempt), Recoverable: true,
			})
			continue
		}
		if baseline == nil {
			metrics.RepairDriftUnchecked = true
			repairAttempt.Error = "business drift cannot be verified because no baseline artifact is available"
			metrics.RepairAttempts = append(metrics.RepairAttempts, repairAttempt)
			result = repairedResult
			result.Issues = append(result.Issues, ArtifactIssue{
				Stage: contract.Stage, Schema: contract.SchemaID, JSONPath: "$.assessments", Field: "assessment",
				Code:        "REPAIR_BUSINESS_DRIFT_UNCHECKED",
				Message:     "repair cannot be accepted without a comparable baseline artifact",
				Recoverable: false,
			})
			metrics.FinalStatus = "failed"
			logArtifactRepairMetrics(metrics, plan, true)
			return result, metrics, ErrArtifactInvalid
		}
		unchanged, driftRef := ArtifactUnitsUnchanged(baseline, &repairedResult.Artifact)
		if !unchanged {
			metrics.RepairDrifted = true
			metrics.RepairDriftUnitRef = driftRef
			repairAttempt.Drifted = true
			metrics.RepairAttempts = append(metrics.RepairAttempts, repairAttempt)
			result = repairedResult
			result.Issues = append(result.Issues, ArtifactIssue{
				Stage: contract.Stage, Schema: contract.SchemaID, JSONPath: "$.assessments", Field: "assessment",
				Code: "REPAIR_BUSINESS_DRIFT", Message: fmt.Sprintf("repair changed business semantics for %q", driftRef), Recoverable: false,
			})
			continue
		}
		metrics.LLMRepairSuccesses++
		metrics.RepairDriftUnchecked = baseline == nil
		metrics.SchemaRepairIssues = append(metrics.SchemaRepairIssues, repairedResult.Issues...)
		metrics.FinalArtifact = repairedResult.Artifact
		repairAttempt.Accepted = true
		metrics.FinalStatus = "success"
		metrics.RepairAttempts = append(metrics.RepairAttempts, repairAttempt)
		logArtifactRepairMetrics(metrics, plan, true)
		return repairedResult, metrics, nil
	}

	logArtifactRepairMetrics(metrics, plan, true)
	metrics.FinalStatus = "failed"
	return result, metrics, ErrArtifactInvalid
}

func isNonRetryableRepairError(err error) bool {
	var nonRetryable *NonRetryableRepairError
	return errors.As(err, &nonRetryable)
}

func localRepairsCount(result ArtifactValidationResult) int {
	if result.LocalRepaired {
		return 1
	}
	return 0
}

func maxRepairAttempts(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func baselineArtifact(raw []byte) *AssessmentArtifact {
	var artifact AssessmentArtifact
	if err := json.Unmarshal(raw, &artifact); err != nil || len(artifact.Assessments) == 0 {
		return nil
	}
	return &artifact
}

func allowedOutcomes(contract ArtifactContract) []string {
	root := contract.JSONSchema["properties"].(map[string]any)
	assessments := root["assessments"].(map[string]any)
	items := assessments["items"].(map[string]any)
	properties := items["properties"].(map[string]any)
	outcome := properties["outcome"].(map[string]any)
	return append([]string(nil), outcome["enum"].([]string)...)
}

func logArtifactRepairMetrics(metrics ArtifactRepairMetrics, plan PlanView, repaired bool) {
	if !repaired && metrics.LocalRepairs == 0 && metrics.LLMRepairAttempts == 0 {
		return
	}
	issueCodes := make([]string, 0, len(metrics.SchemaRepairIssues))
	for _, issue := range metrics.SchemaRepairIssues {
		issueCodes = append(issueCodes, issue.Code)
	}
	log.Printf("[AssessmentContract] repair metrics bundle=%s schema=%s status=%s local=%d llm=%d/%d attempts=%d drifted=%t drift_unchecked=%t issues=%s",
		plan.BundleID, metrics.SchemaID, metrics.FinalStatus, metrics.LocalRepairs,
		metrics.LLMRepairSuccesses, metrics.LLMRepairAttempts, len(metrics.RepairAttempts),
		metrics.RepairDrifted, metrics.RepairDriftUnchecked, strings.Join(issueCodes, ","))
}

func contentHash(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	digest := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(digest[:])
}
