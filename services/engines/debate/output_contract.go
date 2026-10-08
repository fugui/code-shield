package debate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"code-shield/models"
	"fmt"
	"strings"
)

// OutputProfile identifies the runtime artifact contract. It is selected by
// EngineContext.EngineMode, never by free text inside a task prompt file.
type OutputProfile string

const (
	OutputProfileAssessment OutputProfile = "assessments"
	OutputProfileCandidates OutputProfile = "candidates"
	OutputProfileChallenger OutputProfile = "challenger"
	OutputProfileJudge      OutputProfile = "judge"
	OutputProfileSynthesis  OutputProfile = "synthesis"
)

// OutputContract is the single source of truth for rendering an AI prompt and
// validating the corresponding JSON artifact.
type OutputContract struct {
	Profile           OutputProfile
	Stage             string
	SchemaID          string
	SchemaHash        string
	TopLevel          string
	RequiredFields    []string
	ForbiddenTopLevel []string
	Enums             map[string][]string
	Example           string
}

const (
	CandidatesArtifactSchemaV1    = "code-shield.candidates.v1"
	CandidatesArtifactSchemaV2    = "code-shield.candidates.v2"
	DefenseCasesArtifactSchemaV2  = "code-shield.defense-cases.v2"
	FinalVerdictsArtifactSchemaV2 = "code-shield.final-verdicts.v2"
	FinalVerdictsArtifactSchemaV3 = "code-shield.final-verdicts.v3"
	SynthesisReportArtifactSchema = "code-shield.tier4-synthesis-report.v1"
)

// OutputProfileForEngine maps the only registered engine mode to its artifact family.
func OutputProfileForEngine(mode string) (OutputProfile, error) {
	switch mode {
	case "debate_full":
		return OutputProfileCandidates, nil
	default:
		return "", fmt.Errorf("ENGINE_MODE_INVALID: engine mode %q is not registered", mode)
	}
}

// ContractForStage returns the stage-specific contract for an engine profile.
func ContractForStage(mode string, stage string, allowedCategories []string) (OutputContract, error) {
	if mode != "debate_full" {
		return OutputContract{}, fmt.Errorf("ENGINE_MODE_INVALID: engine mode %q is not registered", mode)
	}
	switch stage {
	case "hunter":
		contract := newHunterCandidateContract(allowedCategories)
		contract.SchemaHash = outputContractSchemaHash(contract)
		return contract, nil
	case "challenger":
		contract := newChallengerContract(nil)
		contract.SchemaHash = outputContractSchemaHash(contract)
		return contract, nil
	case "judge":
		return newJudgeContract(allowedCategories), nil
	default:
		return OutputContract{}, fmt.Errorf("candidate profile does not support stage %q", stage)
	}
}

func ContractForStageWithTaxonomy(mode string, stage string, allowedCategories []string, taxonomy models.CategoryTaxonomy) (OutputContract, error) {
	contract, err := ContractForStage(mode, stage, allowedCategories)
	if err != nil || taxonomy.SchemaVersion < 2 {
		if err == nil {
			contract.SchemaHash = outputContractSchemaHash(contract)
		}
		return contract, err
	}
	codes := make([]string, 0, len(taxonomy.Categories))
	labels := make([]string, 0, len(taxonomy.Categories))
	exampleCode := "RACE_RELEASE_ACCESS"
	for _, category := range taxonomy.Categories {
		if taxonomy.SchemaVersion >= 3 && category.Status != "active" {
			continue
		}
		codes = append(codes, category.Code)
		labels = append(labels, category.Label)
	}
	if len(codes) > 0 {
		exampleCode = codes[0]
	}
	contract.Enums["category"] = labels
	contract.Enums["category_code"] = codes
	if stage == "hunter" {
		contract.SchemaID = CandidatesArtifactSchemaV2
		contract.Example = strings.Replace(contract.Example, `"category":`, fmt.Sprintf(`"category_code": %q, "category":`, exampleCode), 1)
	} else if stage == "judge" {
		contract.SchemaID = FinalVerdictsArtifactSchemaV3
		contract.Example = strings.Replace(contract.Example, `"category":`, fmt.Sprintf(`"category_code": %q, "category":`, exampleCode), 1)
	}
	contract.SchemaHash = outputContractSchemaHash(contract)
	return contract, nil
}

