package runner

import (
	"fmt"
	"log"
	"strings"

	"code-shield/models"
)

// TaskResult 承载后处理评分与各级别指标汇总
type TaskResult struct {
	Score   int            `json:"score"`
	Summary string         `json:"summary"`
	Metrics map[string]int `json:"metrics"`
}

// RunPostProcess 解析 findings 并计算综合评分与度量指标
func RunPostProcess(findings []models.AnalysisFinding, taskType models.TaskType) TaskResult {
	var result TaskResult

	severityWeight := map[string]int{
		"fatal":       5,
		"致命":          5,
		"blocker":     5,
		"阻塞":          5,
		"blocking":    5,
		"p0":          5,
		"critical":    4,
		"严重":          4,
		"major_error": 4,
		"error":       4,
		"高":           4,
		"高危":          4,
		"高风险":         4,
		"high":        4,
		"high_risk":   4,
		"p1":          4,
		"major":       3,
		"主要":          3,
		"minor":       2,
		"一般":          2,
		"warning":     2,
		"中":           2,
		"中危":          2,
		"中风险":         2,
		"medium":      2,
		"medium_risk": 2,
		"p2":          2,
		"low":         1,
		"低":           1,
		"低危":          1,
		"低风险":         1,
		"low_risk":    1,
		"hint":        1,
		"p3":          1,
		"suggestion":  0,
		"建议":          0,
		"info":        0,
		"提示":          0,
		"comment":     0,
		"合格":          0,
		"pass":        0,
		"p4":          0,
	}

	metrics := map[string]int{}
	score := 0
	for _, f := range findings {
		sev := strings.ToLower(strings.TrimSpace(f.Severity))
		weight := severityWeight[sev]
		score += weight

		key := sev
		switch sev {
		case "致命", "fatal", "blocker", "阻塞", "blocking", "p0":
			key = "blocking"
		case "严重", "critical", "major_error", "error", "p1":
			key = "critical"
		case "高", "高危", "高风险", "high", "high_risk":
			key = "high_risk"
		case "中", "中危", "中风险", "medium", "medium_risk", "p2":
			key = "medium_risk"
		case "一般", "minor", "warning", "主要", "major":
			key = "minor"
		case "低", "低危", "低风险", "low", "low_risk", "p3":
			key = "low_risk"
		case "建议", "suggestion", "comment", "提示", "info", "hint", "p4":
			key = "suggestion"
		case "合格", "pass":
			key = "pass"
		}
		metrics[key]++
	}
	result.Score = score
	result.Metrics = metrics

	// 构造文字概要
	var summaryParts []string
	if result.Metrics["blocking"] > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("致命：%d个", result.Metrics["blocking"]))
	}
	if result.Metrics["critical"] > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("严重：%d个", result.Metrics["critical"]))
	}
	if result.Metrics["high_risk"] > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("高风险：%d个", result.Metrics["high_risk"]))
	}
	if result.Metrics["medium_risk"] > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("中风险：%d个", result.Metrics["medium_risk"]))
	}
	totalMinor := result.Metrics["minor"] + result.Metrics["major"] + result.Metrics["hint"]
	if totalMinor > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("一般：%d个", totalMinor))
	}
	if result.Metrics["low_risk"] > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("低风险：%d个", result.Metrics["low_risk"]))
	}
	if result.Metrics["suggestion"] > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("建议：%d个", result.Metrics["suggestion"]))
	}

	if len(summaryParts) == 0 {
		if taskType.GovernanceMode == models.GovernanceModeEntityAssessment {
			if len(findings) == 0 {
				result.Summary = "未检测到任何评估实体或测试用例！"
			} else {
				result.Summary = "所有评估实体/用例均符合质量标准！"
			}
		} else {
			result.Summary = "未发现相关类型的代码缺陷"
		}
	} else {
		result.Summary = strings.Join(summaryParts, "，")
	}

	log.Printf("[PostProcess] Score: %d, Summary len: %d, Metrics: %v\n", score, len(result.Summary), metrics)
	return result
}
