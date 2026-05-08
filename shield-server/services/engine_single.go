package services

// SingleEngine 是默认的单次执行引擎，将整个代码仓作为一个整体提交给 AI 分析。
// 采用双阶段流程：分析（JSON） → 综合（Markdown）。
type SingleEngine struct{}

func (e *SingleEngine) Run(ctx *taskContext) error {
	// Phase 1: 分析阶段 — AI 输出结构化 JSON 问题清单
	findings, err := ctx.executeAnalysis(nil)
	if err != nil {
		return err
	}

	// Phase 2: 综合阶段 — 基于 JSON 生成最终 Markdown 报告
	return ctx.executeSynthesis(findings)
}

func init() {
	RegisterEngine("single", &SingleEngine{})
}