func outputContractSchemaHash(contract OutputContract) string {
	schemaBytes, err := json.Marshal(map[string]any{
		"schema_id":           contract.SchemaID,
		"top_level":           contract.TopLevel,
		"required_fields":     contract.RequiredFields,
		"forbidden_top_level": contract.ForbiddenTopLevel,
		"enums":               contract.Enums,
	})
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(schemaBytes)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func newHunterCandidateContract(allowedCategories []string) OutputContract {
	category := "标准缺陷分类"
	if len(allowedCategories) > 0 {
		category = allowedCategories[0]
	}
	return OutputContract{
		Profile:           OutputProfileCandidates,
		Stage:             "hunter",
		SchemaID:          CandidatesArtifactSchemaV1,
		TopLevel:          "candidates",
		RequiredFields:    []string{"schema", "candidates"},
		ForbiddenTopLevel: []string{"findings", "defense_cases", "final_verdicts"},
		Enums: map[string][]string{
			"category": allowedCategories,
		},
		Example: fmt.Sprintf(`{
  "schema": %q,
  "candidates": [
    {
      "candidate_id": "H-001",
      "file_path": "relative/path.cpp",
      "line_range": "42-50",
      "additional_line_ranges": ["35", "120-125"],
      "trigger_line": "if (value > threshold)",
      "scope_symbol": "ClassName::functionName",
      "category": %q,
      "title": "简明候选标题",
      "code_snippet": "真实源码片段",
      "trigger_condition": "触发前提、数据状态、调用路径和后果"
    }
  ]
}`, CandidatesArtifactSchemaV1, category),
	}
}

func newChallengerContract(allowedDimensions []string) OutputContract {
	contract := OutputContract{
		Profile:           OutputProfileChallenger,
		Stage:             "challenger",
		SchemaID:          DefenseCasesArtifactSchemaV2,
		TopLevel:          "defense_cases",
		RequiredFields:    []string{"schema", "defense_cases"},
		ForbiddenTopLevel: []string{"candidates", "findings", "final_verdicts"},
		Enums: map[string][]string{
			"defense_verdict": {"DEFENSE_SUCCESSFUL", "DEFENSE_PARTIAL", "CHALLENGE_FAILED"},
			"dimension":       allowedDimensions,
		},
		Example: fmt.Sprintf(`{
  "schema": %q,
  "defense_cases": [
    {
      "candidate_id": "H-001",
      "defense_verdict": "DEFENSE_SUCCESSFUL",
      "defense_arguments": [
        {
          "dimension": "Guards",
          "finding": "引用真实源码或调用链说明候选已被前置防御",
          "evidence": [
            {
              "evidence_id": "H-001-symbol-001",
              "kind": "caller",
              "path": "relative/path.cpp",
              "line_range": "120-135",
              "snippet": "真实源码片段",
              "reason": "证明调用点受到外层防护"
            }
          ]
        }
      ],
      "mitigating_factors": "简述豁免或防护事实",
      "counter_evidence_snippet": "真实代码片段；没有证据时为空字符串"
    }
  ]
}`, DefenseCasesArtifactSchemaV2),
	}
	contract.SchemaHash = outputContractSchemaHash(contract)
	return contract
}

func newJudgeContract(allowedCategories []string) OutputContract {
	category := "标准缺陷分类"
	if len(allowedCategories) > 0 {
		category = allowedCategories[0]
	}
	return OutputContract{
		Profile:           OutputProfileJudge,
		Stage:             "judge",
		SchemaID:          FinalVerdictsArtifactSchemaV2,
		TopLevel:          "final_verdicts",
		RequiredFields:    []string{"schema", "final_verdicts"},
		ForbiddenTopLevel: []string{"candidates", "findings", "defense_cases"},
		Enums: map[string][]string{
			"category":             allowedCategories,
			"severity_preliminary": {"致命", "严重", "一般", "建议"},
			"verdict":              {"CONFIRMED", "REJECTED", "CONDITIONAL"},
		},
		Example: fmt.Sprintf(`{
  "schema": %q,
  "final_verdicts": [
    {
      "candidate_id": "H-001",
      "verdict": "CONFIRMED",
      "severity_preliminary": "严重",
      "category": %q,
      "file_path": "relative/path.cpp",
      "line_range": "42-50",
      "trigger_line": "if (value > threshold)",
      "scope_symbol": "ClassName::functionName",
      "title": "简明结论标题",
      "judgement_rationale": "引用双方证据和源码事实进行终审说明",
      "code_snippet": "真实源码片段",
      "suggestion": "可执行修复方案",
      "evidence": [
        {
          "evidence_id": "H-001-symbol-001",
          "kind": "caller",
          "path": "relative/path.cpp",
          "line_range": "120-140",
          "snippet": "真实源码片段",
          "reason": "证明触发路径或防护事实"
        }
      ]
    }
  ]
}`, FinalVerdictsArtifactSchemaV2, category),
	}
}

// RenderOutputContract renders the machine contract into a deterministic prompt
// section. The renderer and validator must always read the same contract.
func RenderOutputContract(contract OutputContract) string {
	var sb strings.Builder
	sb.WriteString("## Stage Output Contract\n")
	sb.WriteString("必须输出一个合法 UTF-8 JSON 对象；不得输出 Markdown 代码围栏、注释、日志或解释文字。\n")
	sb.WriteString(fmt.Sprintf("- schema：`%s`\n", contract.SchemaID))
	sb.WriteString(fmt.Sprintf("- 顶层主键：`%s`\n", contract.TopLevel))
	sb.WriteString("- 必填顶层字段：")
	for i, field := range contract.RequiredFields {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString("`" + field + "`")
	}
	sb.WriteString("\n")
	if len(contract.ForbiddenTopLevel) > 0 {
		sb.WriteString("- 禁止出现的顶层字段：")
		for i, field := range contract.ForbiddenTopLevel {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString("`" + field + "`")
		}
		sb.WriteString("\n")
	}
	for _, field := range []string{"category_code", "category", "severity", "severity_preliminary", "verdict", "defense_verdict"} {
		values := contract.Enums[field]
		if len(values) == 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("- `%s` 枚举：`%s`\n", field, strings.Join(values, "` | `")))
	}
	if contract.Profile == OutputProfileCandidates {
		sb.WriteString("- `candidate_id` / `file_path` / `line_range` / `scope_symbol` / `category_code` / `category` / `title` / `code_snippet` / `trigger_condition` 是精确字段名。\n")
		sb.WriteString("- 禁止使用 `id`、`file`、`path`、`function`、`description`、`snippet` 等替代字段名。\n")
		sb.WriteString("- `file_path` 必须是相对 repo root 的 POSIX 路径，禁止绝对路径。\n")
		sb.WriteString("- `line_range` 只能是单一主锚点：`42` 或 `42-50`；不要输出 `42,43` 或 `1-2,10-20`。\n")
		sb.WriteString("- 若缺陷跨越多个不连续位置，主触发位置写入 `line_range`，其他位置写入 `additional_line_ranges`。\n")
		sb.WriteString("- `additional_line_ranges` 是可选字符串数组，每个元素是一个独立区间，例如 `[\"70-91\", \"120-125\"]`。\n")
		sb.WriteString("- 禁止把多个区间合并进一个字符串（如 `\"70-91 120-125\"`）、禁止嵌套数组或 `[70, 91]` 数字对。\n")
		sb.WriteString("- `line_range` 与 `additional_line_ranges` 禁止携带文件名前缀（如 `a.cpp:10-20`），只输出行号或行号区间。\n")
		sb.WriteString("- `trigger_line` 必须引用命中的真实源码行原文，禁止空字符串或省略。\n")
	}
	if contract.Profile == OutputProfileJudge {
		sb.WriteString("- `evidence[].kind` 枚举：`target_source` | `related_source` | `caller` | `definition` | `macro` | `absence_probe`\n")
		sb.WriteString("- `evidence[].evidence_id` 必须原样复制当前候选 Evidence Pack 中已有 Evidence 的 ID；服务端会以该 ID 对应的 path/line/snippet 为准。\n")
		sb.WriteString("- `absence_probe[].probe_id` / `evidence[].probe_id` 必须原样复制 Evidence Pack 中的 `probe_id`；没有可用 probe 时不要输出 absence_probe。\n")
		sb.WriteString("- `line_range` 只能是一个区间，如 `42` 或 `42-50`；不要输出 `42,43` 或 `1-2,10-20`。\n")
		sb.WriteString("- `CONFIRMED` 必须包含同一候选的 source 证据，且至少包含一个 `caller` / `definition` / `macro` 类型的可达性证据。\n")
		sb.WriteString("- 禁止返回 `source_verified`；该字段由服务端 Evidence Verifier 计算。\n")
	}
	if contract.Profile == OutputProfileChallenger {
		sb.WriteString("- `defense_arguments[].evidence[].kind` 枚举：`target_source` | `related_source` | `caller` | `definition` | `macro` | `absence_probe`\n")
		sb.WriteString("- `defense_arguments[].evidence[].evidence_id` 必须原样复制当前候选 Evidence Pack 中已有 Evidence 的 ID；服务端会以该 ID 对应的 path/line/snippet 为准。\n")
		sb.WriteString("- 引用“没有实际调用点”时，必须使用 Evidence Pack 中已有的 `probe_id`；没有可用 probe 时返回 `CHALLENGE_FAILED`。\n")
		sb.WriteString("- `DEFENSE_SUCCESSFUL` / `DEFENSE_PARTIAL` 的每个 defense_argument 必须携带 evidence。\n")
		sb.WriteString("- `CHALLENGE_FAILED` 可以省略 evidence。\n")
		sb.WriteString("- 禁止返回 `source_verified`；该字段由服务端 Evidence Verifier 计算。\n")
	}
	sb.WriteString("\n结构示例（仅说明字段；输出时不得复制本说明，也不得添加围栏）：\n")
	sb.WriteString(contract.Example)
	sb.WriteString("\n")
	return sb.String()
}
