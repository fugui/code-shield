package assessment

import (
	"context"
	"errors"
	"strings"
	"testing"

	"code-shield/models"
)

func contractForTest(t *testing.T) ArtifactContract {
	t.Helper()
	contract, err := ArtifactContractForOutput(AssessmentStage, OutputContract{
		SchemaID:          AssessmentsArtifactSchemaV2,
		TopLevel:          "assessments",
		RequiredFields:    []string{"schema", "assessments"},
		ForbiddenTopLevel: []string{"findings"},
		AllowedOutcomes: []string{
			string(OutcomePass), string(OutcomeDefect), string(OutcomeNotTarget), string(OutcomeNeedsHuman),
		},
	}, "")
	if err != nil {
		t.Fatalf("ArtifactContractForOutput() error = %v", err)
	}
	if contract.SchemaHash == "" || contract.Rendered == "" || len(contract.JSONSchema) == 0 {
		t.Fatal("artifact contract is incomplete")
	}
	return contract
}

func planForTest(t *testing.T, unitIDs ...string) PlanView {
	t.Helper()
	session, err := NewPromptRefSession("bundle-1", unitIDs, nil)
	if err != nil {
		t.Fatalf("NewPromptRefSession() error = %v", err)
	}
	return PlanView{BundleID: "bundle-1", PromptRefs: session, AllowedCategories: []string{"内存安全"}}
}

func passingValidator(t *testing.T) ArtifactValidator {
	t.Helper()
	return validatorFunc(func(_ PlanView, artifact AssessmentArtifact) (AssessmentResult, error) {
		for _, item := range artifact.Assessments {
			if strings.TrimSpace(item.Summary) == "" {
				return AssessmentResult{}, errors.New("summary is required")
			}
		}
		return AssessmentResult{}, nil
	})
}

type validatorFunc func(PlanView, AssessmentArtifact) (AssessmentResult, error)

func (fn validatorFunc) Validate(plan PlanView, artifact AssessmentArtifact) (AssessmentResult, error) {
	return fn(plan, artifact)
}

func validArtifactJSON() string {
	return `{"schema":"` + AssessmentsArtifactSchemaV2 + `","assessments":[{"unit_ref":"u001","outcome":"pass","summary":"ok"}]}`
}

func TestValidateArtifactAcceptsValidJSON(t *testing.T) {
	contract := contractForTest(t)
	result, err := ValidateArtifact([]byte(validArtifactJSON()), contract, planForTest(t, "unit-1"), passingValidator(t))
	if err != nil {
		t.Fatalf("ValidateArtifact() error = %v", err)
	}
	if len(result.Issues) != 0 {
		t.Fatalf("issues = %#v, want empty", result.Issues)
	}
	if result.Artifact.Assessments[0].PrimaryUnitID != "unit-1" {
		t.Fatalf("primary_unit_id = %q, want unit-1", result.Artifact.Assessments[0].PrimaryUnitID)
	}
}

func TestValidateArtifactCleansMarkdownAndExplanation(t *testing.T) {
	contract := contractForTest(t)
	raw := "prefix\n```json\n" + validArtifactJSON() + "\n```\nsuffix"
	result, err := ValidateArtifact([]byte(raw), contract, planForTest(t, "unit-1"), passingValidator(t))
	if err != nil {
		t.Fatalf("ValidateArtifact() error = %v", err)
	}
	if result.Artifact.Assessments[0].UnitRef != "u001" {
		t.Fatalf("artifact was not extracted from surrounding text")
	}
}

func TestPipelineRecordsCleanupAsLocalRepair(t *testing.T) {
	contract := contractForTest(t)
	raw := "prefix\n```json\n" + validArtifactJSON() + "\n```\nsuffix"
	_, metrics, err := ValidateAndRepairArtifact(context.Background(), []byte(raw), contract, planForTest(t, "unit-1"), passingValidator(t), ArtifactPipelineOptions{})
	if err != nil {
		t.Fatalf("ValidateAndRepairArtifact() error = %v", err)
	}
	if metrics.LocalRepairs != 1 {
		t.Fatalf("local repairs = %d, want 1", metrics.LocalRepairs)
	}
}

