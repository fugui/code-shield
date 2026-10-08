package dispatcher

import (
	"context"
	"fmt"

	"code-shield/models"
)

// TierRouter 将逻辑 Tier (tier1_hunter / tier2_challenger / tier3_judge / tier4_synthesis) 映射为底层物理 backend + model
type TierRouter struct {
	dispatcher *ModelDispatcher
}

// GlobalTierRouter 全局阶梯路由器实例
var GlobalTierRouter *TierRouter

// TierAcquisition 代表申请到的阶梯槽位
type TierAcquisition struct {
	Resource   *ModelResource
	ResourceID string
	Backend    string
	ModelName  string
	Release    func()
}

// TierCandidate identifies one configured resource pool. The resource ID is the
// stable stage execution identity; driver and model describe how to invoke it.
type TierCandidate struct {
	ResourceID string
	Driver     string
	Model      string
	Available  bool
}

// TierPlan keeps a selected candidate and its ordered fallbacks. Ordinary
// retries must always use Plan.Selected; only explicit availability failover
// may advance through Candidates.
type TierPlan struct {
	Tier       string
	Selected   TierCandidate
	Candidates []TierCandidate
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
			bestResourceID, bestModel := tr.dispatcher.PickBestCandidateResource(candidateResources)
			if bestResource := models.AppConfig.FindResource(bestResourceID); bestResource != nil {
				backend = bestResource.Driver
				modelName = bestModel
				if modelName == "" {
					modelName = bestResource.ResourceModel()
				}
			}
		} else if len(candidateResources) == 1 {
			if res := models.AppConfig.FindResource(candidateResources[0]); res != nil {
				backend = res.Driver
				modelName = res.ResourceModel()
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

// AcquireTierResourcePlan resolves a stable resource identity and preserves the
// configured fallback order.
func (tr *TierRouter) AcquireTierResourcePlan(ctx context.Context, tierName string, excluded map[string]struct{}) (TierPlan, error) {
	candidateResources := models.AppConfig.GetTierResources(tierName)
	candidates := make([]TierCandidate, 0, len(candidateResources))
	nowAvailable := map[string]bool{}
	for _, resourceID := range candidateResources {
		if _, skip := excluded[resourceID]; skip {
			continue
		}
		available := tr.dispatcher.IsResourceAvailable(resourceID)
		nowAvailable[resourceID] = available
		if res := models.AppConfig.FindResource(resourceID); res != nil {
			candidates = append(candidates, TierCandidate{ResourceID: resourceID, Driver: res.Driver, Model: res.ResourceModel(), Available: available})
			continue
		}
		candidates = append(candidates, TierCandidate{ResourceID: resourceID, Driver: resourceID, Available: available})
	}
	if len(candidates) == 0 {
		return TierPlan{}, fmt.Errorf("tier %q has no unexcluded resources", tierName)
	}

	selected := candidates[0]
	for _, candidate := range candidates {
		if nowAvailable[candidate.ResourceID] {
			selected = candidate
			break
		}
	}
	if len(candidates) > 1 && tr.dispatcher != nil && tr.dispatcher.enabled {
		ids := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			ids = append(ids, candidate.ResourceID)
		}
		bestResourceID, _ := tr.dispatcher.PickBestCandidateResource(ids)
		for _, candidate := range candidates {
			if candidate.ResourceID == bestResourceID && nowAvailable[candidate.ResourceID] {
				selected = candidate
				break
			}
		}
	}
	return TierPlan{Tier: tierName, Selected: selected, Candidates: candidates}, nil
}

// NextTierCandidate returns the first configured candidate after the current
// resource, excluding resources that have already failed.
func NextTierCandidate(plan TierPlan, excluded map[string]struct{}) (TierCandidate, bool) {
	selectedIndex := -1
	for index, candidate := range plan.Candidates {
		if candidate.ResourceID == plan.Selected.ResourceID {
			selectedIndex = index
			break
		}
	}
	if selectedIndex < 0 {
		return TierCandidate{}, false
	}
	for _, candidate := range plan.Candidates[selectedIndex+1:] {
		if _, skip := excluded[candidate.ResourceID]; skip {
			continue
		}
		return candidate, true
	}
	return TierCandidate{}, false
}
