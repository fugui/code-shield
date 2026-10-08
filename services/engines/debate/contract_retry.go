package debate

import (
	"code-shield/models"
	"code-shield/services/invoker"
	"fmt"
	"strings"
)

// isContractMismatch reports whether an AI response violated the shape or field
// contract. These failures are deterministic enough for one targeted retry.
func isContractMismatch(err error) bool {
	if err == nil {
		return false
	}
	if invoker.ClassifyError(err) == invoker.ErrorClassContractMismatch {
		return true
	}
	message := err.Error()
	for _, marker := range []string{
		"schema mismatch",
		"forbidden top-level key",
		"missing top-level key",
		"schema is not a string",
		"top-level is not a JSON object",
		"is not valid JSON",
		"assessment produced no valid units",
		"assessment artifact requires repair",
		"assessment artifact invalid",
		"invalid candidate",
		"invalid finding",
		"invalid defense_case",
		"invalid final_verdict",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func contractRepairEnabled(tierName string, err error) bool {
	if err == nil {
		return false
	}
	class := invoker.ClassifyError(err)
	if class != invoker.ErrorClassContractMismatch && class != invoker.ErrorClassJSONInvalid {
		class = invoker.ErrorClassContractMismatch
	}
	recovery := models.AppConfig.GetTierConfig(tierName).Recovery
	return recoveryAllows(recovery.ContractRepairOn, class)
}

// buildContractRetryPrompt adds the validator failure and re-renders the same
// contract. Keeping this in code ensures the retry cannot drift from the parser.
func buildContractRetryPrompt(prompt string, contract OutputContract, cause error) string {
	var sb strings.Builder
	sb.WriteString(strings.TrimRight(prompt, "\n"))
	sb.WriteString("\n\n## Output Contract Correction\n")
	sb.WriteString(fmt.Sprintf("上一次输出未通过服务端校验：%v。\n", cause))
	sb.WriteString("请重新输出一个完整结果；不要引用上次错误结果，不要添加解释文字，不要输出 Markdown 代码围栏。\n")
	sb.WriteString("必须严格遵循以下唯一输出契约：\n\n")
	sb.WriteString(RenderOutputContract(contract))
	return sb.String()
}