func TestPipelineUsesDirectedRepairForJSONParseFailure(t *testing.T) {
	contract := contractForTest(t)
	plan := planForTest(t, "unit-1")
	calls := 0
	_, metrics, err := ValidateAndRepairArtifact(context.Background(), []byte("{not json"), contract, plan, passingValidator(t),
		ArtifactPipelineOptions{MaxLLMAttempts: 2, Repair: func(_ context.Context, request DirectedRepairRequest) (string, int64, error) {
			calls++
			if !strings.Contains(request.RawArtifact, "{not json") {
				t.Fatalf("repair request did not contain raw artifact: %q", request.RawArtifact)
			}
			if request.Contract.SchemaID != contract.SchemaID || len(request.Issues) == 0 {
				t.Fatal("repair request is missing contract or issues")
			}
			return validArtifactJSON(), 10, nil
		}})
	if err != nil {
		if !errors.Is(err, ErrArtifactInvalid) {
			t.Fatalf("ValidateAndRepairArtifact() error = %v, want ErrArtifactInvalid", err)
		}
	}
	if calls != 1 || metrics.LLMRepairAttempts != 1 || metrics.LLMRepairSuccesses != 0 {
		t.Fatalf("repair calls=%d attempts=%d successes=%d", calls, metrics.LLMRepairAttempts, metrics.LLMRepairSuccesses)
	}
	if metrics.RepairTokens != 10 || metrics.FinalStatus != "failed" || metrics.OriginalArtifactHash == "" {
		t.Fatalf("metrics = %#v, want tokens, success, and original hash", metrics)
	}
	if !metrics.RepairDriftUnchecked {
		t.Fatal("parse failure should be audited as drift unchecked")
	}
	if len(metrics.RepairAttempts) != 1 || metrics.RepairAttempts[0].Accepted || metrics.RepairAttempts[0].Raw == "" ||
		metrics.RepairAttempts[0].Error == "" {
		t.Fatalf("repair attempts = %#v, want a rejected raw artifact with drift-unchecked error", metrics.RepairAttempts)
	}
}

func TestPipelineDoesNotRepairEmptyArtifact(t *testing.T) {
	contract := contractForTest(t)
	plan := planForTest(t, "unit-1")
	calls := 0
	_, metrics, err := ValidateAndRepairArtifact(context.Background(), []byte("   "), contract, plan, passingValidator(t),
		ArtifactPipelineOptions{MaxLLMAttempts: 2, Repair: func(_ context.Context, _ DirectedRepairRequest) (string, int64, error) {
			calls++
			return validArtifactJSON(), 1, nil
		}})
	if !errors.Is(err, ErrArtifactEmpty) {
		t.Fatalf("error = %v, want ErrArtifactEmpty", err)
	}
	if calls != 0 || len(metrics.RepairAttempts) != 1 || !metrics.RepairAttempts[0].NonRetryable {
		t.Fatalf("calls=%d attempts=%#v, want no LLM repair", calls, metrics.RepairAttempts)
	}
	if metrics.FinalStatus != "failed" {
		t.Fatalf("final status = %q, want failed", metrics.FinalStatus)
	}
}

func TestPipelineStopsOnNonRetryableRepairError(t *testing.T) {
	contract := contractForTest(t)
	plan := planForTest(t, "unit-1")
	calls := 0
	_, metrics, err := ValidateAndRepairArtifact(context.Background(), []byte("{broken"), contract, plan, passingValidator(t),
		ArtifactPipelineOptions{MaxLLMAttempts: 2, Repair: func(_ context.Context, _ DirectedRepairRequest) (string, int64, error) {
			calls++
			return "", 0, &NonRetryableRepairError{Err: errors.New("content filtered")}
		}})
	if !errors.Is(err, ErrArtifactInvalid) {
		t.Fatalf("error = %v, want ErrArtifactInvalid", err)
	}
	if calls != 1 || len(metrics.RepairAttempts) != 1 || !metrics.RepairAttempts[0].NonRetryable {
		t.Fatalf("calls=%d attempts=%#v, want one non-retryable attempt", calls, metrics.RepairAttempts)
	}
}

