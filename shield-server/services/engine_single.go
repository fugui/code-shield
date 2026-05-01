package services

// SingleEngine 是默认的单次执行引擎，将整个代码仓作为一个整体提交给 AI 分析。
type SingleEngine struct{}

func (e *SingleEngine) Run(ctx *taskContext) error {
	// 执行 AI 引擎
	if err := ctx.executeAI(nil, ""); err != nil {
		ctx.markFailed(err.Error())
		return err
	}

	// 后处理并归档结果
	result := ctx.runPostProcess()
	return ctx.finalize(result)
}

func init() {
	RegisterEngine("single", &SingleEngine{})
}
