package services

// SingleEngine 是默认的单次执行引擎，将整个代码仓作为一个整体提交给 AI 分析。
type SingleEngine struct{}

func (e *SingleEngine) Run(ctx *taskContext) error {
	return ctx.executeAI(nil, "")
}

func init() {
	RegisterEngine("single", &SingleEngine{})
}
