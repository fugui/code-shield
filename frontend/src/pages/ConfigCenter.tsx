import React, { useState, useEffect, useCallback } from 'react';
import { useParams, useNavigate, useSearchParams } from 'react-router-dom';
import { useToast } from '../components/Toast';
import { Drawer } from '@code/common';
import { appNavigatePath } from '../config';
import {
  LLMConfig,
  ComputeResource,
  DebateTiers,
  TierBinding,
  ComputeConfigResponse,
  UpdateComputeConfigRequest,
  PingResult
} from '../types/config';
import './ConfigCenter.css';

type ActiveTab = 'pool' | 'pipeline';
type TierKeyType = 'tier1_hunter' | 'tier2_challenger' | 'tier3_judge' | 'tier4_synthesis';

interface TierDefinition {
  key: TierKeyType;
  title: string;
  badge: string;
  badgeClass: string;
  roleType: 'thick' | 'native';
  roleTag: string;
  description: string;
}

const TIER_DEFS: TierDefinition[] = [
  {
    key: 'tier1_hunter',
    title: 'Tier 1: 猎手阶段 (Hunter)',
    badge: 'T1 · 广度探测',
    badgeClass: 'code-config-tier-card__badge--tier1_hunter',
    roleType: 'thick',
    roleTag: '必须 Thick Agent 驱动',
    description: '全仓语义扫描与初筛候选缺陷生成，依托 AGY/OpenCode 等自主 Agent 进行上下文代码理解。'
  },
  {
    key: 'tier2_challenger',
    title: 'Tier 2: 挑战者阶段 (Challenger)',
    badge: 'T2 · 深度质询',
    badgeClass: 'code-config-tier-card__badge--tier2_challenger',
    roleType: 'native',
    roleTag: '必须 Native 驱动',
    description: '对初筛缺陷提出质疑与反例，使用 Native 端点进行高速反思与防御取证。'
  },
  {
    key: 'tier3_judge',
    title: 'Tier 3: 裁判阶段 (Judge)',
    badge: 'T3 · 终局裁决',
    badgeClass: 'code-config-tier-card__badge--tier3_judge',
    roleType: 'thick',
    roleTag: '必须 Thick Agent 驱动',
    description: '综合正反双方证据与代码上下文，进行权威裁决。'
  },
  {
    key: 'tier4_synthesis',
    title: 'Tier 4: 综合裁决 (Synthesis)',
    badge: 'T4 · 修复建议',
    badgeClass: 'code-config-tier-card__badge--tier4_synthesis',
    roleType: 'native',
    roleTag: '必须 Native 驱动',
    description: '最终报告归纳与修复补丁生成，利用 Native 高吞吐推理生成结构化建议。'
  }
];

