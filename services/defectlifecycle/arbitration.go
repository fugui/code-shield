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
	prompt.WriteString("You arbitrate whether an unresolved code finding is the same persisted defect.\n")
	prompt.WriteString("Compare stable code root causes, variable targets, and locations. Minor phrasing differences, slight line shifts, or fine-grained category drift (e.g. boundary handling vs logic bug) in the same code context should be judged as SAME. Return JSON only.\n\n")

	obs := request.Observation
	obsScope := obs.Identity.SymbolPath
	if obsScope == "" {
		obsScope = obs.Representative.ScopeSymbol
	}
	if obsScope == "" {
		obsScope = obs.Identity.ScopeKey
	}
	obsSnippet := strings.TrimSpace(obs.Representative.CodeSnippet)
	if obsSnippet == "" {
		obsSnippet = obs.Identity.CleanToken
	}

	prompt.WriteString(fmt.Sprintf("Observation:\n- File: %s\n- Lines: %d-%d\n- Scope: %s\n- Category: %s\n- Title: %s\n- Trigger/Snippet: %s\n\n",
		obs.Identity.NormPath, obs.Identity.LineStart, obs.Identity.LineEnd,
		obsScope, obs.Representative.Category, obs.Representative.Title, obsSnippet))

	prompt.WriteString("Candidates to compare:\n")
	for index, candidate := range request.Candidates {
		defect := candidate.Defect
		candScope := defect.ScopeSymbol
		if candScope == "" {
			candScope = defect.SymbolPath
		}
		if candScope == "" {
			candScope = defect.ScopeKey
		}
		candSnippet := strings.TrimSpace(defect.CodeSnippet)
		if candSnippet == "" {
			candSnippet = defect.CleanToken
		}
		prompt.WriteString(fmt.Sprintf("Candidate %d (candidate_id=%d, match_score=%.4f):\n- File: %s\n- Lines: %d-%d\n- Scope: %s\n- Category: %s\n- Title: %s\n- Trigger/Snippet: %s\n\n",
			index+1, defect.ID, candidate.Score, defect.NormPath, derefInt(defect.LineStart), derefInt(defect.LineEnd),
			candScope, defect.Category, defect.Title, candSnippet))
	}
	prompt.WriteString(`Output format JSON only:
{"decisions":[{"candidate_id":123,"decision":"SAME|DIFFERENT|REVIEW","confidence":0.95,"reason":"explain similarity in root cause and context"}]}`)

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
