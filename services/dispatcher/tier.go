package dispatcher

import (
	"context"

	"code-shield/models"
)

// TierRouter 将逻辑 Tier (tier1_fast / tier2_reasoning / tier3_synthesis) 映射为底层物理 backend + model
type TierRouter struct {
	dispatcher *ModelDispatcher
}

// GlobalTierRouter 全局阶梯路由器实例
var GlobalTierRouter *TierRouter

// TierAcquisition 代表申请到的阶梯槽位
type TierAcquisition struct {
	Resource  *ModelResource
	Backend   string
	ModelName string
	Release   func()
}

// NewTierRouter 创建阶梯路由实例
func NewTierRouter(d *ModelDispatcher) *TierRouter {
	return &TierRouter{dispatcher: d}
}

// GetTierRouter 获取全局阶梯路由单例
func GetTierRouter() *TierRouter {
	if GlobalTierRouter == nil {
		GlobalTierRouter = &TierRouter{dispatcher: GlobalDispatcher}
	}
	return GlobalTierRouter
}

// AcquireTier 解析指定阶梯的路由配置，支持多资源池化（Multi-Resource Pooling）动态择优调度
func (tr *TierRouter) AcquireTier(ctx context.Context, tierName string, overrideBackend string) (*TierAcquisition, error) {
	tierCfg := models.AppConfig.GetTierConfig(tierName)
	backend := tierCfg.Backend
	modelName := tierCfg.Model

	if overrideBackend != "" {
		backend = overrideBackend
	} else {
		candidateResources := models.AppConfig.GetTierResources(tierName)
		if len(candidateResources) > 1 && tr.dispatcher != nil && tr.dispatcher.enabled {
			bestBackend, bestModel := tr.dispatcher.PickBestCandidateResource(candidateResources)
			if bestBackend != "" {
				backend = bestBackend
				modelName = bestModel
			}
		} else if len(candidateResources) == 1 {
			if res := models.AppConfig.FindResource(candidateResources[0]); res != nil {
				backend = res.Driver
				modelName = res.Model
			}
		}
	}

	if backend == "" {
		backend = models.AppConfig.AI.Backend
	}

	acq := &TierAcquisition{
		Backend:   backend,
		ModelName: modelName,
		Release: func() {
			// 物理槽位统一由 DispatchingInvoker.Invoke 闭环管理，此处保留空实现以兼容上层 defer 调用
		},
	}
	return acq, nil
}