func TestLocalRepairsSchemaV1EvidenceAndSingleUnitRef(t *testing.T) {
	contract := contractForTest(t)
	plan := planForTest(t, "unit-1")
	raw := `{"schema":"code-shield.unit-assessments.v1","assessments":[{"unit_ref":"","outcome":"pass","summary":"ok","evidence":"raw text"}]}`
	result, err := ValidateArtifact([]byte(raw), contract, plan, passingValidator(t))
	if err == nil {
		t.Fatal("first validation should fail")
	}
	repaired, changed := NormalizeArtifactLocally(result.CleanedJSON, contract, plan.PromptRefs)
	if !changed {
		t.Fatal("local repair was not applied")
	}
	final, err := ValidateArtifact(repaired, contract, plan, passingValidator(t))
	if err != nil {
		t.Fatalf("repaired validation error = %v; issues=%#v repaired=%s", err, final.Issues, repaired)
	}
	item := final.Artifact.Assessments[0]
	if item.UnitRef != "u001" || item.Evidence["raw"] != "raw text" {
		t.Fatalf("local normalization failed: %#v", item)
	}
}

func TestLocalRepairsIssueEvidence(t *testing.T) {
	contract := contractForTest(t)
	plan := planForTest(t, "unit-1")
	raw := `{"schema":"` + AssessmentsArtifactSchemaV2 + `","assessments":[{"unit_ref":"u001","outcome":"defect","summary":"bad","issues":[{"category":"内存安全","severity":"严重","detail":"bad","evidence":"raw issue evidence"}]}]}`
	result, err := ValidateArtifact([]byte(raw), contract, plan, passingValidator(t))
	if err == nil {
		t.Fatal("first validation should fail")
	}
	repaired, changed := NormalizeArtifactLocally(result.CleanedJSON, contract, plan.PromptRefs)
	if !changed {
		t.Fatal("issue evidence was not normalized")
	}
	final, err := ValidateArtifact(repaired, contract, plan, passingValidator(t))
	if err != nil {
		t.Fatalf("repaired validation error = %v; issues=%#v", err, final.Issues)
	}
	if got := final.Artifact.Assessments[0].Issues[0].Evidence["raw"]; got != "raw issue evidence" {
		t.Fatalf("issue evidence = %#v", got)
	}
}

func TestPipelineLocalRepairRecordsMetric(t *testing.T) {
	contract := contractForTest(t)
	plan := planForTest(t, "unit-1")
	raw := `{"schema":"code-shield.unit-assessments.v1","assessments":[{"unit_ref":"u001","outcome":"pass","summary":"ok"}]}`
	_, metrics, err := ValidateAndRepairArtifact(context.Background(), []byte(raw), contract, plan, passingValidator(t), ArtifactPipelineOptions{})
	if err != nil {
		t.Fatalf("ValidateAndRepairArtifact() error = %v", err)
	}
	if metrics.LocalRepairs != 1 || metrics.LLMRepairAttempts != 0 {
		t.Fatalf("metrics = %#v, want local repair only", metrics)
	}
	if len(metrics.RepairAttempts) != 1 || metrics.RepairAttempts[0].Source != "local" ||
		!metrics.RepairAttempts[0].Accepted || !strings.Contains(metrics.RepairAttempts[0].Raw, AssessmentsArtifactSchemaV2) {
		t.Fatalf("repair attempts = %#v, want an accepted local attempt artifact", metrics.RepairAttempts)
	}
}

func TestPipelineRecordsObservationForValidArtifact(t *testing.T) {
	contract := contractForTest(t)
	_, metrics, err := ValidateAndRepairArtifact(context.Background(), []byte(validArtifactJSON()), contract,
		planForTest(t, "unit-1"), passingValidator(t), ArtifactPipelineOptions{
			Observation: ArtifactPipelineObservation{
				Driver:             "native",
				ResourceID:         "resource-1",
				ResponseFormatMode: "auto",
			},
		})
	if err != nil {
		t.Fatalf("ValidateAndRepairArtifact() error = %v", err)
	}
	if metrics.SchemaID != contract.SchemaID || metrics.SchemaHash != contract.SchemaHash {
		t.Fatalf("schema metrics = %#v", metrics)
	}
	if metrics.Driver != "native" || metrics.ResourceID != "resource-1" || metrics.ResponseFormatMode != "auto" {
		t.Fatalf("observation metrics = %#v", metrics)
	}
}

