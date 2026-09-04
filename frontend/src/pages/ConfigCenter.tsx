import React, { useState, useEffect, useCallback } from 'react';
import { useParams, useNavigate, useSearchParams } from 'react-router-dom';
import { useToast } from '../components/Toast';
import { Drawer } from '@code/common';
import { appNavigatePath } from '../config';
import {
  LLMConfig,
  ScannerConfig,
  GovernancePolicyConfig,
  NotificationConfig,
  ComputeResource,
  ResourceEndpoint,
  PingResult
} from '../types/config';
import './ConfigCenter.css';

type ConfigCategory = 'llm' | 'scanner' | 'governance' | 'notification';
const VALID_TABS: ConfigCategory[] = ['llm', 'scanner', 'governance', 'notification'];

export default function ConfigCenter() {
  const { showToast } = useToast();
  const { tab } = useParams<{ tab: string }>();
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();

  const resolveTab = useCallback((rawTab?: string): ConfigCategory => {
    if (rawTab && VALID_TABS.includes(rawTab as ConfigCategory)) {
      return rawTab as ConfigCategory;
    }
    const queryTab = searchParams.get('tab');
    if (queryTab && VALID_TABS.includes(queryTab as ConfigCategory)) {
      return queryTab as ConfigCategory;
    }
    return 'llm';
  }, [searchParams]);

  const [activeTab, setActiveTab] = useState<ConfigCategory>(() => resolveTab(tab));

  useEffect(() => {
    const currentResolved = resolveTab(tab);
    if (currentResolved !== activeTab) {
      setActiveTab(currentResolved);
    }
  }, [tab, resolveTab, activeTab]);

  const handleTabChange = (newTab: ConfigCategory) => {
    if (newTab === activeTab) return;
    setActiveTab(newTab);
    navigate(appNavigatePath(`/admin/config/${newTab}`));
  };

  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [resetting, setResetting] = useState(false);

  // 全局 4 个模块的数据
  const [llmConfig, setLlmConfig] = useState<LLMConfig>({
    default_resource: 'native',
    debug_logs: false,
    resources: []
  });

  const [scannerConfig, setScannerConfig] = useState<ScannerConfig>({
    worker_count: 5,
    max_queue_size: 2000,
    mock_on_missing_cli: true,
    throttling: {
      work_hours: {
        enabled: false,
        workdays: [1, 2, 3, 4, 5],
        start_time: '09:00',
        end_time: '22:00',
        scale: 0.1
      }
    },
    debate: {
      enabled: true,
      fast_pass_enabled: true,
      max_candidates_per_chunk: 30,
      stage_timeout_seconds: 600,
      log_retention_days: 30,
      backpressure_threshold: 10,
      backpressure_timeout_seconds: 300,
      tiers: {
        tier1_hunter: { resource: 'native', timeout_seconds: 600 },
        tier2_reasoning: { resource: 'native', timeout_seconds: 900 },
        tier3_synthesis: { resource: 'native', timeout_seconds: 600 }
      }
    },
    tools: {
      default_resource: 'native',
      overrides: {}
    }
  });

  const [govConfig, setGovConfig] = useState<GovernancePolicyConfig>({
    fingerprint: {
      enabled: true,
      similarity_threshold: 0.85
    },
    lifecycle: {
      scope_guard_enabled: true,
      auto_resolve_missing: true,
      diff_gate_strict: false
    },
    feedback_memory: {
      injection_enabled: true,
      max_rules_injected: 10
    }
  });

  const [notifConfig, setNotifConfig] = useState<NotificationConfig>({
    webhook: ''
  });

  // 明文可见状态与 Ping 状态
  const [showSecrets, setShowSecrets] = useState<Record<string, boolean>>({});
  const [pingStates, setPingStates] = useState<Record<string, { loading: boolean; result?: PingResult }>>({});

  // Endpoint 抽屉编辑状态
  const [editingEndpoint, setEditingEndpoint] = useState<{ resIdx: number; epIdx: number | null; data: ResourceEndpoint } | null>(null);

  // 拉取全量配置
  const fetchFullConfig = useCallback(async () => {
    setLoading(true);
    try {
      const res = await fetch('/api/admin/config/full');
      if (res.ok) {
        const data = await res.json();
        if (data.llm) setLlmConfig(data.llm);
        if (data.scanner) setScannerConfig(data.scanner);
        if (data.governance) setGovConfig(data.governance);
        if (data.notification) setNotifConfig(data.notification);
      } else {
        showToast('获取系统配置失败: ' + res.statusText, 'error');
      }
    } catch (err: any) {
      showToast('加载配置发生网络异常: ' + err.message, 'error');
    } finally {
      setLoading(false);
    }
  }, [showToast]);

  useEffect(() => {
    fetchFullConfig();
  }, [fetchFullConfig]);

  const toggleSecret = (key: string) => {
    setShowSecrets(prev => ({ ...prev, [key]: !prev[key] }));
  };

  // 保存当前激活 Tab 对应的模块配置 (细粒度 PUT API)
  const handleSaveCurrentModule = async () => {
    setSaving(true);
    let payload: any = null;
    switch (activeTab) {
      case 'llm':
        payload = llmConfig;
        break;
      case 'scanner':
        payload = scannerConfig;
        break;
      case 'governance':
        payload = govConfig;
        break;
      case 'notification':
        payload = notifConfig;
        break;
    }

    try {
      const res = await fetch(`/api/admin/config/category/${activeTab}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
      if (res.ok) {
        showToast(`已成功保存【${getTabLabel(activeTab)}】配置并实时生效！`, 'success');
      } else {
        const data = await res.json().catch(() => ({}));
        showToast('保存失败: ' + (data.error || res.statusText), 'error');
      }
    } catch (err: any) {
      showToast('保存请求异常: ' + err.message, 'error');
    } finally {
      setSaving(false);
    }
  };

  // 重置当前模块为初始 config.yaml 模版
  const handleResetToSeed = async () => {
    if (!window.confirm(`确定要将【${getTabLabel(activeTab)}】配置重置为 config.yaml 的初始默认值吗？`)) {
      return;
    }
    setResetting(true);
    try {
      const res = await fetch('/api/admin/config/reset-to-seed', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ category: activeTab })
      });
      if (res.ok) {
        showToast(`已成功重置【${getTabLabel(activeTab)}】为默认模版！`, 'success');
        await fetchFullConfig();
      } else {
        const data = await res.json().catch(() => ({}));
        showToast('重置失败: ' + (data.error || res.statusText), 'error');
      }
    } catch (err: any) {
      showToast('重置异常: ' + err.message, 'error');
    } finally {
      setResetting(false);
    }
  };

  // Ping API 测速
  const handlePingEndpoint = async (key: string, baseURL: string, apiKey: string, model: string) => {
    setPingStates(prev => ({ ...prev, [key]: { loading: true } }));
    try {
      const res = await fetch('/api/admin/config/ping-endpoint', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          base_url: baseURL,
          api_key: apiKey,
          model: model
        })
      });
      const data: PingResult = await res.json();
      setPingStates(prev => ({ ...prev, [key]: { loading: false, result: data } }));
      if (data.success) {
        showToast(`端点探测成功: 延迟 ${data.latency_ms}ms`, 'success');
      } else {
        showToast(`端点探测失败: ${data.message}`, 'error');
      }
    } catch (err: any) {
      setPingStates(prev => ({
        ...prev,
        [key]: { loading: false, result: { success: false, message: err.message } }
      }));
      showToast('探测端点网络异常: ' + err.message, 'error');
    }
  };

  // LLM 资源列表操作
  const handleAddResource = () => {
    const newRes: ComputeResource = {
      id: `resource-${llmConfig.resources.length + 1}`,
      driver: 'native',
      model: 'glm-4-flash',
      concurrent: 10,
      base_url: 'http://192.168.56.18:8000/v1',
      api_key: '',
      response_format_json: false,
      max_retries: 3,
      retry_backoff_ms: 1000,
      endpoints: []
    };
    setLlmConfig(prev => ({ ...prev, resources: [...prev.resources, newRes] }));
  };

  const handleRemoveResource = (index: number) => {
    if (!window.confirm('确定要删除此算力资源节点吗？')) return;
    setLlmConfig(prev => {
      const next = [...prev.resources];
      next.splice(index, 1);
      return { ...prev, resources: next };
    });
  };

  const updateResource = (index: number, patch: Partial<ComputeResource>) => {
    setLlmConfig(prev => {
      const next = [...prev.resources];
      next[index] = { ...next[index], ...patch };
      return { ...prev, resources: next };
    });
  };

  // Endpoint 抽屉操作
  const handleOpenAddEndpoint = (resIdx: number) => {
    setEditingEndpoint({
      resIdx,
      epIdx: null,
      data: {
        name: `端点-${(llmConfig.resources[resIdx].endpoints?.length || 0) + 1}`,
        base_url: 'http://192.168.56.18:8000/v1',
        api_key: '',
        model: 'glm-4-flash',
        concurrent: 10,
        weight: 100,
        temperature: 0.1
      }
    });
  };

  const handleSaveEndpoint = () => {
    if (!editingEndpoint) return;
    const { resIdx, epIdx, data } = editingEndpoint;
    setLlmConfig(prev => {
      const next = [...prev.resources];
      const res = { ...next[resIdx] };
      const eps = [...(res.endpoints || [])];
      if (epIdx === null) {
        eps.push(data);
      } else {
        eps[epIdx] = data;
      }
      res.endpoints = eps;
      next[resIdx] = res;
      return { ...prev, resources: next };
    });
    setEditingEndpoint(null);
  };

  const handleDeleteEndpoint = (resIdx: number, epIdx: number) => {
    setLlmConfig(prev => {
      const next = [...prev.resources];
      const res = { ...next[resIdx] };
      const eps = [...(res.endpoints || [])];
      eps.splice(epIdx, 1);
      res.endpoints = eps;
      next[resIdx] = res;
      return { ...prev, resources: next };
    });
  };

  const getTabLabel = (t: ConfigCategory) => {
    switch (t) {
      case 'llm': return '大模型与算力池';
      case 'scanner': return '扫描引擎与流水线';
      case 'governance': return '质量治理与门禁';
      case 'notification': return '通知服务';
    }
  };

  // 智能体辩论各阶梯元数据与选型建议规范
  const TIER_METAS: Record<'tier1_hunter' | 'tier2_reasoning' | 'tier3_synthesis', {
    tierNumber: string;
    roleTitle: string;
    badgeModifier: string;
    engineBadgeText: string;
    engineReqType: 'thick-required' | 'thin-recommended' | 'thin-suggested';
    desc: string;
    recSnippet: string;
    defaultSeconds: number;
  }> = {
    tier1_hunter: {
      tierNumber: 'Tier 1',
      roleTitle: 'Hunter 源码初筛',
      badgeModifier: 'code-config-tier-card__badge--tier1_hunter',
      engineBadgeText: '必须 Thick Agent',
      engineReqType: 'thick-required',
      desc: '遍历文件树与跨文件调用链，自主阅读磁盘源码并生成初筛案卷。',
      recSnippet: '必须 Thick (如 agy / opencode)，纯 Thin 节点无法读取磁盘文件。',
      defaultSeconds: 1200,
    },
    tier2_reasoning: {
      tierNumber: 'Tier 2',
      roleTitle: 'Challenger & Judge 深度推理',
      badgeModifier: 'code-config-tier-card__badge--tier2_reasoning',
      engineBadgeText: '强烈推荐 Thin LLM',
      engineReqType: 'thin-recommended',
      desc: '案卷代码已全量内联，负责反向质询辩护与终审事实仲裁，纯逻辑推演。',
      recSnippet: '强烈推荐 native (Thin)，案卷代码已内联；推荐配 agy (Thick) 容灾备选。',
      defaultSeconds: 1800,
    },
    tier3_synthesis: {
      tierNumber: 'Tier 3',
      roleTitle: 'Synthesis 终审汇总',
      badgeModifier: 'code-config-tier-card__badge--tier3_synthesis',
      engineBadgeText: '推荐 Thin LLM',
      engineReqType: 'thin-suggested',
      desc: '全仓确诊缺陷聚合、态势评分与 Markdown / JSON 排版直传直出。',
      recSnippet: '推荐 native (Thin)，内存数据直传直出，消除 CLI 进程冷启动开销。',
      defaultSeconds: 300,
    },
  };

  // 辅助渲染各阶段算力资源池多选选择器
  const renderTierResourcePoolSelector = (
    tierKey: 'tier1_hunter' | 'tier2_reasoning' | 'tier3_synthesis'
  ) => {
    const meta = TIER_METAS[tierKey];
    const tierItem = scannerConfig.debate.tiers?.[tierKey] || { resource: 'native', timeout_seconds: meta.defaultSeconds };
    const selected = (tierItem.resources && tierItem.resources.length > 0)
      ? tierItem.resources
      : (tierItem.resource ? [tierItem.resource] : []);

    let totalSlots = 0;
    let hasThick = false;
    let hasThin = false;

    selected.forEach(id => {
      const res = llmConfig.resources.find(r => r.id === id);
      const isThin = res ? res.driver === 'native' : id === 'native';
      if (isThin) {
        hasThin = true;
      } else {
        hasThick = true;
      }
      if (res) totalSlots += (res.concurrent || 5);
      else totalSlots += 5;
    });

    const toggleRes = (resId: string) => {
      let next: string[];
      if (selected.includes(resId)) {
        if (selected.length === 1) {
          showToast('至少需要保留 1 个算力节点', 'warning');
          return;
        }
        next = selected.filter(x => x !== resId);
      } else {
        next = [...selected, resId];
      }
      setScannerConfig({
        ...scannerConfig,
        debate: {
          ...scannerConfig.debate,
          tiers: {
            ...scannerConfig.debate.tiers,
            [tierKey]: {
              ...tierItem,
              resource: next[0] || '',
              resources: next,
            }
          }
        }
      });
    };

    let diagnosticNotice: { type: 'warning' | 'info' | 'success'; message: string } | null = null;
    if (tierKey === 'tier1_hunter') {
      if (!hasThick) {
        diagnosticNotice = {
          type: 'warning',
          message: '⚠️ 选型告警：当前未绑定任何 Thick Agent（如 agy / opencode）。Hunter 初筛需自主遍历工作区读取磁盘源码，纯 Thin 模式将导致扫描无法读取文件，请务必勾选 Thick 节点！'
        };
      }
    } else if (tierKey === 'tier2_reasoning') {
      if (!hasThin) {
        diagnosticNotice = {
          type: 'info',
          message: '💡 架构建议：案卷代码已全量内联，建议勾选 native (Thin LLM) 获得 10x 吞吐与毫秒级延迟；可保留 agy 等作为削峰容灾备选池。'
        };
      } else if (hasThick) {
        diagnosticNotice = {
          type: 'success',
          message: '✅ 最佳实践组合：已同时绑定 Thin 纯推理 (native) 与 Thick Agent (agy)，兼具高吞吐纯推理与弹性容灾保障。'
        };
      }
    } else if (tierKey === 'tier3_synthesis') {
      if (!hasThin) {
        diagnosticNotice = {
          type: 'info',
          message: '💡 架构建议：推荐勾选 native (Thin LLM) 进行内存报告快速排版汇总，消除 CLI 进程冷启动开销。'
        };
      }
    }

    return (
      <div className="code-config-tier-card">
        <div className="code-config-tier-card__header">
          <div className="code-config-tier-card__title-group">
            <div className="code-config-tier-card__title-row">
              <span className={`code-config-tier-card__badge ${meta.badgeModifier}`}>
                {meta.tierNumber}
              </span>
              <strong className="code-config-tier-card__title">{meta.roleTitle}</strong>
              <span className={`code-config-tier-card__role-tag code-config-tier-card__role-tag--${meta.engineReqType}`}>
                {meta.engineBadgeText}
              </span>
            </div>
            <p className="code-config-tier-card__desc">{meta.desc}</p>
          </div>
          {selected.length > 1 && (
            <span className="code-config-tier-card__slots">
              池化: {totalSlots} 槽
            </span>
          )}
        </div>

        <div className="code-config-field">
          <label className="code-config-label">
            绑定算力资源 (多选负载打散)
          </label>
          <div className="code-config-tier-card__btn-group">
            {llmConfig.resources.map(r => {
              const isChecked = selected.includes(r.id);
              const isThin = r.driver === 'native';
              const isRecommended = (tierKey === 'tier2_reasoning' && isThin) ||
                                    (tierKey === 'tier1_hunter' && !isThin) ||
                                    (tierKey === 'tier3_synthesis' && isThin);
              return (
                <button
                  type="button"
                  key={r.id}
                  onClick={() => toggleRes(r.id)}
                  className={`code-config-tier-res-btn ${isChecked ? 'code-config-tier-res-btn--selected' : ''}`}
                >
                  <span>{isChecked ? '✓' : '+'}</span>
                  <span>{r.id}</span>
                  <span className="code-config-tier-res-btn__driver-tag">
                    {isThin ? 'Thin' : 'Thick'} · {r.concurrent || 5}槽
                  </span>
                  {isRecommended && (
                    <span className={tierKey === 'tier1_hunter' ? 'code-config-tier-res-btn__req-tag' : 'code-config-tier-res-btn__rec-tag'}>
                      {tierKey === 'tier1_hunter' ? '必选' : '推荐'}
                    </span>
                  )}
                </button>
              );
            })}
          </div>
        </div>

        {diagnosticNotice && (
          <div className={`code-config-tier-notice code-config-tier-notice--${diagnosticNotice.type}`}>
            {diagnosticNotice.message}
          </div>
        )}

        <div className="code-config-field" style={{ marginTop: 'auto' }}>
          <label className="code-config-label">超时时间 (秒)</label>
          <input
            type="number"
            className="code-config-input"
            value={tierItem.timeout_seconds}
            onChange={e => setScannerConfig({
              ...scannerConfig,
              debate: {
                ...scannerConfig.debate,
                tiers: {
                  ...scannerConfig.debate.tiers,
                  [tierKey]: { ...tierItem, timeout_seconds: parseInt(e.target.value) || meta.defaultSeconds }
                }
              }
            })}
          />
        </div>
      </div>
    );
  };

  if (loading) {
    return (
      <div className="code-config-container" style={{ alignItems: 'center', justifyContent: 'center', minHeight: '300px' }}>
        <div style={{ color: 'var(--color-text-secondary)', fontSize: '0.9rem' }}>正在加载系统动态配置数据...</div>
      </div>
    );
  }

  return (
    <div className="code-config-container">
      {/* 顶部 Header */}
      <div className="code-config-header">
        <div className="code-config-header__titles">
          <h1 className="code-config-header__title">
            <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <rect x="2" y="3" width="20" height="14" rx="2" ry="2" />
              <line x1="8" y1="21" x2="16" y2="21" />
              <line x1="12" y1="17" x2="12" y2="21" />
            </svg>
            系统动态配置中心
          </h1>
          <p className="code-config-header__subtitle">
            全系统集中式动态配置总线 (SSOT)。管理底层 LLM 算力池、任务 Worker 线程、对抗辩论流水线与企业治理门禁。
            修改即刻写入数据库并自动触发 Dispatcher 热生效，无需重启服务进程。
          </p>
        </div>

        <div className="code-config-header__actions">
          <button
            className="btn btn-secondary"
            onClick={handleResetToSeed}
            disabled={resetting || saving}
            title="将当前模块重置为 config.yaml 的初始状态"
          >
            {resetting ? '重置中...' : '重置为初始模版'}
          </button>
          <button
            className="btn btn-primary"
            onClick={handleSaveCurrentModule}
            disabled={saving}
          >
            {saving ? '保存中...' : `保存【${getTabLabel(activeTab)}】`}
          </button>
        </div>
      </div>

      {/* Tab 导航 */}
      <div className="code-config-tabs">
        <button
          className={`code-config-tab-btn ${activeTab === 'llm' ? 'active' : ''}`}
          onClick={() => handleTabChange('llm')}
        >
          🤖 大模型与算力池
        </button>
        <button
          className={`code-config-tab-btn ${activeTab === 'scanner' ? 'active' : ''}`}
          onClick={() => handleTabChange('scanner')}
        >
          ⚙️ 扫描引擎与流水线
        </button>
        <button
          className={`code-config-tab-btn ${activeTab === 'governance' ? 'active' : ''}`}
          onClick={() => handleTabChange('governance')}
        >
          🛡️ 质量治理与门禁
        </button>
        <button
          className={`code-config-tab-btn ${activeTab === 'notification' ? 'active' : ''}`}
          onClick={() => handleTabChange('notification')}
        >
          🔔 通知服务
        </button>
      </div>

      {/* Tab 1: 大模型与算力池 */}
      {activeTab === 'llm' && (
        <div className="code-config-panel">
          {/* 全局设置卡片 */}
          <div className="code-config-card">
            <div className="code-config-card__header">
              <h3 className="code-config-card__title">算力调度全局策略</h3>
            </div>
            <div className="code-config-grid-2">
              <div className="code-config-field">
                <label className="code-config-label">
                  默认兜底算力资源 (Default Resource)
                  <span className="code-config-label-hint">未指定阶梯时的默认选择</span>
                </label>
                <select
                  className="code-config-select"
                  value={llmConfig.default_resource}
                  onChange={e => setLlmConfig({ ...llmConfig, default_resource: e.target.value })}
                >
                  {llmConfig.resources.map(r => (
                    <option key={r.id} value={r.id}>{r.id} ({r.driver} / {r.model})</option>
                  ))}
                  {llmConfig.resources.length === 0 && <option value="native">native</option>}
                </select>
              </div>

              <div className="code-config-switch-row" style={{ alignSelf: 'flex-end', height: '42px' }}>
                <div className="code-config-switch-info">
                  <span className="code-config-switch-title">底层调用调试日志 (Debug Logs)</span>
                  <span className="code-config-switch-desc">打印完整 HTTP 请求体与模型 Raw 返回</span>
                </div>
                <label className="code-config-toggle">
                  <input
                    type="checkbox"
                    checked={llmConfig.debug_logs}
                    onChange={e => setLlmConfig({ ...llmConfig, debug_logs: e.target.checked })}
                  />
                  <span className="code-config-toggle__slider" />
                </label>
              </div>
            </div>
          </div>

          {/* 算力节点列表 */}
          <div className="code-config-card">
            <div className="code-config-card__header">
              <div>
                <h3 className="code-config-card__title">算力节点集群 (Compute Resources)</h3>
                <span className="code-config-card__desc">
                  配置各物理/逻辑 LLM 服务器端点、并发槽位数与加权分流。支持 Thin LLM（HTTP REST 纯推理）与 Thick Agent（自主探索型 CLI）混合供给。
                </span>
              </div>
              <button className="btn btn-secondary" onClick={handleAddResource}>
                + 添加算力节点
              </button>
            </div>

            <div className="code-config-resource-list">
              {llmConfig.resources.map((res, idx) => (
                <div key={idx} className="code-config-resource-item">
                  <div className="code-config-resource-item__top">
                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
                      <span className="code-config-resource-item__badge" style={{
                        background: res.driver === 'native' ? 'var(--color-success-subtle)' : 'var(--color-primary-subtle)',
                        color: res.driver === 'native' ? 'var(--color-success)' : 'var(--color-primary)',
                        border: `1px solid ${res.driver === 'native' ? 'var(--color-success-border)' : 'var(--color-primary-border)'}`
                      }}>
                        {res.driver === 'native' ? 'Thin · native' : `Thick · ${res.driver}`}
                      </span>
                      <strong style={{ fontSize: '1rem' }}>{res.id}</strong>
                      <span style={{ fontSize: '0.85rem', color: 'var(--color-text-secondary)' }}>模型: {res.model || '-'}</span>
                      <span style={{ fontSize: '0.85rem', color: 'var(--color-text-muted)' }}>并发槽位: {res.concurrent}</span>
                    </div>

                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                      {res.driver === 'native' && (
                        <button
                          className="btn btn-secondary"
                          style={{ fontSize: '0.8rem', padding: '0.3rem 0.6rem' }}
                          onClick={() => {
                            const key = `res-${idx}`;
                            handlePingEndpoint(key, res.base_url || '', res.api_key || '', res.model || '');
                          }}
                          disabled={pingStates[`res-${idx}`]?.loading}
                        >
                          {pingStates[`res-${idx}`]?.loading ? '探测中...' : '测试连接 (Ping API)'}
                        </button>
                      )}
                      <button
                        className="btn btn-danger"
                        style={{ fontSize: '0.8rem', padding: '0.3rem 0.6rem' }}
                        onClick={() => handleRemoveResource(idx)}
                      >
                        删除节点
                      </button>
                    </div>
                  </div>

                  {/* 基础属性网格 */}
                  <div className="code-config-grid-3">
                    <div className="code-config-field">
                      <label className="code-config-label">资源 ID (唯一标识)</label>
                      <input
                        className="code-config-input"
                        value={res.id}
                        onChange={e => updateResource(idx, { id: e.target.value })}
                        placeholder="例如: native, opencode-fast"
                      />
                    </div>

                    <div className="code-config-field">
                      <label className="code-config-label">驱动后端 (Driver)</label>
                      <select
                        className="code-config-select"
                        value={res.driver}
                        onChange={e => updateResource(idx, { driver: e.target.value })}
                      >
                        <option value="native">native (Thin LLM · HTTP REST 高并发纯推理)</option>
                        <option value="agy">agy (Thick Agent · Antigravity 探索型平台)</option>
                        <option value="opencode">opencode (Thick Agent · CLI 模式)</option>
                        <option value="claude">claude (Thick Agent · CLI 模式)</option>
                        <option value="codex">codex (Thick Agent · CLI 模式)</option>
                      </select>
                    </div>

                    <div className="code-config-field">
                      <label className="code-config-label">模型名 (Model)</label>
                      <input
                        className="code-config-input"
                        value={res.model}
                        onChange={e => updateResource(idx, { model: e.target.value })}
                        placeholder="例如: glm-4-flash, qwen-2.5-coder"
                      />
                    </div>

                    <div className="code-config-field">
                      <label className="code-config-label">并发槽位数 (Concurrent)</label>
                      <input
                        type="number"
                        className="code-config-input"
                        value={res.concurrent}
                        onChange={e => updateResource(idx, { concurrent: parseInt(e.target.value) || 1 })}
                      />
                    </div>

                    {res.driver === 'native' && (
                      <>
                        <div className="code-config-field" style={{ gridColumn: 'span 2' }}>
                          <label className="code-config-label">服务 Base URL</label>
                          <input
                            className="code-config-input"
                            value={res.base_url || ''}
                            onChange={e => updateResource(idx, { base_url: e.target.value })}
                            placeholder="例如: http://192.168.56.18:8000/v1"
                          />
                        </div>

                        <div className="code-config-field" style={{ gridColumn: 'span 2' }}>
                          <label className="code-config-label">
                            API Key (明文输入直存)
                            <span className="code-config-label-hint">内部私有部署，直接明文存储</span>
                          </label>
                          <div className="code-config-secret-box">
                            <input
                              type={showSecrets[`res-key-${idx}`] ? 'text' : 'password'}
                              className="code-config-secret-input"
                              value={res.api_key || ''}
                              onChange={e => updateResource(idx, { api_key: e.target.value })}
                              placeholder="Bearer 密钥或留空"
                            />
                            <button
                              type="button"
                              className="code-config-secret-toggle"
                              onClick={() => toggleSecret(`res-key-${idx}`)}
                              title="显示/隐藏明文"
                            >
                              {showSecrets[`res-key-${idx}`] ? '🙈' : '👁️'}
                            </button>
                          </div>
                        </div>

                        <div className="code-config-switch-row" style={{ gridColumn: 'span 1' }}>
                          <div className="code-config-switch-info">
                            <span className="code-config-switch-title">强制 JSON 模式</span>
                          </div>
                          <label className="code-config-toggle">
                            <input
                              type="checkbox"
                              checked={!!res.response_format_json}
                              onChange={e => updateResource(idx, { response_format_json: e.target.checked })}
                            />
                            <span className="code-config-toggle__slider" />
                          </label>
                        </div>
                      </>
                    )}
                  </div>

                  {/* Native 集群多端点加权分流子表 */}
                  {res.driver === 'native' && (
                    <div style={{ marginTop: '0.75rem', background: 'var(--color-bg-muted)', padding: '1rem', borderRadius: '8px' }}>
                      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.75rem' }}>
                        <div>
                          <strong style={{ fontSize: '0.9rem' }}>集群加权多端点分流 (Endpoints)</strong>
                          <span style={{ fontSize: '0.78rem', color: 'var(--color-text-muted)', marginLeft: '0.5rem' }}>
                            配置多个物理推理机，按相对权重实现平滑分流与故障容灾
                          </span>
                        </div>
                        <button
                          className="btn btn-secondary"
                          style={{ fontSize: '0.78rem', padding: '0.25rem 0.6rem' }}
                          onClick={() => handleOpenAddEndpoint(idx)}
                        >
                          + 添加集群端点
                        </button>
                      </div>

                      {res.endpoints && res.endpoints.length > 0 ? (
                        <table className="code-config-table">
                          <thead>
                            <tr>
                              <th>端点名称</th>
                              <th>Base URL</th>
                              <th>模型名</th>
                              <th>并发数</th>
                              <th>加权 (Weight)</th>
                              <th>测速状态</th>
                              <th style={{ textAlign: 'right' }}>操作</th>
                            </tr>
                          </thead>
                          <tbody>
                            {res.endpoints.map((ep, epIdx) => {
                              const pingKey = `res-${idx}-ep-${epIdx}`;
                              const pingInfo = pingStates[pingKey];
                              return (
                                <tr key={epIdx}>
                                  <td><strong>{ep.name}</strong></td>
                                  <td style={{ fontFamily: 'monospace', fontSize: '0.8rem' }}>{ep.base_url}</td>
                                  <td>{ep.model}</td>
                                  <td>{ep.concurrent}</td>
                                  <td>
                                    <span style={{ fontWeight: 600, color: 'var(--color-primary)' }}>{ep.weight}</span>
                                  </td>
                                  <td>
                                    {pingInfo?.loading ? (
                                      <span className="code-config-ping-badge code-config-ping-badge--testing">探测中...</span>
                                    ) : pingInfo?.result ? (
                                      <span className={`code-config-ping-badge ${pingInfo.result.success ? 'code-config-ping-badge--success' : 'code-config-ping-badge--error'}`}>
                                        {pingInfo.result.success ? `🟢 ${pingInfo.result.latency_ms}ms` : '🔴 失败'}
                                      </span>
                                    ) : (
                                      <span style={{ color: 'var(--color-text-muted)', fontSize: '0.75rem' }}>未测试</span>
                                    )}
                                  </td>
                                  <td style={{ textAlign: 'right' }}>
                                    <button
                                      className="btn btn-secondary"
                                      style={{ fontSize: '0.75rem', padding: '0.2rem 0.5rem', marginRight: '0.4rem' }}
                                      onClick={() => handlePingEndpoint(pingKey, ep.base_url, ep.api_key, ep.model)}
                                      disabled={pingInfo?.loading}
                                    >
                                      Ping
                                    </button>
                                    <button
                                      className="btn btn-secondary"
                                      style={{ fontSize: '0.75rem', padding: '0.2rem 0.5rem', marginRight: '0.4rem' }}
                                      onClick={() => setEditingEndpoint({ resIdx: idx, epIdx, data: { ...ep } })}
                                    >
                                      编辑
                                    </button>
                                    <button
                                      className="btn btn-danger"
                                      style={{ fontSize: '0.75rem', padding: '0.2rem 0.5rem' }}
                                      onClick={() => handleDeleteEndpoint(idx, epIdx)}
                                    >
                                      移除
                                    </button>
                                  </td>
                                </tr>
                              );
                            })}
                          </tbody>
                        </table>
                      ) : (
                        <div style={{ color: 'var(--color-text-muted)', fontSize: '0.8rem', textAlign: 'center', padding: '0.5rem' }}>
                          未配置集群多端点，将直接使用主节点的 Base URL 和 API Key 进行单节点调用。
                        </div>
                      )}
                    </div>
                  )}
                </div>
              ))}
            </div>
          </div>
        </div>
      )}

      {/* Tab 2: 扫描引擎与流水线 */}
      {activeTab === 'scanner' && (
        <div className="code-config-panel">
          {/* 任务调度器核心并发 */}
          <div className="code-config-card">
            <div className="code-config-card__header">
              <h3 className="code-config-card__title">调度器与并发控制</h3>
            </div>
            <div className="code-config-grid-3">
              <div className="code-config-field">
                <label className="code-config-label">
                  扫描任务并发工作线程 (Worker Count)
                  <span className="code-config-label-hint">{scannerConfig.worker_count} 个任务并发</span>
                </label>
                <input
                  type="number"
                  min="1"
                  max="64"
                  className="code-config-input"
                  value={scannerConfig.worker_count}
                  onChange={e => setScannerConfig({ ...scannerConfig, worker_count: parseInt(e.target.value) || 1 })}
                />
              </div>

              <div className="code-config-field">
                <label className="code-config-label">
                  排队队列容量上限 (Max Queue Size)
                  <span className="code-config-label-hint">超过将拒绝入队</span>
                </label>
                <input
                  type="number"
                  min="100"
                  max="10000"
                  className="code-config-input"
                  value={scannerConfig.max_queue_size}
                  onChange={e => setScannerConfig({ ...scannerConfig, max_queue_size: parseInt(e.target.value) || 1000 })}
                />
              </div>

              <div className="code-config-switch-row" style={{ alignSelf: 'flex-end', height: '42px' }}>
                <div className="code-config-switch-info">
                  <span className="code-config-switch-title">CLI 缺失模拟降级 (Mock On Missing)</span>
                  <span className="code-config-switch-desc">缺少 CLI 时模拟降级返回而不崩溃</span>
                </div>
                <label className="code-config-toggle">
                  <input
                    type="checkbox"
                    checked={scannerConfig.mock_on_missing_cli}
                    onChange={e => setScannerConfig({ ...scannerConfig, mock_on_missing_cli: e.target.checked })}
                  />
                  <span className="code-config-toggle__slider" />
                </label>
              </div>
            </div>
          </div>

          {/* 工作时间自动限流 */}
          <div className="code-config-card">
            <div className="code-config-card__header">
              <div>
                <h3 className="code-config-card__title">工作时间智能避峰限流 (Throttling)</h3>
                <span className="code-config-card__desc">在白天研发高峰期自动压缩并发槽位，把算力留给业务团队</span>
              </div>
              <label className="code-config-toggle">
                <input
                  type="checkbox"
                  checked={scannerConfig.throttling.work_hours.enabled}
                  onChange={e => setScannerConfig({
                    ...scannerConfig,
                    throttling: {
                      ...scannerConfig.throttling,
                      work_hours: { ...scannerConfig.throttling.work_hours, enabled: e.target.checked }
                    }
                  })}
                />
                <span className="code-config-toggle__slider" />
              </label>
            </div>

            {scannerConfig.throttling.work_hours.enabled && (
              <div className="code-config-grid-3">
                <div className="code-config-field">
                  <label className="code-config-label">避峰时段起始时间 (Start Time)</label>
                  <input
                    type="text"
                    className="code-config-input"
                    value={scannerConfig.throttling.work_hours.start_time}
                    onChange={e => setScannerConfig({
                      ...scannerConfig,
                      throttling: {
                        ...scannerConfig.throttling,
                        work_hours: { ...scannerConfig.throttling.work_hours, start_time: e.target.value }
                      }
                    })}
                    placeholder="例如: 09:00"
                  />
                </div>

                <div className="code-config-field">
                  <label className="code-config-label">避峰时段截止时间 (End Time)</label>
                  <input
                    type="text"
                    className="code-config-input"
                    value={scannerConfig.throttling.work_hours.end_time}
                    onChange={e => setScannerConfig({
                      ...scannerConfig,
                      throttling: {
                        ...scannerConfig.throttling,
                        work_hours: { ...scannerConfig.throttling.work_hours, end_time: e.target.value }
                      }
                    })}
                    placeholder="例如: 22:00"
                  />
                </div>

                <div className="code-config-field">
                  <label className="code-config-label">
                    限流并发比例 (Scale)
                    <span className="code-config-label-hint">{(scannerConfig.throttling.work_hours.scale * 100).toFixed(0)}% 正常算力</span>
                  </label>
                  <input
                    type="number"
                    step="0.05"
                    min="0.05"
                    max="1.0"
                    className="code-config-input"
                    value={scannerConfig.throttling.work_hours.scale}
                    onChange={e => setScannerConfig({
                      ...scannerConfig,
                      throttling: {
                        ...scannerConfig.throttling,
                        work_hours: { ...scannerConfig.throttling.work_hours, scale: parseFloat(e.target.value) || 0.1 }
                      }
                    })}
                  />
                </div>
              </div>
            )}
          </div>

          {/* 辩论流水线流控与阶梯绑定 */}
          <div className="code-config-card">
            <div className="code-config-card__header">
              <div>
                <h3 className="code-config-card__title">多智能体对抗辩论流水线 (Debate Pipeline)</h3>
                <span className="code-config-card__desc">Hunter 快速初筛 + Challenger/Judge 强推理辩论 + Synthesis 报告终审</span>
              </div>
              <label className="code-config-toggle">
                <input
                  type="checkbox"
                  checked={scannerConfig.debate.enabled}
                  onChange={e => setScannerConfig({
                    ...scannerConfig,
                    debate: { ...scannerConfig.debate, enabled: e.target.checked }
                  })}
                />
                <span className="code-config-toggle__slider" />
              </label>
            </div>

            <div className="code-config-grid-3">
              <div className="code-config-switch-row">
                <div className="code-config-switch-info">
                  <span className="code-config-switch-title">快通模式 (Fast Pass)</span>
                  <span className="code-config-switch-desc">高置信度缺陷直接跳过冗长辩论</span>
                </div>
                <label className="code-config-toggle">
                  <input
                    type="checkbox"
                    checked={scannerConfig.debate.fast_pass_enabled}
                    onChange={e => setScannerConfig({
                      ...scannerConfig,
                      debate: { ...scannerConfig.debate, fast_pass_enabled: e.target.checked }
                    })}
                  />
                  <span className="code-config-toggle__slider" />
                </label>
              </div>

              <div className="code-config-field">
                <label className="code-config-label">单分片候选缺陷上限 (Max Candidates)</label>
                <input
                  type="number"
                  className="code-config-input"
                  value={scannerConfig.debate.max_candidates_per_chunk}
                  onChange={e => setScannerConfig({
                    ...scannerConfig,
                    debate: { ...scannerConfig.debate, max_candidates_per_chunk: parseInt(e.target.value) || 30 }
                  })}
                />
              </div>

              <div className="code-config-field">
                <label className="code-config-label">阶段执行超时时间 (秒)</label>
                <input
                  type="number"
                  className="code-config-input"
                  value={scannerConfig.debate.stage_timeout_seconds}
                  onChange={e => setScannerConfig({
                    ...scannerConfig,
                    debate: { ...scannerConfig.debate, stage_timeout_seconds: parseInt(e.target.value) || 600 }
                  })}
                />
              </div>
            </div>

            {/* 辩论 3 层流水线绑定设置 */}
            <div style={{ marginTop: '1.25rem', borderTop: '1px solid var(--color-border-subtle)', paddingTop: '1.25rem' }}>
              {/* 动静分离与阶梯选型推荐指南 */}
              <div className="code-config-guide-banner">
                <div className="code-config-guide-banner__header">
                  <span className="code-config-guide-banner__icon">💡</span>
                  <div>
                    <h4 className="code-config-guide-banner__title">智能体辩论阶梯算力选型指南 (动静分离最佳实践)</h4>
                    <p className="code-config-guide-banner__subtitle">
                      根据任务是否需要自主探索本地文件系统，辩论流水线划分为「探索型 (Thick Agent)」与「纯推演型 (Thin LLM)」两类阶段，请参考以下选型建议：
                    </p>
                  </div>
                </div>
                <div className="code-config-guide-banner__grid">
                  <div className="code-config-guide-item code-config-guide-item--thick">
                    <div className="code-config-guide-item__head">
                      <span className="code-config-guide-item__badge code-config-guide-item__badge--thick">Tier 1 · 必须 Thick</span>
                      <strong className="code-config-guide-item__title">Hunter 初筛猎手</strong>
                    </div>
                    <p className="code-config-guide-item__desc">
                      <strong>任务特征：</strong>Prompt 仅传入文件名清单。模型<strong>必须具备本地磁盘文件读写与工作区遍历能力</strong>，需递归穿透跨文件调用链。
                    </p>
                    <div className="code-config-guide-item__rec">
                      <span>推荐驱动：</span><code>agy</code> (Antigravity 平台) / <code>opencode</code> (Thick CLI)
                    </div>
                  </div>

                  <div className="code-config-guide-item code-config-guide-item--thin">
                    <div className="code-config-guide-item__head">
                      <span className="code-config-guide-item__badge code-config-guide-item__badge--thin">Tier 2 · 强烈推荐 Thin</span>
                      <strong className="code-config-guide-item__title">Challenger & Judge</strong>
                    </div>
                    <p className="code-config-guide-item__desc">
                      <strong>任务特征：</strong>案卷代码切片与上下文<strong>已在 Prompt 中全量内联</strong>，无需磁盘 I/O。纯静态推理，需高吞吐与严格 JSON 格式。
                    </p>
                    <div className="code-config-guide-item__rec">
                      <span>推荐驱动：</span>优先 <code>native</code> (Thin LLM 毫秒级低延迟/10+并发)，推荐加选 <code>agy</code> 组成弹性容灾池
                    </div>
                  </div>

                  <div className="code-config-guide-item code-config-guide-item--thin-alt">
                    <div className="code-config-guide-item__head">
                      <span className="code-config-guide-item__badge code-config-guide-item__badge--thin-alt">Tier 3 · 推荐 Thin</span>
                      <strong className="code-config-guide-item__title">Synthesis 终审汇总</strong>
                    </div>
                    <p className="code-config-guide-item__desc">
                      <strong>任务特征：</strong>全仓确诊缺陷聚合、态势评分归因与 Markdown/JSON 排版。内存纯文本直传直出，零 CLI 进程开销。
                    </p>
                    <div className="code-config-guide-item__rec">
                      <span>推荐驱动：</span>推荐 <code>native</code> (Thin LLM 直传直出)
                    </div>
                  </div>
                </div>
              </div>

              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '0.85rem' }}>
                <h4 style={{ margin: 0, fontSize: '0.95rem' }}>各阶段算力阶梯绑定 (Tiers Binding)</h4>
                <span style={{ fontSize: '0.78rem', color: 'var(--color-text-muted)' }}>点击节点可多选形成混合资源池，由 ModelDispatcher 动态负载打散与故障转移</span>
              </div>
              <div className="code-config-grid-3">
                {renderTierResourcePoolSelector('tier1_hunter')}
                {renderTierResourcePoolSelector('tier2_reasoning')}
                {renderTierResourcePoolSelector('tier3_synthesis')}
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Tab 3: 质量治理与门禁 */}
      {activeTab === 'governance' && (
        <div className="code-config-panel">
          {/* 缺陷指纹与防抖 */}
          <div className="code-config-card">
            <div className="code-config-card__header">
              <div>
                <h3 className="code-config-card__title">缺陷指纹与智能防抖 (Defect Fingerprint)</h3>
                <span className="code-config-card__desc">基于代码 AST 上下文特征计算缺陷唯一指纹，抑制重复提单造成告警风暴</span>
              </div>
              <label className="code-config-toggle">
                <input
                  type="checkbox"
                  checked={govConfig.fingerprint.enabled}
                  onChange={e => setGovConfig({
                    ...govConfig,
                    fingerprint: { ...govConfig.fingerprint, enabled: e.target.checked }
                  })}
                />
                <span className="code-config-toggle__slider" />
              </label>
            </div>

            <div className="code-config-grid-2">
              <div className="code-config-field">
                <label className="code-config-label">
                  指纹相似度阈值 (Similarity Threshold)
                  <span className="code-config-label-hint">{(govConfig.fingerprint.similarity_threshold * 100).toFixed(0)}% 相似判定为同一缺陷</span>
                </label>
                <input
                  type="number"
                  step="0.01"
                  min="0.5"
                  max="1.0"
                  className="code-config-input"
                  value={govConfig.fingerprint.similarity_threshold}
                  onChange={e => setGovConfig({
                    ...govConfig,
                    fingerprint: { ...govConfig.fingerprint, similarity_threshold: parseFloat(e.target.value) || 0.85 }
                  })}
                />
              </div>
            </div>
          </div>

          {/* 全生命周期守卫 */}
          <div className="code-config-card">
            <div className="code-config-card__header">
              <h3 className="code-config-card__title">全生命周期治理守卫 (Lifecycle Guards)</h3>
            </div>
            <div className="code-config-grid-2">
              <div className="code-config-switch-row">
                <div className="code-config-switch-info">
                  <span className="code-config-switch-title">代码变更范围守卫 (Scope Guard)</span>
                  <span className="code-config-switch-desc">严格过滤未在本次变更 Diff 窗口内的遗留历史缺陷</span>
                </div>
                <label className="code-config-toggle">
                  <input
                    type="checkbox"
                    checked={govConfig.lifecycle.scope_guard_enabled}
                    onChange={e => setGovConfig({
                      ...govConfig,
                      lifecycle: { ...govConfig.lifecycle, scope_guard_enabled: e.target.checked }
                    })}
                  />
                  <span className="code-config-toggle__slider" />
                </label>
              </div>

              <div className="code-config-switch-row">
                <div className="code-config-switch-info">
                  <span className="code-config-switch-title">缺席缺陷自动闭环 (Auto Resolve Missing)</span>
                  <span className="code-config-switch-desc">重新扫描未检出时自动将历史存量缺陷标记为已修复</span>
                </div>
                <label className="code-config-toggle">
                  <input
                    type="checkbox"
                    checked={govConfig.lifecycle.auto_resolve_missing}
                    onChange={e => setGovConfig({
                      ...govConfig,
                      lifecycle: { ...govConfig.lifecycle, auto_resolve_missing: e.target.checked }
                    })}
                  />
                  <span className="code-config-toggle__slider" />
                </label>
              </div>

              <div className="code-config-switch-row">
                <div className="code-config-switch-info">
                  <span className="code-config-switch-title">PR 门禁严格拦截模式 (Diff Gate Strict)</span>
                  <span className="code-config-switch-desc">存在阻断级致命缺陷时直接标记 CI 检查失败</span>
                </div>
                <label className="code-config-toggle">
                  <input
                    type="checkbox"
                    checked={govConfig.lifecycle.diff_gate_strict}
                    onChange={e => setGovConfig({
                      ...govConfig,
                      lifecycle: { ...govConfig.lifecycle, diff_gate_strict: e.target.checked }
                    })}
                  />
                  <span className="code-config-toggle__slider" />
                </label>
              </div>
            </div>
          </div>

          {/* 专家反馈记忆池 */}
          <div className="code-config-card">
            <div className="code-config-card__header">
              <div>
                <h3 className="code-config-card__title">专家反馈经验库 (Feedback Memory)</h3>
                <span className="code-config-card__desc">基于工程师人工标注的误报与负样本规则，自动在 Prompt 中完成动态少样本注入</span>
              </div>
              <label className="code-config-toggle">
                <input
                  type="checkbox"
                  checked={govConfig.feedback_memory.injection_enabled}
                  onChange={e => setGovConfig({
                    ...govConfig,
                    feedback_memory: { ...govConfig.feedback_memory, injection_enabled: e.target.checked }
                  })}
                />
                <span className="code-config-toggle__slider" />
              </label>
            </div>

            <div className="code-config-grid-2">
              <div className="code-config-field">
                <label className="code-config-label">单任务最大负样本注入条数 (Max Rules Injected)</label>
                <input
                  type="number"
                  min="1"
                  max="50"
                  className="code-config-input"
                  value={govConfig.feedback_memory.max_rules_injected}
                  onChange={e => setGovConfig({
                    ...govConfig,
                    feedback_memory: { ...govConfig.feedback_memory, max_rules_injected: parseInt(e.target.value) || 10 }
                  })}
                />
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Tab 4: 通知服务 */}
      {activeTab === 'notification' && (
        <div className="code-config-panel">
          <div className="code-config-card">
            <div className="code-config-card__header">
              <h3 className="code-config-card__title">全局事件通知 (Notification Webhook)</h3>
            </div>
            <div className="code-config-field">
              <label className="code-config-label">
                全局通知 Webhook 地址
                <span className="code-config-label-hint">支持飞书、企业微信、钉钉或自定义 HTTP 接收网关</span>
              </label>
              <input
                className="code-config-input"
                value={notifConfig.webhook}
                onChange={e => setNotifConfig({ ...notifConfig, webhook: e.target.value })}
                placeholder="例如: https://open.feishu.cn/open-apis/bot/v2/hook/xxx"
              />
            </div>
          </div>
        </div>
      )}

      {/* Native Endpoint 编辑抽屉 */}
      {editingEndpoint && (
        <Drawer
          open={!!editingEndpoint}
          onClose={() => setEditingEndpoint(null)}
          title={editingEndpoint.epIdx === null ? '添加集群算力端点' : `编辑端点 — ${editingEndpoint.data.name}`}
          subtitle="配置该节点的物理推理机 Base URL、密钥与调度相对权重"
          width="500px"
          footer={
            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.75rem', width: '100%' }}>
              <button className="btn btn-secondary" onClick={() => setEditingEndpoint(null)}>
                取消
              </button>
              <button className="btn btn-primary" onClick={handleSaveEndpoint}>
                保存端点
              </button>
            </div>
          }
        >
          <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem', padding: '1rem' }}>
            <div className="code-config-field">
              <label className="code-config-label">端点名称 (Name)</label>
              <input
                className="code-config-input"
                value={editingEndpoint.data.name}
                onChange={e => setEditingEndpoint({
                  ...editingEndpoint,
                  data: { ...editingEndpoint.data, name: e.target.value }
                })}
                placeholder="例如: GPU-01-vLLM"
              />
            </div>

            <div className="code-config-field">
              <label className="code-config-label">服务 Base URL</label>
              <input
                className="code-config-input"
                value={editingEndpoint.data.base_url}
                onChange={e => setEditingEndpoint({
                  ...editingEndpoint,
                  data: { ...editingEndpoint.data, base_url: e.target.value }
                })}
                placeholder="例如: http://192.168.56.18:8000/v1"
              />
            </div>

            <div className="code-config-field">
              <label className="code-config-label">API Key (明文直存)</label>
              <input
                type="text"
                className="code-config-input"
                value={editingEndpoint.data.api_key}
                onChange={e => setEditingEndpoint({
                  ...editingEndpoint,
                  data: { ...editingEndpoint.data, api_key: e.target.value }
                })}
                placeholder="留空或 Bearer Token"
              />
            </div>

            <div className="code-config-grid-2">
              <div className="code-config-field">
                <label className="code-config-label">端点模型名 (Model)</label>
                <input
                  className="code-config-input"
                  value={editingEndpoint.data.model}
                  onChange={e => setEditingEndpoint({
                    ...editingEndpoint,
                    data: { ...editingEndpoint.data, model: e.target.value }
                  })}
                  placeholder="例如: glm-4-flash"
                />
              </div>

              <div className="code-config-field">
                <label className="code-config-label">并发槽位 (Concurrent)</label>
                <input
                  type="number"
                  className="code-config-input"
                  value={editingEndpoint.data.concurrent}
                  onChange={e => setEditingEndpoint({
                    ...editingEndpoint,
                    data: { ...editingEndpoint.data, concurrent: parseInt(e.target.value) || 1 }
                  })}
                />
              </div>
            </div>

            <div className="code-config-grid-2">
              <div className="code-config-field">
                <label className="code-config-label">相对分流权重 (Weight)</label>
                <input
                  type="number"
                  className="code-config-input"
                  value={editingEndpoint.data.weight}
                  onChange={e => setEditingEndpoint({
                    ...editingEndpoint,
                    data: { ...editingEndpoint.data, weight: parseInt(e.target.value) || 100 }
                  })}
                />
              </div>

              <div className="code-config-field">
                <label className="code-config-label">采样温度 (Temperature)</label>
                <input
                  type="number"
                  step="0.05"
                  min="0"
                  max="1.5"
                  className="code-config-input"
                  value={editingEndpoint.data.temperature}
                  onChange={e => setEditingEndpoint({
                    ...editingEndpoint,
                    data: { ...editingEndpoint.data, temperature: parseFloat(e.target.value) || 0.1 }
                  })}
                />
              </div>
            </div>
          </div>
        </Drawer>
      )}
    </div>
  );
}