export default function ConfigCenter() {
  const { showToast } = useToast();
  const { tab } = useParams<{ tab?: string }>();
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();

  const resolveTab = useCallback((): ActiveTab => {
    const raw = tab || searchParams.get('tab');
    if (raw === 'pipeline' || raw === 'tiers') return 'pipeline';
    return 'pool';
  }, [tab, searchParams]);

  const [activeTab, setActiveTab] = useState<ActiveTab>(resolveTab);

  useEffect(() => {
    const nextTab = resolveTab();
    if (nextTab !== activeTab) {
      setActiveTab(nextTab);
    }
  }, [resolveTab, activeTab]);

  const handleTabChange = (nextTab: ActiveTab) => {
    setActiveTab(nextTab);
    navigate(appNavigatePath(`/admin/config/${nextTab}`));
  };

  // 权威动态算力数据状态
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [resetting, setResetting] = useState(false);
  const [version, setVersion] = useState<number>(1);
  const [updatedAt, setUpdatedAt] = useState<string>('');
  const [updatedBy, setUpdatedBy] = useState<string>('');
  const [workerCount, setWorkerCount] = useState<number>(8);
  const [conflictError, setConflictError] = useState<string | null>(null);

  const [llmConfig, setLlmConfig] = useState<LLMConfig>({
    default_resource: 'native-1',
    debug_logs: false,
    resources: []
  });

  const [tiersConfig, setTiersConfig] = useState<DebateTiers>({
    tier1_hunter: { timeout_seconds: 1800, attempt_timeout_seconds: 900 },
    tier2_challenger: { timeout_seconds: 1200, attempt_timeout_seconds: 600 },
    tier3_judge: { timeout_seconds: 1800, attempt_timeout_seconds: 900 },
    tier4_synthesis: { timeout_seconds: 1200, attempt_timeout_seconds: 600 }
  });

  // 端点测速状态记录
  const [pingStates, setPingStates] = useState<Record<string, { loading?: boolean; latency_ms?: number; error?: string }>>({});

  // 节点 Drawer 编辑状态
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [editingIndex, setEditingIndex] = useState<number | null>(null);
  const [editingResource, setEditingResource] = useState<ComputeResource>({
    id: '',
    driver: 'native',
    model: '',
    concurrent: 5,
    endpoints: []
  });

  // 1. 获取权威动态算力快照
  const fetchComputeConfig = useCallback(async () => {
    setLoading(true);
    setConflictError(null);
    try {
      const res = await fetch('/api/admin/compute/config');
      if (!res.ok) {
        throw new Error(`获取算力配置失败 (HTTP ${res.status})`);
      }
      const data: ComputeConfigResponse = await res.json();
      setVersion(data.version);
      setUpdatedAt(data.updated_at);
      setUpdatedBy(data.updated_by);
      setWorkerCount(data.worker_count || 8);
      if (data.llm) {
        setLlmConfig(data.llm);
      }
      if (data.tiers) {
        setTiersConfig(data.tiers);
      }
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : '获取算力配置失败';
      showToast(msg, 'error');
    } finally {
      setLoading(false);
    }
  }, [showToast]);

  useEffect(() => {
    fetchComputeConfig();
  }, [fetchComputeConfig]);

  // 2. 保存并应用动态算力配置 (带 CAS 乐观锁防并发覆盖)
  const handleSaveConfig = async () => {
    setSaving(true);
    setConflictError(null);

    // 校验默认资源是否存在
    if (!llmConfig.resources.some(r => r.id === llmConfig.default_resource)) {
      showToast(`默认算力资源 [${llmConfig.default_resource}] 在列表中不存在，请重新指定`, 'error');
      setSaving(false);
      return;
    }

    // 校验超时倒挂约束
    for (const def of TIER_DEFS) {
      const binding = tiersConfig[def.key];
      if (binding && binding.attempt_timeout_seconds && binding.timeout_seconds) {
        if (binding.attempt_timeout_seconds > binding.timeout_seconds) {
          showToast(`${def.title} 单次尝试超时 (${binding.attempt_timeout_seconds}s) 不能超过阶段总预算 (${binding.timeout_seconds}s)`, 'error');
          setSaving(false);
          return;
        }
      }
    }

    const payload: UpdateComputeConfigRequest = {
      version,
      llm: llmConfig,
      tiers: tiersConfig,
      worker_count: workerCount
    };

    try {
      const res = await fetch('/api/admin/compute/config', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });

      if (res.status === 409) {
        const errJson = await res.json().catch(() => ({}));
        const conflictMsg = errJson.error || '配置已被其他管理员或外部进程修改，当前版本已失效';
        setConflictError(conflictMsg);
        showToast('保存冲突：版本已过时，请刷新最新版本', 'error');
        return;
      }

      if (!res.ok) {
        const errJson = await res.json().catch(() => ({}));
        throw new Error(errJson.error || `保存失败 (HTTP ${res.status})`);
      }

      const result = await res.json();
      setVersion(result.version);
      setUpdatedAt(result.updated_at);
      setUpdatedBy(result.updated_by);
      showToast(`算力配置已原子保存并热生效 (版本: v${result.version})`, 'success');
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : '保存算力配置发生异常';
      showToast(msg, 'error');
    } finally {
      setSaving(false);
    }
  };

  // 3. 重置为种子冷配置
  const handleResetToSeed = async () => {
    if (!window.confirm('确认重置所有算力与调度配置为 config.yaml 初始种子状态？未保存的动态调整将被重置。')) {
      return;
    }
    setResetting(true);
    try {
      const res = await fetch('/api/admin/compute/reset-to-seed', { method: 'POST' });
      if (!res.ok) {
        const errJson = await res.json().catch(() => ({}));
        throw new Error(errJson.error || '重置种子配置失败');
      }
      showToast('已重置为 config.yaml 初始算力种子', 'success');
      await fetchComputeConfig();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : '重置种子失败';
      showToast(msg, 'error');
    } finally {
      setResetting(false);
    }
  };

  // 4. Ping 算力端点连通性测试
  const handlePingEndpoint = async (key: string, baseURL: string, apiKey: string, model: string) => {
    setPingStates(prev => ({ ...prev, [key]: { loading: true } }));
    try {
      const res = await fetch('/api/admin/compute/ping-endpoint', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ base_url: baseURL, api_key: apiKey, model })
      });
      const data: PingResult = await res.json();
      if (res.ok && data.success) {
        setPingStates(prev => ({
          ...prev,
          [key]: { loading: false, latency_ms: data.latency_ms }
        }));
      } else {
        setPingStates(prev => ({
          ...prev,
          [key]: { loading: false, error: data.message || `HTTP ${data.status_code || res.status}` }
        }));
      }
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : '网络请求失败';
      setPingStates(prev => ({ ...prev, [key]: { loading: false, error: msg } }));
    }
  };

  // 节点 Drawer 操作
  const openAddResource = () => {
    setEditingIndex(null);
    setEditingResource({
      id: `native-${llmConfig.resources.length + 1}`,
      driver: 'native',
      model: '',
      concurrent: 5,
      endpoints: [
        { name: 'primary', base_url: 'http://127.0.0.1:8000/v1', api_key: '', model: 'glm-4-flash', concurrent: 5 }
      ]
    });
    setDrawerOpen(true);
  };

  const openEditResource = (index: number) => {
    setEditingIndex(index);
    setEditingResource(JSON.parse(JSON.stringify(llmConfig.resources[index])));
    setDrawerOpen(true);
  };

  const handleDeleteResource = (index: number) => {
    const resItem = llmConfig.resources[index];
    if (resItem.id === llmConfig.default_resource) {
      showToast(`[${resItem.id}] 为默认资源，不可直接删除，请先指定其他默认资源`, 'warning');
      return;
    }
    if (!window.confirm(`确定删除算力节点 [${resItem.id}] 吗？`)) return;

    setLlmConfig(prev => ({
      ...prev,
      resources: prev.resources.filter((_, i) => i !== index)
    }));
    showToast(`已移除算力节点 [${resItem.id}]，请点击顶部保存生效`, 'info');
  };

  const handleSaveDrawerResource = () => {
    if (!editingResource.id.trim()) {
      showToast('算力节点 ID 不能为空', 'error');
      return;
    }
    if (editingIndex === null) {
      if (llmConfig.resources.some(r => r.id === editingResource.id.trim())) {
        showToast(`节点 ID [${editingResource.id}] 已存在，请更换`, 'error');
        return;
      }
      setLlmConfig(prev => ({
        ...prev,
        resources: [...prev.resources, { ...editingResource, id: editingResource.id.trim() }]
      }));
    } else {
      setLlmConfig(prev => {
        const nextList = [...prev.resources];
        nextList[editingIndex] = { ...editingResource, id: editingResource.id.trim() };
        return { ...prev, resources: nextList };
      });
    }
    setDrawerOpen(false);
  };

  // Tier 胶囊点击切换多选池化绑定
  const handleToggleTierResource = (tierKey: TierKeyType, resourceId: string) => {
    setTiersConfig(prev => {
      const binding: TierBinding = prev[tierKey] || { timeout_seconds: 1200 };
      const currentList = binding.resources && binding.resources.length > 0
        ? [...binding.resources]
        : (binding.resource ? [binding.resource] : []);

      let nextList: string[];
      if (currentList.includes(resourceId)) {
        nextList = currentList.filter(id => id !== resourceId);
      } else {
        nextList = [...currentList, resourceId];
      }

      return {
        ...prev,
        [tierKey]: {
          ...binding,
          resource: nextList[0] || '',
          resources: nextList
        }
      };
    });
  };

  const handleTierTimeoutChange = (tierKey: TierKeyType, field: 'timeout_seconds' | 'attempt_timeout_seconds', val: number) => {
    setTiersConfig(prev => {
      const binding: TierBinding = prev[tierKey] || { timeout_seconds: 1200 };
      return {
        ...prev,
        [tierKey]: {
          ...binding,
          [field]: Math.max(1, val)
        }
      };
    });
  };

  if (loading) {
    return (
      <div className="code-config-container">
        <div className="code-config-card" style={{ textAlign: 'center', padding: '3rem' }}>
          <div className="status-badge status-badge--pending" style={{ fontSize: '1rem', padding: '0.5rem 1rem' }}>
            正在加载算力快照与集群拓扑...
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="code-config-container">
      {/* 顶部 Header 与全局并发控制 */}
      <div className="code-config-header">
        <div className="code-config-header__titles">
          <h1 className="code-config-header__title">
            <span>🛡️ 算力调度中心 (Compute Hub)</span>
            <span className="code-config-badge code-config-badge--blue" title="CAS 乐观锁版本号">v{version}</span>
          </h1>
          <p className="code-config-header__subtitle">
            集中管理全局并发、LLM 推理节点池化与 4 阶梯辩论流水线编排。静态冷配置由 config.yaml 统一托管。
            {updatedAt && <span style={{ marginLeft: '0.75rem', opacity: 0.8 }}>最近更新: {new Date(updatedAt).toLocaleString()} ({updatedBy || '系统'})</span>}
          </p>
        </div>

        <div className="code-config-header__actions">
          <div className="code-config-header__worker-box">
            <label className="code-config-label" style={{ margin: 0, fontWeight: 600 }}>全局 Worker 并发</label>
            <input
              type="number"
              min={1}
              max={64}
              className="code-config-input"
              style={{ width: '80px', textAlign: 'center', padding: '0.4rem' }}
              value={workerCount}
              onChange={e => setWorkerCount(Math.max(1, parseInt(e.target.value, 10) || 1))}
            />
          </div>

          <button
            type="button"
            className="btn btn-secondary"
            onClick={handleResetToSeed}
            disabled={resetting || saving}
            title="纯净解析 config.yaml 种子配置恢复默认"
          >
            {resetting ? '重置中...' : '重置种子'}
          </button>

          <button
            type="button"
            className="btn btn-primary"
            onClick={handleSaveConfig}
            disabled={saving || resetting}
          >
            {saving ? '保存应用中...' : '保存并生效'}
          </button>
        </div>
      </div>

      {/* 乐观锁并发冲突警示条 */}
      {conflictError && (
        <div className="code-config-tier-notice code-config-tier-notice--warning" style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '0.75rem 1rem' }}>
          <div>
            <strong>⚠️ 并发冲突 (HTTP 409)：</strong> {conflictError}
          </div>
          <button className="btn btn-secondary" style={{ padding: '0.2rem 0.6rem', fontSize: '0.8rem' }} onClick={fetchComputeConfig}>
            立即刷新最新数据
          </button>
        </div>
      )}

      {/* 极简双 Tab 导航 */}
      <div className="code-config-tabs">
        <button
          type="button"
          className={`code-config-tab-btn ${activeTab === 'pool' ? 'active' : ''}`}
          onClick={() => handleTabChange('pool')}
        >
          <span>🖥️ 算力供给池 (Compute Pool)</span>
          <span className="code-config-badge code-config-badge--neutral">{llmConfig.resources.length} 个节点</span>
        </button>
        <button
          type="button"
          className={`code-config-tab-btn ${activeTab === 'pipeline' ? 'active' : ''}`}
          onClick={() => handleTabChange('pipeline')}
        >
          <span>⚖️ 辩论流水线编排 (Pipeline Orchestration)</span>
          <span className="code-config-badge code-config-badge--blue">4 阶梯</span>
        </button>
      </div>

      {/* Tab 1: 算力供给池 */}
      {activeTab === 'pool' && (
        <div className="code-config-panel">
          <div className="code-config-card">
            <div className="code-config-card__header" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '1rem' }}>
              <div>
                <h3 className="code-config-card__title">集群算力节点拓扑</h3>
                <p className="code-config-card__desc">配置原生推理机端点 (Native) 或自主智能体引擎 (Thick Agent)，支持动态增删与连通性自检。</p>
              </div>

              <div style={{ display: 'flex', alignItems: 'center', gap: '1rem' }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                  <label className="code-config-label" style={{ margin: 0 }}>默认算力资源:</label>
                  <select
                    className="code-config-select"
                    style={{ width: '150px' }}
                    value={llmConfig.default_resource}
                    onChange={e => setLlmConfig(prev => ({ ...prev, default_resource: e.target.value }))}
                  >
                    {llmConfig.resources.map(r => (
                      <option key={r.id} value={r.id}>{r.id} ({r.driver})</option>
                    ))}
                  </select>
                </div>

                <button type="button" className="btn btn-primary" onClick={openAddResource}>
                  + 添加算力节点
                </button>
              </div>
            </div>

            {/* 节点列表卡片网格 */}
            <div className="code-config-resource-list" style={{ marginTop: '1rem' }}>
              {llmConfig.resources.map((res, index) => {
                const isNative = res.driver === 'native';
                const isDefault = res.id === llmConfig.default_resource;

                return (
                  <div key={res.id} className="code-config-resource-item">
                    <div className="code-config-resource-item__top">
                      <div style={{ display: 'flex', alignItems: 'center', gap: '0.65rem' }}>
                        <span style={{ fontWeight: 700, fontSize: '1.05rem', color: 'var(--color-text-primary)' }}>{res.id}</span>
                        <span className={`code-config-resource-item__badge code-config-resource-item__badge--${isNative ? 'native' : 'thick'}`}>
                          {isNative ? 'Native 推理' : `${res.driver.toUpperCase()} Thick Agent`}
                        </span>
                        {isDefault && (
                          <span className="code-config-badge code-config-badge--blue">默认资源</span>
                        )}
                        <span className="code-config-label-hint">并发配额: {res.concurrent}</span>
                      </div>

                      <div style={{ display: 'flex', gap: '0.5rem' }}>
                        <button type="button" className="btn btn-secondary" style={{ padding: '0.25rem 0.6rem', fontSize: '0.8rem' }} onClick={() => openEditResource(index)}>
                          编辑
                        </button>
                        <button
                          type="button"
                          className="btn btn-danger"
                          style={{ padding: '0.25rem 0.6rem', fontSize: '0.8rem' }}
                          onClick={() => handleDeleteResource(index)}
                          disabled={isDefault}
                        >
                          删除
                        </button>
                      </div>
                    </div>

                    {/* 端点明细与 Ping 测试 */}
                    {isNative && res.endpoints && res.endpoints.length > 0 ? (
                      <div style={{ marginTop: '0.75rem', display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
                        {res.endpoints.map(ep => {
                          const pingKey = `${res.id}-${ep.name}`;
                          const pingInfo = pingStates[pingKey];

                          return (
                            <div key={ep.name} style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', background: 'var(--color-bg-muted)', padding: '0.5rem 0.75rem', borderRadius: 'var(--radius-sm, 6px)', fontSize: '0.85rem' }}>
                              <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', flexWrap: 'wrap' }}>
                                <span style={{ fontWeight: 600 }}>{ep.name}:</span>
                                <code style={{ color: 'var(--color-text-secondary)' }}>{ep.base_url}</code>
                                <span className="code-config-badge code-config-badge--neutral">模型: {ep.model}</span>
                                <span className="code-config-label-hint">并发: {ep.concurrent}</span>
                              </div>

                              <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                                {pingInfo?.loading && <span className="code-config-label-hint">测速中...</span>}
                                {pingInfo?.latency_ms !== undefined && (
                                  <span className={`code-config-badge ${pingInfo.latency_ms < 300 ? 'code-config-badge--blue' : 'code-config-badge--neutral'}`} style={{ color: 'var(--color-success)' }}>
                                    ● {pingInfo.latency_ms}ms
                                  </span>
                                )}
                                {pingInfo?.error && (
                                  <span className="code-config-badge" style={{ color: 'var(--color-danger)' }}>
                                    ✖ {pingInfo.error}
                                  </span>
                                )}
                                <button
                                  type="button"
                                  className="btn btn-secondary"
                                  style={{ padding: '0.15rem 0.5rem', fontSize: '0.75rem' }}
                                  disabled={pingInfo?.loading}
                                  onClick={() => handlePingEndpoint(pingKey, ep.base_url, ep.api_key, ep.model)}
                                >
                                  Ping
                                </button>
                              </div>
                            </div>
                          );
                        })}
                      </div>
                    ) : (
                      <div style={{ marginTop: '0.5rem', fontSize: '0.85rem', color: 'var(--color-text-secondary)' }}>
                        模型标识: {res.model || '(继承底层 Agent 全局环境配置)'}
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
          </div>
        </div>
      )}

      {/* Tab 2: 辩论流水线编排 */}
      {activeTab === 'pipeline' && (
        <div className="code-config-panel">
          <div className="code-config-card">
            <h3 className="code-config-card__title">辩论流水线 4 阶梯算力池化编排</h3>
            <p className="code-config-card__desc">
              严格遵循架构分层规范：Tier 1/3 强制绑定 Thick Agent 节点，Tier 2/4 强制绑定 Native 推理节点。勾选多个节点即开启同类型池化调度。
            </p>

            <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem', marginTop: '1.25rem' }}>
              {TIER_DEFS.map(def => {
                const binding: TierBinding = tiersConfig[def.key] || { timeout_seconds: 1200 };
                const selectedResources = binding.resources && binding.resources.length > 0
                  ? binding.resources
                  : (binding.resource ? [binding.resource] : []);

                // 候选节点过滤
                const candidateResources = llmConfig.resources.filter(r =>
                  def.roleType === 'thick' ? r.driver !== 'native' : r.driver === 'native'
                );

                const isTimeoutInverted = !!(binding.attempt_timeout_seconds && binding.attempt_timeout_seconds > binding.timeout_seconds);

                return (
                  <div key={def.key} className="code-config-tier-card">
                    <div className="code-config-tier-card__header">
                      <div className="code-config-tier-card__title-group">
                        <div className="code-config-tier-card__title-row">
                          <span className={`code-config-tier-card__badge ${def.badgeClass}`}>{def.badge}</span>
                          <span className="code-config-tier-card__title">{def.title}</span>
                          <span className={`code-config-tier-card__role-tag ${def.roleType === 'thick' ? 'code-config-tier-card__role-tag--thick-required' : 'code-config-tier-card__role-tag--native-required'}`}>
                            {def.roleTag}
                          </span>
                        </div>
                        <span className="code-config-tier-card__desc">{def.description}</span>
                      </div>

                      <div className="code-config-tier-card__slots">
                        已选节点: {selectedResources.length} 个
                      </div>
                    </div>

                    {/* 节点池化多选胶囊 */}
                    <div style={{ marginTop: '0.85rem' }}>
                      <div className="code-config-label" style={{ marginBottom: '0.4rem', fontSize: '0.8rem' }}>
                        池化节点选择 (点击切换勾选):
                      </div>
                      {candidateResources.length === 0 ? (
                        <div className="code-config-tier-notice code-config-tier-notice--warning">
                          尚未配置符合条件的 {def.roleType === 'thick' ? 'Thick Agent' : 'Native'} 算力节点，请先在供给池中添加。
                        </div>
                      ) : (
                        <div className="code-config-tier-card__btn-group">
                          {candidateResources.map(r => {
                            const isSelected = selectedResources.includes(r.id);
                            return (
                              <button
                                key={r.id}
                                type="button"
                                className={`code-config-tier-res-btn ${isSelected ? 'code-config-tier-res-btn--selected' : ''}`}
                                onClick={() => handleToggleTierResource(def.key, r.id)}
                              >
                                <span>{isSelected ? '✓ ' : '+ '}{r.id}</span>
                                <span className="code-config-tier-res-btn__driver-tag">{r.driver}</span>
                              </button>
                            );
                          })}
                        </div>
                      )}
                    </div>

                    {/* 超时控制与倒挂校验 */}
                    <div className="code-config-grid-2" style={{ marginTop: '0.85rem' }}>
                      <div className="code-config-field" style={{ margin: 0 }}>
                        <label className="code-config-label">
                          阶段总预算超时 (timeout_seconds):
                          <span className="code-config-label-hint">单位: 秒</span>
                        </label>
                        <input
                          type="number"
                          className="code-config-input"
                          value={binding.timeout_seconds || 1200}
                          onChange={e => handleTierTimeoutChange(def.key, 'timeout_seconds', parseInt(e.target.value, 10) || 1200)}
                        />
                      </div>

                      <div className="code-config-field" style={{ margin: 0 }}>
                        <label className="code-config-label">
                          单次尝试超时 (attempt_timeout_seconds):
                          <span className="code-config-label-hint">单位: 秒</span>
                        </label>
                        <input
                          type="number"
                          className="code-config-input"
                          value={binding.attempt_timeout_seconds || 600}
                          onChange={e => handleTierTimeoutChange(def.key, 'attempt_timeout_seconds', parseInt(e.target.value, 10) || 600)}
                        />
                      </div>
                    </div>

                    {isTimeoutInverted && (
                      <div className="code-config-tier-notice code-config-tier-notice--warning" style={{ marginTop: '0.5rem' }}>
                        ⚠️ 约束冲突：单次尝试超时 ({binding.attempt_timeout_seconds}s) 不能大于阶段总超时 ({binding.timeout_seconds}s)。
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
          </div>
        </div>
      )}

      {/* 节点编辑 / 新增 Drawer */}
      {drawerOpen && (
        <Drawer
          open={drawerOpen}
          onClose={() => setDrawerOpen(false)}
          title={editingIndex === null ? '添加算力节点' : `编辑算力节点: ${editingResource.id}`}
          subtitle="配置推理节点驱动类型、物理端点与并发吞吐额度"
          width="520px"
          footer={
            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.75rem', width: '100%' }}>
              <button type="button" className="btn btn-secondary" onClick={() => setDrawerOpen(false)}>
                取消
              </button>
              <button type="button" className="btn btn-primary" onClick={handleSaveDrawerResource}>
                确定保存
              </button>
            </div>
          }
        >
          <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem', padding: '0.5rem 0' }}>
            <div className="code-config-field">
              <label className="code-config-label">节点 ID (唯一标识):</label>
              <input
                type="text"
                className="code-config-input"
                disabled={editingIndex !== null}
                value={editingResource.id}
                onChange={e => setEditingResource(prev => ({ ...prev, id: e.target.value }))}
                placeholder="例如: native-1, agy-1"
              />
            </div>

            <div className="code-config-field">
              <label className="code-config-label">驱动架构类型:</label>
              <select
                className="code-config-select"
                value={editingResource.driver}
                onChange={e => {
                  const drv = e.target.value;
                  setEditingResource(prev => ({
                    ...prev,
                    driver: drv,
                    endpoints: drv === 'native' ? (prev.endpoints && prev.endpoints.length > 0 ? prev.endpoints : [
                      { name: 'primary', base_url: 'http://127.0.0.1:8000/v1', api_key: '', model: 'glm-4-flash', concurrent: 5 }
                    ]) : undefined
                  }));
                }}
              >
                <option value="native">Native (直接调用 OpenAI 兼容端点)</option>
                <option value="agy">AGY (Antigravity 智能体容器)</option>
                <option value="opencode">OpenCode (自主代码分析智能体)</option>
                <option value="claude">Claude (Anthropic 命令行/Agent)</option>
                <option value="codex">Codex (CLI 补丁生成智能体)</option>
              </select>
            </div>

            <div className="code-config-field">
              <label className="code-config-label">并发配额上限 (Concurrent):</label>
              <input
                type="number"
                min={1}
                max={100}
                className="code-config-input"
                value={editingResource.concurrent}
                onChange={e => setEditingResource(prev => ({ ...prev, concurrent: Math.max(1, parseInt(e.target.value, 10) || 1) }))}
              />
            </div>

            {editingResource.driver === 'native' ? (
              <div style={{ borderTop: '1px solid var(--color-border-primary)', paddingTop: '1rem' }}>
                <h4 style={{ margin: '0 0 0.5rem 0', fontSize: '0.95rem' }}>Native 端点列表配置</h4>
                {(editingResource.endpoints || []).map((ep, epIdx) => (
                  <div key={epIdx} style={{ background: 'var(--color-bg-muted)', padding: '0.75rem', borderRadius: 'var(--radius-sm, 6px)', display: 'flex', flexDirection: 'column', gap: '0.5rem', marginBottom: '0.75rem' }}>
                    <div className="code-config-grid-2">
                      <div className="code-config-field" style={{ margin: 0 }}>
                        <label className="code-config-label">端点名称:</label>
                        <input
                          type="text"
                          className="code-config-input"
                          value={ep.name}
                          onChange={e => {
                            const val = e.target.value;
                            setEditingResource(prev => {
                              const eps = [...(prev.endpoints || [])];
                              eps[epIdx] = { ...eps[epIdx], name: val };
                              return { ...prev, endpoints: eps };
                            });
                          }}
                        />
                      </div>

                      <div className="code-config-field" style={{ margin: 0 }}>
                        <label className="code-config-label">模型标识 (Model):</label>
                        <input
                          type="text"
                          className="code-config-input"
                          value={ep.model}
                          onChange={e => {
                            const val = e.target.value;
                            setEditingResource(prev => {
                              const eps = [...(prev.endpoints || [])];
                              eps[epIdx] = { ...eps[epIdx], model: val };
                              return { ...prev, endpoints: eps };
                            });
                          }}
                        />
                      </div>
                    </div>

                    <div className="code-config-field" style={{ margin: 0 }}>
                      <label className="code-config-label">Base URL (OpenAI 兼容):</label>
                      <input
                        type="text"
                        className="code-config-input"
                        value={ep.base_url}
                        onChange={e => {
                          const val = e.target.value;
                          setEditingResource(prev => {
                            const eps = [...(prev.endpoints || [])];
                            eps[epIdx] = { ...eps[epIdx], base_url: val };
                            return { ...prev, endpoints: eps };
                          });
                        }}
                      />
                    </div>

                    <div className="code-config-field" style={{ margin: 0 }}>
                      <label className="code-config-label">API Key (留空或保持脱敏则不覆盖):</label>
                      <input
                        type="password"
                        className="code-config-input"
                        placeholder="sk-****"
                        value={ep.api_key}
                        onChange={e => {
                          const val = e.target.value;
                          setEditingResource(prev => {
                            const eps = [...(prev.endpoints || [])];
                            eps[epIdx] = { ...eps[epIdx], api_key: val };
                            return { ...prev, endpoints: eps };
                          });
                        }}
                      />
                    </div>
                  </div>
                ))}
              </div>
            ) : (
              <div className="code-config-field">
                <label className="code-config-label">Agent 默认模型标识 (可选):</label>
                <input
                  type="text"
                  className="code-config-input"
                  value={editingResource.model || ''}
                  placeholder="可留空使用环境默认值"
                  onChange={e => setEditingResource(prev => ({ ...prev, model: e.target.value }))}
                />
              </div>
            )}
          </div>
        </Drawer>
      )}
    </div>
  );
}
