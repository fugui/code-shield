package services

// TaskEngine 定义了任务执行引擎的接口。
// 每种执行模式（如 single、chunked）需要实现此接口，并通过 init() 注册到 engineRegistry 中。
type TaskEngine interface {
	// Run 执行任务。调用方已完成数据加载、代码同步和前置检查，
	// 引擎只需负责 AI 执行、后处理和结果归档。
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
