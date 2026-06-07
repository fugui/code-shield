package services

// TaskEngine 定义了任务执行引擎的接口。
// 每种执行模式（如 single、chunked）需要实现此接口，并通过 init() 注册到 engineRegistry 中。
//
// 引擎契约：
//   - 调用方已完成数据加载、代码同步、前置检查和输出路径准备
//   - ctx.reportPath / ctx.jsonPath 已就绪，引擎应将最终报告写入这些路径
//   - 引擎可调用 ctx.executeAI() 作为底层工具
//   - 引擎不得调用外层管线方法（runPostProcess、finalize、markFailed）
//   - 错误通过 return err 传递给外层管线处理
type TaskEngine interface {
	Run(ctx *taskContext) error
}

// engineRegistry 存储已注册的引擎实现
var engineRegistry = map[string]TaskEngine{}

// RegisterEngine 将一个引擎实现注册到指定的模式名下
func RegisterEngine(mode string, engine TaskEngine) {
	engineRegistry[mode] = engine
}

// GetEngine 根据模式名返回对应的引擎实现，如果未找到则回退到 single 引擎
func GetEngine(mode string) TaskEngine {
	if e, ok := engineRegistry[mode]; ok {
		return e
	}
	return engineRegistry["single"]
}
