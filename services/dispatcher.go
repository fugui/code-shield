package services

import (
	"code-shield/services/dispatcher"
)

// 导出 dispatcher 子包中的核心类型别名，保证 100% 向后兼容
type (
	ModelDispatcher     = dispatcher.ModelDispatcher
	ModelResource       = dispatcher.ModelResource
	ModelResourceStatus = dispatcher.ModelResourceStatus
	ThrottleInfo        = dispatcher.ThrottleInfo
	LLMSlotLease        = dispatcher.LLMSlotLease
	TierRouter          = dispatcher.TierRouter
	TierAcquisition     = dispatcher.TierAcquisition
)

// Dispatcher 为多 LLM 并发分配器的全局单例引用
var Dispatcher *ModelDispatcher = dispatcher.GlobalDispatcher

// InitModelDispatcher 初始化全局并发调度器并同步单例引用
func InitModelDispatcher() {
	dispatcher.InitModelDispatcher()
	Dispatcher = dispatcher.GlobalDispatcher
}

// GetTierRouter 获取算力分级路由器门面
func GetTierRouter() *TierRouter {
	return dispatcher.GetTierRouter()
}
