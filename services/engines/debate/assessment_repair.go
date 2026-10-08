package debate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"code-shield/models"
	"code-shield/services/dispatcher"
	"code-shield/services/engines/assessment"
	"code-shield/services/invoker"
)

func (e *DebateEngine) repairAssessmentArtifact(
	ctx context.Context,
	request assessment.DirectedRepairRequest,
) (string, int64, error) {
	resource := models.AppConfig.SchemaRepairResource()
	rawInvoker, ok := invoker.GetRawInvoker(resource)
	if !ok || rawInvoker == nil {
		return "", 0, &assessment.NonRetryableRepairError{
			Err: fmt.Errorf("schema repair resource %q is not registered", resource),
		}
	}

	outputFile, err := os.CreateTemp("", "assessment-artifact-repair-*.json")
	if err != nil {
		return "", 0, &assessment.NonRetryableRepairError{
			Err: fmt.Errorf("create assessment repair output: %w", err),
		}
	}
	outputPath := outputFile.Name()
	if closeErr := outputFile.Close(); closeErr != nil {
		_ = os.Remove(outputPath)
		return "", 0, &assessment.NonRetryableRepairError{Err: closeErr}
	}
	defer os.Remove(outputPath)

	prompt := buildDirectedAssessmentRepairPrompt(request)
	temperature := 0.0
	req := invoker.AIRequest{
		ParentContext:  ctx,
		PromptMsg:      prompt,
		OutputPath:     outputPath,
		TimeoutSeconds: models.AppConfig.SchemaRepairTimeoutSeconds(),
		Temperature:    &temperature,
		ResponseFormat: "json",
		WorkContext: &invoker.LLMWorkContext{
			Stage:    "系统工具: Specialized Assessment Artifact Repair",
			SubTask:  "修复结构化评估 artifact 契约",
			TierName: "system_tool",
		},
	}
	if err := dispatcher.WrapInvoker(rawInvoker).Invoke(req); err != nil {
		switch invoker.ClassifyError(err) {
		case invoker.ErrorClassContentFiltered, invoker.ErrorClassAuth, invoker.ErrorClassConfig:
			return "", 0, &assessment.NonRetryableRepairError{Err: err}
		}
		return "", 0, err
	}
	repaired, err := os.ReadFile(outputPath)
	if err != nil {
		return "", 0, &assessment.NonRetryableRepairError{
			Err: fmt.Errorf("read assessment repair output: %w", err),
		}
	}
	return string(repaired), int64((len(prompt) + len(repaired)) / 4), nil
}

func buildDirectedAssessmentRepairPrompt(request assessment.DirectedRepairRequest) string {
	var sb strings.Builder
	sb.WriteString("你是 JSON Artifact 修复器。只修复 JSON 结构、类型、unit_ref、枚举和转义错误；")
	sb.WriteString("不得新增、删除 assessment unit，不得修改 outcome、summary、reason、evidence、domain、issues 等业务结论。\n\n")
	sb.WriteString("## Validation Issues\n```json\n")
	issues, _ := json.MarshalIndent(request.Issues, "", "  ")
	sb.Write(issues)
	sb.WriteString("\n```\n\n## Artifact To Repair\n```json\n")
	sb.WriteString(strings.TrimSpace(request.RawArtifact))
	sb.WriteString("\n```\n\n## JSON Schema\n```json\n")
	schema, _ := json.MarshalIndent(request.Contract.JSONSchema, "", "  ")
	sb.Write(schema)
	sb.WriteString("\n```\n\n## Allowed unit_ref Values\n")
	for _, ref := range request.ValidRefs {
		displayName := strings.TrimSpace(ref.DisplayName)
		if displayName == "" {
			displayName = "unknown target"
		}
		sb.WriteString(fmt.Sprintf("- %s -> %s (%s)\n", ref.Ref, ref.UnitID, displayName))
	}
	sb.WriteString("\n## Allowed Enums\n")
	names := make([]string, 0, len(request.AllowedEnums))
	for name := range request.AllowedEnums {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		values := request.AllowedEnums[name]
		sb.WriteString(fmt.Sprintf("- %s: %s\n", name, strings.Join(values, ", ")))
	}
	sb.WriteString("\n只输出修复后的完整 JSON；不要 Markdown 围栏、解释或日志。\n")
	return sb.String()
}

func assessmentJSONSchemaRequest(contract assessment.ArtifactContract, mode string) *invoker.JSONSchemaRequest {
	if mode != "auto" && mode != "strict" || len(contract.JSONSchema) == 0 {
		return nil
	}
	name := strings.ReplaceAll(contract.SchemaID, ".", "_")
	if len(name) > 64 {
		name = name[:64]
	}
	return &invoker.JSONSchemaRequest{
		Name:   name,
		Strict: mode == "strict",
		Schema: contract.JSONSchema,
	}
}

func callSpecializedAssessmentTier(
	ctx context.Context,
	backend string,
	modelName string,
	prompt string,
	workDir string,
	outPath string,
	timeoutSeconds int,
	timeoutPolicy aiTimeoutPolicy,
	metrics *invoker.InvocationMetrics,
	jsonSchema *invoker.JSONSchemaRequest,
	workCtx ...*invoker.LLMWorkContext,
) (string, int64, error) {
	return invokeAITierWithRequestOptions(
		ctx, backend, modelName, prompt, "", workDir, outPath, timeoutSeconds, timeoutPolicy, metrics,
		func(request *invoker.AIRequest) { request.JSONSchema = jsonSchema },
		workCtx...,
	)
}
