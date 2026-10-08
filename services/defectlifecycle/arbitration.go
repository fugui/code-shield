package defectlifecycle

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"code-shield/models"
	"code-shield/services/invoker"
)

type AIInvokingArbitrator struct {
	invoker        invoker.AIInvoker
	work           invoker.LLMWorkContext
	contextLines   int
	timeoutSeconds int
}

func NewAIInvokingArbitrator(inv invoker.AIInvoker, reportID uint, repoName, taskType string) *AIInvokingArbitrator {
	return &AIInvokingArbitrator{
		invoker: inv,
		work: invoker.LLMWorkContext{
			ReportID: reportID, RepoName: repoName, TaskType: taskType,
			Stage: "问题归并: AI仲裁", SubTask: "灰色带候选比较", TierName: "system_tool",
		},
		contextLines:   models.AppConfig.Arbitration.ContextLines,
		timeoutSeconds: models.AppConfig.Arbitration.TimeoutSeconds,
	}
}

func NewRuntimeArbitrator(reportID uint, repoName, taskType string) ArbitrationProvider {
	if !models.AppConfig.ArbitrationEnabled() {
		return nil
	}
	inv, ok := invoker.GetRawInvoker("native")
	if !ok || inv == nil {
		return nil
	}
	return NewAIInvokingArbitrator(inv, reportID, repoName, taskType)
}

func (a *AIInvokingArbitrator) Arbitrate(request ArbitrationRequest) ([]ArbitrationDecision, error) {
	if a == nil || a.invoker == nil || len(request.Candidates) == 0 {
		return nil, fmt.Errorf("arbitration invoker or candidates missing")
	}
	var prompt strings.Builder
	prompt.WriteString("You arbitrate whether an unresolved code finding is the same persisted defect. Compare stable code identity, not prose. Return JSON only.\n")
	prompt.WriteString(fmt.Sprintf("Observation: file=%s lines=%d-%d scope=%s statement=%s title=%s\n",
		request.Observation.Identity.NormPath, request.Observation.Identity.LineStart, request.Observation.Identity.LineEnd,
		request.Observation.Identity.ScopeKey, request.Observation.Identity.CleanToken, request.Observation.Representative.Title))
	for index, candidate := range request.Candidates {
		defect := candidate.Defect
		prompt.WriteString(fmt.Sprintf("Candidate %d: id=%d score=%.4f file=%s lines=%d-%d scope=%s statement=%s\n",
			index+1, defect.ID, candidate.Score, defect.NormPath, derefInt(defect.LineStart), derefInt(defect.LineEnd),
			defect.ScopeKey, defect.CleanToken))
	}
	prompt.WriteString(`{"decisions":[{"candidate_id":123,"decision":"SAME|DIFFERENT|REVIEW","confidence":0.0,"reason":"stable identity evidence"}]}`)

	temp, err := os.CreateTemp("", "defect-arbitration-*.json")
	if err != nil {
		return nil, err
	}
	outputPath := temp.Name()
	_ = temp.Close()
	defer os.Remove(outputPath)
	req := invoker.AIRequest{
		PromptMsg:               prompt.String(),
		OutputPath:              outputPath,
		TimeoutMin:              1,
		AttemptTimeoutSeconds:   a.timeoutSeconds,
		FirstByteTimeoutSeconds: a.timeoutSeconds,
		IdleTimeoutSeconds:      a.timeoutSeconds,
		MaxOutputBytes:          64 * 1024,
		ResponseFormat:          "json",
		WorkContext:             &a.work,
	}
	if err := a.invoker.Invoke(req); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Decisions []ArbitrationDecision `json:"decisions"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	return payload.Decisions, nil
}
