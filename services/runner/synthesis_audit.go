package runner

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"code-shield/services/engines/debate"
)

var (
	synthesisStatsPattern  = regexp.MustCompile(`致命：(\d+)，严重：(\d+)，一般：(\d+)，建议：(\d+)`)
	synthesisDetailPattern = regexp.MustCompile(`(?m)^###\s+\d+\.\s+(.+?)\s*$\n+^- \*\*文件\*\*: (.+?)\s*$`)
)

func readSynthesisAuditBaseline(path string) []byte {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return raw
}

func newSynthesisAuditMetrics(baseline []byte) *debate.ArtifactRepairMetrics {
	return debate.NewSynthesisArtifactRepairMetrics(baseline)
}

func addSynthesisInvocationAttempt(
	audit *debate.ArtifactRepairMetrics,
	raw string,
	tokens int64,
	invocationErr error,
) {
	audit.AddLLMRepair(raw, tokens, false, false, false, invocationErr)
}

func synthesisBusinessSignature(raw []byte) (map[string]string, bool) {
	text := string(raw)
	stats := synthesisStatsPattern.FindStringSubmatch(text)
	if stats == nil {
		return nil, false
	}
	signature := map[string]string{
		"stats": strings.Join(stats[1:], "\x1f"),
	}
	for index, match := range synthesisDetailPattern.FindAllStringSubmatch(text, -1) {
		signature[fmt.Sprintf("detail-%d", index)] = strings.Join(match[1:], "\x1f")
	}
	return signature, true
}

func compareSynthesisBusinessSignatures(before map[string]string, after map[string]string) (bool, string) {
	if len(before) != len(after) {
		return true, firstSynthesisSignatureDifference(before, after)
	}
	for key, value := range before {
		if after[key] != value {
			return true, key
		}
	}
	return false, ""
}

func firstSynthesisSignatureDifference(before, after map[string]string) string {
	for key, value := range before {
		if after[key] != value {
			return key
		}
	}
	for key := range after {
		if _, exists := before[key]; !exists {
			return key
		}
	}
	return ""
}

func acceptLastSynthesisAttempt(
	audit *debate.ArtifactRepairMetrics,
	baseline map[string]string,
	baselineKnown bool,
	accepted bool,
	validationErr error,
) {
	if audit == nil || len(audit.RepairAttempts) == 0 {
		return
	}
	attemptIndex := len(audit.RepairAttempts) - 1
	attempt := &audit.RepairAttempts[attemptIndex]
	attempt.Accepted = accepted
	attempt.NonRetryable = !accepted
	attempt.Error = errorText(validationErr)
	if !accepted {
		return
	}
	audit.LLMRepairSuccesses++
}

func evaluateSynthesisAttemptDrift(
	audit *debate.ArtifactRepairMetrics,
	baseline map[string]string,
	baselineKnown bool,
	artifact []byte,
) {
	if audit == nil || len(audit.RepairAttempts) == 0 {
		return
	}
	attemptIndex := len(audit.RepairAttempts) - 1
	attempt := &audit.RepairAttempts[attemptIndex]
	attemptSignature, attemptKnown := synthesisBusinessSignature(artifact)
	if !baselineKnown || !attemptKnown {
		audit.RepairDriftUnchecked = true
		attempt.Drifted = false
		return
	}
	drifted, driftRef := compareSynthesisBusinessSignatures(baseline, attemptSignature)
	attempt.Drifted = drifted
	audit.RepairDriftUnitRef = driftRef
	audit.RepairDrifted = audit.RepairDrifted || drifted
}

func addSynthesisAuditIssue(
	audit *debate.ArtifactRepairMetrics,
	field string,
	message string,
	recoverable bool,
) {
	audit.AddIssue(debate.ArtifactIssue{
		Stage:       "synthesis",
		Schema:      debate.SynthesisReportArtifactSchema,
		JSONPath:    "$",
		Field:       field,
		Code:        "ARTIFACT_ISSUE_UNRESOLVED",
		Message:     message,
		Recoverable: recoverable,
	})
}

func persistSynthesisRepairAudit(
	ctx context.Context,
	reportID, repoID, taskTypeID uint,
	audit *debate.ArtifactRepairMetrics,
) {
	debate.PersistArtifactRepairAudit(
		ctx,
		fmt.Sprintf("report-%d/synthesis", reportID),
		reportID,
		repoID,
		taskTypeID,
		audit,
	)
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