func TestPipelineSendsMultiUnitUnknownRefToLLM(t *testing.T) {
	contract := contractForTest(t)
	plan := planForTest(t, "unit-1", "unit-2")
	raw := `{"schema":"` + AssessmentsArtifactSchemaV2 + `","assessments":[{"unit_ref":"u001","outcome":"pass","summary":"ok"},{"unit_ref":"bad","outcome":"pass","summary":"ok"}]}`
	calls := 0
	_, metrics, err := ValidateAndRepairArtifact(context.Background(), []byte(raw), contract, plan, passingValidator(t),
		ArtifactPipelineOptions{MaxLLMAttempts: 1, Repair: func(_ context.Context, request DirectedRepairRequest) (string, int64, error) {
			calls++
			var allowed []string
			for _, ref := range request.ValidRefs {
				allowed = append(allowed, ref.Ref)
			}
			if len(allowed) != 2 {
				t.Fatalf("valid refs = %v", allowed)
			}
			return `{"schema":"` + AssessmentsArtifactSchemaV2 + `","assessments":[{"unit_ref":"u001","outcome":"pass","summary":"ok"},{"unit_ref":"u002","outcome":"pass","summary":"ok"}]}`, 5, nil
		}})
	if err != nil {
		t.Fatalf("ValidateAndRepairArtifact() error = %v", err)
	}
	if calls != 1 || metrics.LLMRepairSuccesses != 1 {
		t.Fatalf("repair calls=%d metrics=%#v", calls, metrics)
	}
	if len(metrics.RepairAttempts) != 1 || metrics.RepairAttempts[0].Source != "llm" ||
		!metrics.RepairAttempts[0].Accepted || !strings.Contains(metrics.RepairAttempts[0].Raw, "u002") {
		t.Fatalf("repair attempts = %#v, want accepted raw LLM artifact", metrics.RepairAttempts)
	}
}

func TestPipelineRejectsLLMRepairBusinessDrift(t *testing.T) {
	contract := contractForTest(t)
	plan := planForTest(t, "unit-1")
	raw := `{"schema":"` + AssessmentsArtifactSchemaV2 + `","assessments":[{"unit_ref":"bad","outcome":"defect","summary":"original","issues":[{"category":"内存安全","severity":"严重","detail":"bad"}]}]}`
	_, metrics, err := ValidateAndRepairArtifact(context.Background(), []byte(raw), contract, plan, passingValidator(t),
		ArtifactPipelineOptions{MaxLLMAttempts: 2, Repair: func(_ context.Context, _ DirectedRepairRequest) (string, int64, error) {
			return validArtifactJSON(), 3, nil
		}})
	if !errors.Is(err, ErrArtifactInvalid) {
		t.Fatalf("error = %v, want ErrArtifactInvalid", err)
	}
	if metrics.LLMRepairAttempts != 2 || !metrics.RepairDrifted || metrics.RepairDriftUnitRef == "" {
		t.Fatalf("metrics = %#v, want two drifted attempts", metrics)
	}
	for _, attempt := range metrics.RepairAttempts {
		if attempt.Source != "llm" || attempt.Accepted || !attempt.Drifted || attempt.Tokens != 3 ||
			!strings.Contains(attempt.Raw, "pass") {
			t.Fatalf("attempt = %#v, want a rejected drifted raw artifact", attempt)
		}
	}
}

func TestPipelineExplicitlyFailsAfterTwoRepairFailures(t *testing.T) {
	contract := contractForTest(t)
	plan := planForTest(t, "unit-1")
	attempts := 0
	_, metrics, err := ValidateAndRepairArtifact(context.Background(), []byte("{broken"), contract, plan, passingValidator(t),
		ArtifactPipelineOptions{MaxLLMAttempts: 2, Repair: func(_ context.Context, _ DirectedRepairRequest) (string, int64, error) {
			attempts++
			return "{broken", 1, nil
		}})
	if !errors.Is(err, ErrArtifactInvalid) {
		t.Fatalf("error = %v, want ErrArtifactInvalid", err)
	}
	if attempts != 2 || metrics.LLMRepairAttempts != 2 || metrics.LLMRepairSuccesses != 0 {
		t.Fatalf("attempts=%d metrics=%#v", attempts, metrics)
	}
	for index, attempt := range metrics.RepairAttempts {
		if attempt.Attempt != index+1 || attempt.Raw != "{broken" || attempt.Tokens != 1 || attempt.Accepted || attempt.Error == "" {
			t.Fatalf("attempt[%d] = %#v, want retained failed raw artifact", index, attempt)
		}
	}
}

func TestAssessmentContractRepairDefaultDisabled(t *testing.T) {
	if (models.Config{}).AssessmentContractRepairEnabled() {
		t.Fatal("assessment contract repair should default to disabled")
	}
}
