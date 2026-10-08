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
  TierBinding,
  PingResult
} from '../types/config';
import './ConfigCenter.css';

type ConfigCategory = 'llm' | 'scanner' | 'governance' | 'notification';
const VALID_TABS: ConfigCategory[] = ['llm', 'scanner', 'governance', 'notification'];
const DEFAULT_ATTEMPT_SECONDS = 900;
const DEFAULT_FIRST_BYTE_SECONDS = 180;
const DEFAULT_STREAM_IDLE_SECONDS = 300;

type TierKeyType = 'tier1_hunter' | 'tier2_challenger' | 'tier3_judge' | 'tier4_synthesis';

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
    chunk_concurrency: 6,
    max_queue_size: 2000,
    mock_on_missing_cli: true,
    analysis: {
      max_retries: 3,
      retry_backoff_ms: 2000,
      max_backoff_seconds: 30,
      retryable_errors: ['rate_limited', 'network_transient', 'unknown']
    },
    artifact: {
      normalization_enabled: true,
      max_schema_repair_attempts: 2,
      schema_repair_timeout_seconds: 600,
      schema_repair_resource: 'native',
      allow_candidate_salvage: true,
      max_quarantined_candidate_ratio: 0.5
    },
    resume: {
      legacy_v2_policy: 'reject'
    },
    throttling: {
      work_hours: {
        enabled: false,
        workdays: [1, 2, 3, 4, 5],
        start_time: '09:00',
        end_time: '22:00',
        scale: 0.1
      }
    },
    opencode: {
      continuation: {
        enabled: true,
        max_seconds: 600
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
        tier1_hunter: { resource: '', timeout_seconds: 3600, attempt_timeout_seconds: DEFAULT_ATTEMPT_SECONDS },
        tier2_challenger: { resource: '', timeout_seconds: 1800, attempt_timeout_seconds: DEFAULT_ATTEMPT_SECONDS },
        tier3_judge: { resource: '', timeout_seconds: 1800, attempt_timeout_seconds: DEFAULT_ATTEMPT_SECONDS },
        tier4_synthesis: { resource: '', timeout_seconds: 900, attempt_timeout_seconds: DEFAULT_ATTEMPT_SECONDS }
      }
    },
    tools: {
      default_resource: 'native',
      overrides: {}
    },
    determinism: {
      enabled: true,
      temperature: 0,
      bind_model_per_task: true,
      prefer_seed: true,
      seed_policy: 'repo+task+commit+chunk'
    }
  });

  const [govConfig, setGovConfig] = useState<GovernancePolicyConfig>({
    identity: {
      algorithm_version: 'v1',
      max_candidates_per_observation: 64,
      max_candidate_edges_per_report: 20000,
      max_assignment_width: 16,
      strong_same_threshold: 0.90,
      assign_band: 0.65,
      reject_below: 0.45,
      auto_resolve_gray_zone: true,
      ai_arbitration_confidence: 0.70,
      gray_zone_fallback_merge_score: 0.60
    },
    arbitration: {
      enabled: true,
      max_calls_per_report: 20,
      context_lines: 8,
      timeout_seconds: 30
    },
    lifecycle: {
      high_risk_severities: ['致命', '严重'],
      resolved_rounds: 2,
      dormant_rounds: 2,
      obsolete_after_dormant_rounds: 8,
      require_coverage: true,
      require_change: true
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
  const [advancedTierSettingsOpen, setAdvancedTierSettingsOpen] = useState<Partial<Record<TierKeyType, boolean>>>({});

  // 拉取全量配置
  const fetchFullConfig = useCallback(async () => {
    setLoading(true);
    try {
      const res = await fetch('/api/admin/config/full');
      if (res.ok) {
        const data = await res.json();
        if (data.llm) setLlmConfig(data.llm);
        if (data.scanner) {
          const scanner = data.scanner;
          // Older scanner rows predate idle_timeout_seconds. Show the
          // recommended value so a subsequent save does not persist zero.
          const tier1 = scanner.debate?.tiers?.tier1_hunter;
          if (tier1 && tier1.idle_timeout_seconds == null) {
            tier1.idle_timeout_seconds = 600;
          }
          if (scanner.debate?.tiers) {
            (Object.entries(scanner.debate.tiers) as Array<[TierKeyType, TierBinding | undefined]>).forEach(([tierKey, tier]) => {
              if (!tier) return;
              if (tier.timeout_seconds == null || tier.timeout_seconds <= 0) {
                tier.timeout_seconds = 1800;
              }
              if (tier.attempt_timeout_seconds == null || tier.attempt_timeout_seconds <= 0) {
                tier.attempt_timeout_seconds = DEFAULT_ATTEMPT_SECONDS;
              }
              if (tierKey === 'tier2_challenger' || tierKey === 'tier4_synthesis') {
                if (tier.first_byte_timeout_seconds == null || tier.first_byte_timeout_seconds <= 0) {
                  tier.first_byte_timeout_seconds = DEFAULT_FIRST_BYTE_SECONDS;
                }
                if (tier.idle_timeout_seconds == null || tier.idle_timeout_seconds <= 0) {
                  tier.idle_timeout_seconds = DEFAULT_STREAM_IDLE_SECONDS;
                }
              }
            });
          }
          if (scanner.chunk_concurrency == null) {
            scanner.chunk_concurrency = 6;
          }
          if (!scanner.analysis) {
            scanner.analysis = {
              max_retries: 3,
              retry_backoff_ms: 2000,
              max_backoff_seconds: 30,
              retryable_errors: ['rate_limited', 'network_transient', 'unknown']
            };
          }
          if (!scanner.artifact) {
            scanner.artifact = {
              normalization_enabled: true,
              max_schema_repair_attempts: 2,
              schema_repair_timeout_seconds: 600,
              schema_repair_resource: 'native',
              allow_candidate_salvage: true,
              max_quarantined_candidate_ratio: 0.5
            };
          }
          if (!scanner.resume) {
            scanner.resume = { legacy_v2_policy: 'reject' };
          }
          setScannerConfig(scanner);
        }
        if (data.governance) {
          const governance = data.governance;
          setGovConfig({
            identity: governance.identity || {
              algorithm_version: 'v1',
              max_candidates_per_observation: 64,
              max_candidate_edges_per_report: 20000,
              max_assignment_width: 16,
              strong_same_threshold: 0.90,
              assign_band: 0.65,
              reject_below: 0.45,
              auto_resolve_gray_zone: true,
              ai_arbitration_confidence: 0.70,
              gray_zone_fallback_merge_score: 0.60
            },
            arbitration: governance.arbitration || {
              enabled: true,
              max_calls_per_report: 20,
              context_lines: 8,
              timeout_seconds: 30
            },
            lifecycle: governance.lifecycle || {
              high_risk_severities: ['致命', '严重'],
              resolved_rounds: 2,
              dormant_rounds: 2,
              obsolete_after_dormant_rounds: 8,
              require_coverage: true,
              require_change: true
            }
          });
        }
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

  const validateTierBindings = () => {
    const tiers = scannerConfig.debate?.tiers;
    if (!tiers) return null;

    for (const [tierKey, meta] of Object.entries(TIER_METAS) as Array<[TierKeyType, typeof TIER_METAS[TierKeyType]]>) {
      const tier = tiers[tierKey];
      if (!tier) continue;

      const selected = (tier.resources && tier.resources.length > 0)
        ? tier.resources
        : (tier.resource ? [tier.resource] : []);
      const validResources = selected.filter(id => {
        const res = llmConfig.resources.find(item => item.id === id);
        if (!res) return false;
        const isThin = res.driver === 'native';
        return meta.allowedEngine === 'thick' ? !isThin : isThin;
      });

      if (validResources.length === 0) {
        return `${meta.roleTitle} 未绑定任何允许的 ${meta.allowedEngine === 'thick' ? 'Thick Agent' : 'Native LLM'} 节点`;
      }
      if (validResources.length !== selected.length) {
        return `${meta.roleTitle} 绑定了不允许的引擎类型`;
      }

      const attemptSeconds = tier.attempt_timeout_seconds || DEFAULT_ATTEMPT_SECONDS;
      if (attemptSeconds > tier.timeout_seconds) {
        return `${meta.roleTitle} 的单次 AI 调用超时不能大于阶段总预算`;
      }
    }

    return null;
  };

  // 保存当前激活 Tab 对应的模块配置 (细粒度 PUT API)
  const handleSaveCurrentModule = async () => {
    if (activeTab === 'scanner') {
      const validationError = validateTierBindings();
      if (validationError) {
        showToast(validationError, 'error');
        return;
      }
    }

    setSaving(true);
    let payload: any = null;
    switch (activeTab) {
      case 'llm':
        payload = {
          ...llmConfig,
          resources: llmConfig.resources.map(res => {
            if (res.driver !== 'native' || !res.endpoints?.length) return res;
            const { base_url, api_key, model, concurrent, ...resource } = res;
            return resource;
          })
        };
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
      enable_thinking: false,
      max_tokens: 0,
      max_retries: 3,
      retry_backoff_ms: 500,
      attempt_timeout_seconds: 300,
      first_byte_timeout_seconds: 60,
      idle_timeout_seconds: 60,
      max_output_bytes: 32768,
      endpoints: [{
        name: 'default',
        base_url: 'http://192.168.56.18:8000/v1',
        api_key: '',
        model: 'glm-4-flash',
        concurrent: 10,
      }]
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
    if ((llmConfig.resources[resIdx].endpoints?.length || 0) <= 1) {
      showToast('Native 节点至少需要保留 1 个端点', 'warning');
      return;
    }
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

  const toggleWorkday = (day: number) => {
    const current = scannerConfig.throttling.work_hours.workdays || [];
    const next = current.includes(day)
      ? current.filter(item => item !== day)
      : [...current, day].sort((a, b) => a - b);
    setScannerConfig({
      ...scannerConfig,
      throttling: {
        ...scannerConfig.throttling,
        work_hours: { ...scannerConfig.throttling.work_hours, workdays: next }
      }
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

  // 智能体辩论各阶梯元数据、允许引擎与默认预算
  const TIER_METAS: Record<TierKeyType, {
    tierNumber: string;
    roleTitle: string;
    badgeModifier: string;
    engineBadgeText: string;
    allowedEngine: 'thick' | 'native';
    desc: string;
    defaultSeconds: number;
  }> = {
    tier1_hunter: {
      tierNumber: 'Tier 1',
      roleTitle: 'Hunter 源码初筛',
      badgeModifier: 'code-config-tier-card__badge--tier1_hunter',
      allowedEngine: 'thick',
      engineBadgeText: '仅 Thick Agent',
      desc: '需要读取磁盘源码并遍历调用链；仅允许 Thick Agent，用于生成初筛案卷。',
      defaultSeconds: 3600,
    },
    tier2_challenger: {
      tierNumber: 'Tier 2',
      roleTitle: 'Challenger 辩护对抗',
      badgeModifier: 'code-config-tier-card__badge--tier2_challenger',
      allowedEngine: 'native',
      engineBadgeText: '仅 Native LLM',
      desc: '案卷代码已全量内联；仅允许 Native LLM 进行高吞吐反向抗辩。',
      defaultSeconds: 1800,
    },
    tier3_judge: {
      tierNumber: 'Tier 3',
      roleTitle: 'Judge 终审裁决',
      badgeModifier: 'code-config-tier-card__badge--tier3_judge',
      allowedEngine: 'thick',
      engineBadgeText: '仅 Thick Agent',
      desc: '基于源码物理事实进行终审仲裁；仅允许具备工作区访问能力的 Thick Agent。',
      defaultSeconds: 1800,
    },
    tier4_synthesis: {
      tierNumber: 'Tier 4',
      roleTitle: 'Synthesis 全仓汇总',
      badgeModifier: 'code-config-tier-card__badge--tier4_synthesis',
      allowedEngine: 'native',
      engineBadgeText: '仅 Native LLM',
      desc: '基于内存中的全量诊断结果聚合排版；仅允许 Native LLM 直传直出。',
      defaultSeconds: 900,
    },
  };

  // 辅助渲染各阶段算力资源池多选选择器
  const renderTierResourcePoolSelector = (tierKey: TierKeyType) => {
    const meta = TIER_METAS[tierKey];
    const availableResources = llmConfig.resources.filter(res => {
      const isThin = res.driver === 'native';
      return meta.allowedEngine === 'thick' ? !isThin : isThin;
    });

    let tierItem = scannerConfig.debate.tiers?.[tierKey];
    if (!tierItem) {
      tierItem = {
        resource: '',
        timeout_seconds: meta.defaultSeconds,
        attempt_timeout_seconds: DEFAULT_ATTEMPT_SECONDS,
      };
    }

    let selected = (tierItem.resources && tierItem.resources.length > 0)
      ? tierItem.resources
      : (tierItem.resource ? [tierItem.resource] : []);
    selected = selected.filter(id => availableResources.some(res => res.id === id));
    if (selected.length === 0 && availableResources.length > 0) {
      selected = [availableResources[0].id];
    }

    let totalSlots = 0;
    selected.forEach(id => {
      const res = llmConfig.resources.find(r => r.id === id);
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
      const updatedTiers = {
        ...scannerConfig.debate.tiers,
        [tierKey]: {
          ...tierItem,
          resource: next[0] || '',
          resources: next,
        }
      };
      setScannerConfig({
        ...scannerConfig,
        debate: {
          ...scannerConfig.debate,
          tiers: updatedTiers
        }
      });
    };

    const updateTierItem = (patch: Partial<TierBinding>) => {
      const nextItem = { ...tierItem, ...patch };
      const updatedTiers = {
        ...scannerConfig.debate.tiers,
        [tierKey]: nextItem,
      };
      setScannerConfig({
        ...scannerConfig,
        debate: { ...scannerConfig.debate, tiers: updatedTiers },
      });
    };

    const isNativeStage = meta.allowedEngine === 'native';
    const attemptSeconds = tierItem.attempt_timeout_seconds || DEFAULT_ATTEMPT_SECONDS;
    const advancedOpen = advancedTierSettingsOpen[tierKey] ?? false;
    const updateAdvancedOpen = (open: boolean) => {
      setAdvancedTierSettingsOpen(prev => ({ ...prev, [tierKey]: open }));
    };

    const timeoutBudgetWarnings = selected
      .map(id => {
        const res = llmConfig.resources.find(r => r.id === id);
        if (!isNativeStage || !res) return null;

        const retries = Math.max(0, res.max_retries ?? 0);
        const effectiveAttemptSeconds = tierItem.attempt_timeout_seconds || DEFAULT_ATTEMPT_SECONDS;
        const attempts = retries + 1;
        const requiredSeconds = attempts * effectiveAttemptSeconds;
        if (tierItem.timeout_seconds >= requiredSeconds) return null;

        return `${res.id} 需要 ${attempts} 次尝试 × ${effectiveAttemptSeconds} 秒 ≈ ${requiredSeconds} 秒，当前阶段总预算 ${tierItem.timeout_seconds} 秒（未含重试退避）`;
      })
      .filter((message): message is string => message !== null);

    const budgetWarnings = [...timeoutBudgetWarnings];
    if (attemptSeconds > tierItem.timeout_seconds) {
      budgetWarnings.push(`单次 AI 调用超时 ${attemptSeconds} 秒不能大于阶段总预算 ${tierItem.timeout_seconds} 秒。`);
    }
    if (tierKey === 'tier1_hunter' && attemptSeconds * 3 > tierItem.timeout_seconds) {
      budgetWarnings.push(`Tier 1 需要支持拆分重试，建议阶段总预算至少为 ${attemptSeconds * 3} 秒。`);
    }

    const diagnosticNotice = availableResources.length === 0
      ? {
          type: 'warning' as const,
          message: `当前没有可用的 ${meta.allowedEngine === 'thick' ? 'Thick Agent' : 'Native LLM'} 节点，请先在算力池中配置。`
        }
      : {
          type: 'info' as const,
          message: `本阶段仅允许 ${meta.allowedEngine === 'thick' ? 'Thick Agent' : 'Native LLM'}，共 ${availableResources.length} 个可选节点。`
        };

    return (
      <div className="code-config-tier-card">
        <div className="code-config-tier-card__header">
          <div className="code-config-tier-card__title-group">
            <div className="code-config-tier-card__title-row">
              <span className={`code-config-tier-card__badge ${meta.badgeModifier}`}>
                {meta.tierNumber}
              </span>
              <strong className="code-config-tier-card__title">{meta.roleTitle}</strong>
              <span className={`code-config-tier-card__role-tag ${meta.allowedEngine === 'thick' ? 'code-config-tier-card__role-tag--thick-required' : 'code-config-tier-card__role-tag--native-required'}`}>
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
            {availableResources.map(r => {
              const isChecked = selected.includes(r.id);
              const isThin = r.driver === 'native';
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

        {budgetWarnings.length > 0 && (
          <div className="code-config-tier-notice code-config-tier-notice--warning">
            <div>⚠️ 预算提醒：</div>
            {budgetWarnings.map(message => (
              <div key={message}>{message}</div>
            ))}
          </div>
        )}

          <div className="code-config-field" style={{ marginTop: 'auto' }}>
            <label className="code-config-label">
              阶段总预算 (秒)
            </label>
          <input
            type="number"
            className="code-config-input"
            value={tierItem.timeout_seconds}
            onChange={e => updateTierItem({ timeout_seconds: parseInt(e.target.value) || meta.defaultSeconds })}
          />
        </div>

          <div className="code-config-field" style={{ marginTop: '0.65rem' }}>
            <label className="code-config-label">
              单次 AI 调用超时 (秒)
              <span className="code-config-label-hint">
                {meta.allowedEngine === 'thick'
                  ? '每次 AI 调用的上限；0 使用资源默认。'
                  : '每次 Native HTTP 调用的上限；0 使用资源默认。'}
              </span>
            </label>
            <input
              type="number"
              min="1"
              className="code-config-input"
              value={tierItem.attempt_timeout_seconds || DEFAULT_ATTEMPT_SECONDS}
              onChange={e => updateTierItem({ attempt_timeout_seconds: Math.max(1, parseInt(e.target.value) || DEFAULT_ATTEMPT_SECONDS) })}
            />
          </div>

          {meta.allowedEngine === 'native' && (
            <details
              className="code-config-tier-advanced"
              open={advancedOpen}
              onToggle={event => updateAdvancedOpen(event.currentTarget.open)}
            >
              <summary className="code-config-tier-advanced__summary">
                <span>高级流式控制</span>
                <span>{advancedOpen ? '收起' : '展开'}</span>
              </summary>
              <div className="code-config-tier-advanced__body">
              <div className="code-config-field" style={{ marginTop: '0.65rem' }}>
                <label className="code-config-label">
                  首包超时 (秒)
                  <span className="code-config-label-hint">默认 180 秒；阶段级配置优先生效。</span>
                </label>
                <input
                  type="number"
                  min="1"
                  className="code-config-input"
                  value={tierItem.first_byte_timeout_seconds || DEFAULT_FIRST_BYTE_SECONDS}
                  onChange={e => updateTierItem({ first_byte_timeout_seconds: Math.max(1, parseInt(e.target.value) || DEFAULT_FIRST_BYTE_SECONDS) })}
                />
              </div>

              <div className="code-config-field" style={{ marginTop: '0.65rem' }}>
                <label className="code-config-label">
                  流式空闲超时 (秒)
                  <span className="code-config-label-hint">默认 300 秒；阶段级配置优先生效。</span>
                </label>
                <input
                  type="number"
                  min="1"
                  className="code-config-input"
                  value={tierItem.idle_timeout_seconds || DEFAULT_STREAM_IDLE_SECONDS}
                  onChange={e => updateTierItem({ idle_timeout_seconds: Math.max(1, parseInt(e.target.value) || DEFAULT_STREAM_IDLE_SECONDS) })}
                />
              </div>

              <div className="code-config-field" style={{ marginTop: '0.65rem' }}>
                <label className="code-config-label">
                  流式输出硬上限 (字节)
                  <span className="code-config-label-hint">0 使用资源默认；仍无配置时使用内置 32768 字节。</span>
                </label>
                <input
                  type="number"
                  min="0"
                  step="1024"
                  className="code-config-input"
                  value={tierItem.max_output_bytes ?? 0}
                  onChange={e => updateTierItem({ max_output_bytes: Math.max(0, parseInt(e.target.value) || 0) })}
                />
              </div>
              </div>
            </details>
          )}

          {meta.allowedEngine === 'thick' && (
            <div className="code-config-field" style={{ marginTop: '0.65rem' }}>
              <label className="code-config-label">
                进程空闲超时 (秒)
                <span className="code-config-label-hint">CLI stdout/stderr 无活动时提前判死；0 表示禁用。</span>
              </label>
              <input
                type="number"
                min="0"
                className="code-config-input"
                value={tierItem.idle_timeout_seconds ?? 0}
                onChange={e => updateTierItem({ idle_timeout_seconds: Math.max(0, parseInt(e.target.value) || 0) })}
              />
            </div>
          )}

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
                      <span style={{ fontSize: '0.85rem', color: 'var(--color-text-secondary)' }}>
                        模型: {res.driver === 'native'
                          ? ((res.endpoints?.length || 0) === 1 ? res.endpoints?.[0].model : `${res.endpoints?.length || 0} 个端点`)
                          : (res.model || '-')}
                      </span>
                      <span style={{ fontSize: '0.85rem', color: 'var(--color-text-muted)' }}>
                        并发槽位: {res.driver === 'native'
                          ? (res.endpoints || []).reduce((sum, endpoint) => sum + (endpoint.concurrent || 0), 0)
                          : res.concurrent}
                      </span>
                    </div>

                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
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
                        onChange={e => {
                          const driver = e.target.value;
                          if (driver === 'native' && !res.endpoints?.length) {
                            updateResource(idx, {
                              driver,
                              endpoints: [{
                                name: 'default',
                                base_url: res.base_url || 'http://192.168.56.18:8000/v1',
                                api_key: res.api_key || '',
                                model: res.model || 'glm-4-flash',
                                concurrent: res.concurrent || 10,
                              }]
                            });
                            return;
                          }
                          updateResource(idx, { driver });
                        }}
                      >
                        <option value="native">native (Thin LLM · HTTP REST 高并发纯推理)</option>
                        <option value="agy">agy (Thick Agent · Antigravity 探索型平台)</option>
                        <option value="opencode">opencode (Thick Agent · CLI 模式)</option>
                        <option value="claude">claude (Thick Agent · CLI 模式)</option>
                        <option value="codex">codex (Thick Agent · CLI 模式)</option>
                      </select>
                    </div>

                    {res.driver !== 'native' && (
                      <>
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
                      </>
                    )}

                    {res.driver === 'native' && (
                      <>
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

                        <div className="code-config-switch-row" style={{ gridColumn: 'span 1' }}>
                          <div className="code-config-switch-info">
                            <span className="code-config-switch-title">允许模型思考</span>
                            <span className="code-config-switch-desc">关闭后下发 thinking disabled</span>
                          </div>
                          <label className="code-config-toggle">
                            <input
                              type="checkbox"
                              checked={!!res.enable_thinking}
                              onChange={e => updateResource(idx, { enable_thinking: e.target.checked })}
                            />
                            <span className="code-config-toggle__slider" />
                          </label>
                        </div>

                        <div className="code-config-field">
                          <label className="code-config-label">
                            最大重试次数 (Max Retries)
                            <span className="code-config-label-hint">0 表示不重试；小于 1 时按 3 次重试处理，共最多 4 次尝试</span>
                          </label>
                          <input
                            type="number"
                            min="0"
                            max="10"
                            className="code-config-input"
                            value={res.max_retries ?? 0}
                            onChange={e => updateResource(idx, { max_retries: Math.max(0, parseInt(e.target.value) || 0) })}
                          />
                        </div>

                        <div className="code-config-field">
                          <label className="code-config-label">
                            重试退避基准 (毫秒)
                            <span className="code-config-label-hint">指数退避：500 表示约 0.5s / 1s / 2s</span>
                          </label>
                          <input
                            type="number"
                            min="0"
                            step="50"
                            className="code-config-input"
                            value={res.retry_backoff_ms ?? 0}
                            onChange={e => updateResource(idx, { retry_backoff_ms: Math.max(0, parseInt(e.target.value) || 0) })}
                          />
                        </div>

                        <div className="code-config-field">
                          <label className="code-config-label">
                            单次 HTTP 尝试超时 (秒)
                            <span className="code-config-label-hint">Tier 配置优先；0 使用内置默认 300 秒</span>
                          </label>
                          <input
                            type="number"
                            min="0"
                            className="code-config-input"
                            value={res.attempt_timeout_seconds ?? 0}
                            onChange={e => updateResource(idx, { attempt_timeout_seconds: Math.max(0, parseInt(e.target.value) || 0) })}
                          />
                        </div>

                        <div className="code-config-field">
                          <label className="code-config-label">
                            最大输出 Token
                            <span className="code-config-label-hint">0 表示 JSON 请求不限制；文本流默认 32768</span>
                          </label>
                          <input
                            type="number"
                            min="0"
                            step="256"
                            className="code-config-input"
                            value={res.max_tokens ?? 0}
                            onChange={e => updateResource(idx, { max_tokens: Math.max(0, parseInt(e.target.value) || 0) })}
                          />
                        </div>

                        <div className="code-config-field">
                          <label className="code-config-label">
                            首包超时 (秒，仅文本流)
                            <span className="code-config-label-hint">Tier 配置优先；0 使用内置默认 60 秒</span>
                          </label>
                          <input
                            type="number"
                            min="0"
                            className="code-config-input"
                            value={res.first_byte_timeout_seconds ?? 0}
                            onChange={e => updateResource(idx, { first_byte_timeout_seconds: Math.max(0, parseInt(e.target.value) || 0) })}
                          />
                        </div>

                        <div className="code-config-field">
                          <label className="code-config-label">
                            流式空闲超时 (秒)
                            <span className="code-config-label-hint">仅文本流生效；JSON 非流式由单次尝试超时保护；0 使用内置默认 60 秒</span>
                          </label>
                          <input
                            type="number"
                            min="0"
                            className="code-config-input"
                            value={res.idle_timeout_seconds ?? 0}
                            onChange={e => updateResource(idx, { idle_timeout_seconds: Math.max(0, parseInt(e.target.value) || 0) })}
                          />
                        </div>

                        <div className="code-config-field">
                          <label className="code-config-label">
                            流式输出硬上限 (字节，仅文本流)
                            <span className="code-config-label-hint">Tier 配置优先；0 使用内置默认 32768 字节</span>
                          </label>
                          <input
                            type="number"
                            min="0"
                            step="1024"
                            className="code-config-input"
                            value={res.max_output_bytes ?? 0}
                            onChange={e => updateResource(idx, { max_output_bytes: Math.max(0, parseInt(e.target.value) || 0) })}
                          />
                        </div>
                      </>
                    )}
                  </div>

                  {/* Native 集群多端点子表 */}
                  {res.driver === 'native' && (
                    <div style={{ marginTop: '0.75rem', background: 'var(--color-bg-muted)', padding: '1rem', borderRadius: '8px' }}>
                      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.75rem' }}>
                        <div>
                          <strong style={{ fontSize: '0.9rem' }}>集群多端点分流 (Endpoints)</strong>
                          <span style={{ fontSize: '0.78rem', color: 'var(--color-text-muted)', marginLeft: '0.5rem' }}>
                            物理推理机只能在此配置；按并发数自动分流，总并发 = 各端点并发之和
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
                        <div style={{ color: 'var(--color-danger, #ef4444)', fontSize: '0.8rem', textAlign: 'center', padding: '0.5rem' }}>
                          Native 节点至少需要 1 个集群端点。
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
                  <span className="code-config-label-hint">
                    {scannerConfig.worker_count} 个任务并发（保存后热生效；缩容不中断当前任务）
                  </span>
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
                  单任务分片并发 (Chunk Concurrency)
                  <span className="code-config-label-hint">
                    {scannerConfig.chunk_concurrency} 个分片并发（保存后对新任务生效）
                  </span>
                </label>
                <input
                  type="number"
                  min="1"
                  max="64"
                  className="code-config-input"
                  value={scannerConfig.chunk_concurrency}
                  onChange={e => setScannerConfig({ ...scannerConfig, chunk_concurrency: parseInt(e.target.value) || 1 })}
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

          <div className="code-config-card">
            <div className="code-config-card__header">
              <div>
                <h3 className="code-config-card__title">通用分析重试 (Analysis Retry)</h3>
                <span className="code-config-card__desc">对分片或全仓 AI 分析失败执行类型化业务重试，不包含算力节点内部故障转移</span>
              </div>
            </div>
            <div className="code-config-grid-3">
              <div className="code-config-field">
                <label className="code-config-label">最大重试次数</label>
                <input type="number" min="0" max="10" className="code-config-input" value={scannerConfig.analysis?.max_retries ?? 3} onChange={e => setScannerConfig({ ...scannerConfig, analysis: { ...scannerConfig.analysis, max_retries: Math.max(0, parseInt(e.target.value) || 0) } })} />
              </div>
              <div className="code-config-field">
                <label className="code-config-label">退避基准 (毫秒)</label>
                <input type="number" min="0" step="100" className="code-config-input" value={scannerConfig.analysis?.retry_backoff_ms ?? 2000} onChange={e => setScannerConfig({ ...scannerConfig, analysis: { ...scannerConfig.analysis, retry_backoff_ms: Math.max(0, parseInt(e.target.value) || 0) } })} />
              </div>
              <div className="code-config-field">
                <label className="code-config-label">最大退避等待 (秒)</label>
                <input type="number" min="1" className="code-config-input" value={scannerConfig.analysis?.max_backoff_seconds ?? 30} onChange={e => setScannerConfig({ ...scannerConfig, analysis: { ...scannerConfig.analysis, max_backoff_seconds: Math.max(1, parseInt(e.target.value) || 30) } })} />
              </div>
              <div className="code-config-field" style={{ gridColumn: 'span 3' }}>
                <label className="code-config-label">
                  可重试错误类型
                  <span className="code-config-label-hint">使用逗号分隔，例如 rate_limited, network_transient, unknown</span>
                </label>
                <input className="code-config-input" value={(scannerConfig.analysis?.retryable_errors || []).join(', ')} onChange={e => setScannerConfig({ ...scannerConfig, analysis: { ...scannerConfig.analysis, retryable_errors: e.target.value.split(',').map(item => item.trim()).filter(Boolean) } })} />
              </div>
            </div>
          </div>

          <div className="code-config-card">
            <div className="code-config-card__header">
              <div>
                <h3 className="code-config-card__title">产物修复与候选抢救 (Artifact Guard)</h3>
                <span className="code-config-card__desc">控制 AI 产物确定性规范化、Schema 修复和异常候选隔离/抢救边界</span>
              </div>
            </div>
            <div className="code-config-grid-3">
              <div className="code-config-switch-row">
                <div className="code-config-switch-info">
                  <span className="code-config-switch-title">确定性规范化 (Normalization)</span>
                  <span className="code-config-switch-desc">启用路径、行号、代码片段和结构化字段的稳定归一化</span>
                </div>
                <label className="code-config-toggle">
                  <input type="checkbox" checked={scannerConfig.artifact?.normalization_enabled ?? true} onChange={e => setScannerConfig({ ...scannerConfig, artifact: { ...scannerConfig.artifact, normalization_enabled: e.target.checked } })} />
                  <span className="code-config-toggle__slider" />
                </label>
              </div>
              <div className="code-config-switch-row">
                <div className="code-config-switch-info">
                  <span className="code-config-switch-title">允许候选抢救 (Candidate Salvage)</span>
                  <span className="code-config-switch-desc">从降级产物中抢救仍可信的缺陷候选</span>
                </div>
                <label className="code-config-toggle">
                  <input type="checkbox" checked={scannerConfig.artifact?.allow_candidate_salvage ?? true} onChange={e => setScannerConfig({ ...scannerConfig, artifact: { ...scannerConfig.artifact, allow_candidate_salvage: e.target.checked } })} />
                  <span className="code-config-toggle__slider" />
                </label>
              </div>
              <div className="code-config-field">
                <label className="code-config-label">最大 Schema 修复次数</label>
                <input type="number" min="0" max="10" className="code-config-input" value={scannerConfig.artifact?.max_schema_repair_attempts ?? 2} onChange={e => setScannerConfig({ ...scannerConfig, artifact: { ...scannerConfig.artifact, max_schema_repair_attempts: Math.max(0, parseInt(e.target.value) || 0) } })} />
              </div>
              <div className="code-config-field">
                <label className="code-config-label">Schema 修复超时 (秒)</label>
                <input type="number" min="1" className="code-config-input" value={scannerConfig.artifact?.schema_repair_timeout_seconds ?? 600} onChange={e => setScannerConfig({ ...scannerConfig, artifact: { ...scannerConfig.artifact, schema_repair_timeout_seconds: Math.max(1, parseInt(e.target.value) || 600) } })} />
              </div>
              <div className="code-config-field">
                <label className="code-config-label">Schema 修复算力节点</label>
                <select className="code-config-select" value={scannerConfig.artifact?.schema_repair_resource || 'native'} onChange={e => setScannerConfig({ ...scannerConfig, artifact: { ...scannerConfig.artifact, schema_repair_resource: e.target.value } })}>
                  {llmConfig.resources.map(resource => (<option key={resource.id} value={resource.id}>{resource.id}</option>))}
                </select>
              </div>
              <div className="code-config-field">
                <label className="code-config-label">最大隔离候选比例</label>
                <input type="number" step="0.05" min="0" max="1" className="code-config-input" value={scannerConfig.artifact?.max_quarantined_candidate_ratio ?? 0.5} onChange={e => setScannerConfig({ ...scannerConfig, artifact: { ...scannerConfig.artifact, max_quarantined_candidate_ratio: Math.max(0, parseFloat(e.target.value) || 0) } })} />
              </div>
            </div>
          </div>

          <div className="code-config-card">
            <div className="code-config-card__header">
              <div>
                <h3 className="code-config-card__title">Checkpoint 兼容策略 (Resume)</h3>
                <span className="code-config-card__desc">控制旧版本扫描 checkpoint 的回放与接受行为</span>
              </div>
            </div>
            <div className="code-config-grid-3">
              <div className="code-config-field">
                <label className="code-config-label">Legacy V2 Bundle 策略</label>
                <select className="code-config-select" value={scannerConfig.resume?.legacy_v2_policy || 'reject'} onChange={e => setScannerConfig({ ...scannerConfig, resume: { legacy_v2_policy: e.target.value } })}>
                  <option value="reject">reject（默认，回放旧 checkpoint）</option>
                  <option value="accept_success_as_complete">accept_success_as_complete（仅在确认旧产物可信时启用）</option>
                </select>
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

                <div className="code-config-field" style={{ gridColumn: 'span 3' }}>
                  <label className="code-config-label">生效工作日 (Workdays)</label>
                  <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap' }}>
                    {[
                      { day: 1, label: '周一' },
                      { day: 2, label: '周二' },
                      { day: 3, label: '周三' },
                      { day: 4, label: '周四' },
                      { day: 5, label: '周五' },
                      { day: 6, label: '周六' },
                      { day: 7, label: '周日' }
                    ].map(({ day, label }) => (
                      <button
                        type="button"
                        key={day}
                        className={`code-config-tier-res-btn ${scannerConfig.throttling.work_hours.workdays?.includes(day) ? 'code-config-tier-res-btn--selected' : ''}`}
                        onClick={() => toggleWorkday(day)}
                      >
                        <span>{scannerConfig.throttling.work_hours.workdays?.includes(day) ? '✓' : '+'}</span>
                        <span>{label}</span>
                      </button>
                    ))}
                  </div>
                </div>
              </div>
            )}
          </div>

          {/* OpenCode 续跑策略 */}
          <div className="code-config-card">
            <div className="code-config-card__header">
              <div>
                <h3 className="code-config-card__title">OpenCode 失败会话续跑 (Continuation)</h3>
                <span className="code-config-card__desc">首次执行输出异常时，复用会话上下文进行一次恢复重试</span>
              </div>
              <label className="code-config-toggle">
                <input
                  type="checkbox"
                  checked={scannerConfig.opencode?.continuation?.enabled ?? true}
                  onChange={e => setScannerConfig({
                    ...scannerConfig,
                    opencode: {
                      continuation: {
                        enabled: e.target.checked,
                        max_seconds: scannerConfig.opencode?.continuation?.max_seconds ?? 600
                      }
                    }
                  })}
                />
                <span className="code-config-toggle__slider" />
              </label>
            </div>
            {scannerConfig.opencode?.continuation?.enabled !== false && (
              <div className="code-config-grid-3">
                <div className="code-config-field">
                  <label className="code-config-label">
                    续跑预算 (Max Seconds)
                    <span className="code-config-label-hint">续跑重试的最大执行时间，默认 600 秒</span>
                  </label>
                  <input
                    type="number"
                    className="code-config-input"
                    min={60}
                    max={7200}
                    value={scannerConfig.opencode?.continuation?.max_seconds ?? 600}
                    onChange={e => setScannerConfig({
                      ...scannerConfig,
                      opencode: {
                        continuation: {
                          enabled: scannerConfig.opencode?.continuation?.enabled ?? true,
                          max_seconds: Math.max(60, parseInt(e.target.value) || 600)
                        }
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

              <div className="code-config-field">
                <label className="code-config-label">辩论轨迹保留天数</label>
                <input
                  type="number"
                  min="1"
                  max="3650"
                  className="code-config-input"
                  value={scannerConfig.debate.log_retention_days}
                  onChange={e => setScannerConfig({
                    ...scannerConfig,
                    debate: { ...scannerConfig.debate, log_retention_days: parseInt(e.target.value) || 30 }
                  })}
                />
              </div>

              <div className="code-config-field">
                <label className="code-config-label">背压触发阈值 (积压分片数)</label>
                <input
                  type="number"
                  min="0"
                  className="code-config-input"
                  value={scannerConfig.debate.backpressure_threshold}
                  onChange={e => setScannerConfig({
                    ...scannerConfig,
                    debate: { ...scannerConfig.debate, backpressure_threshold: Math.max(0, parseInt(e.target.value) || 0) }
                  })}
                />
              </div>

              <div className="code-config-field">
                <label className="code-config-label">背压等待超时 (秒)</label>
                <input
                  type="number"
                  min="0"
                  className="code-config-input"
                  value={scannerConfig.debate.backpressure_timeout_seconds}
                  onChange={e => setScannerConfig({
                    ...scannerConfig,
                    debate: { ...scannerConfig.debate, backpressure_timeout_seconds: Math.max(0, parseInt(e.target.value) || 0) }
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
                    <h4 className="code-config-guide-banner__title">各阶段引擎约束</h4>
                    <p className="code-config-guide-banner__subtitle">
                      阶段职责决定允许引擎：需要读写工作区的阶段固定使用 Thick Agent；纯内存推理阶段固定使用 Native LLM。
                    </p>
                  </div>
                </div>
                <div className="code-config-guide-banner__grid">
                  <div className="code-config-guide-item code-config-guide-item--thick">
                    <div className="code-config-guide-item__head">
                      <span className="code-config-guide-item__badge code-config-guide-item__badge--thick">Tier 1 · 仅 Thick</span>
                      <strong className="code-config-guide-item__title">Hunter 初筛猎手</strong>
                    </div>
                    <p className="code-config-guide-item__desc">
                      <strong>任务特征：</strong>Prompt 仅传入文件名清单。模型<strong>必须具备本地磁盘文件读写与工作区遍历能力</strong>，需递归穿透跨文件调用链。
                    </p>
                    <div className="code-config-guide-item__rec">
                      <span>允许驱动：</span><code>agy</code> / <code>opencode</code> / <code>codex</code>
                    </div>
                  </div>

                  <div className="code-config-guide-item code-config-guide-item--thin">
                    <div className="code-config-guide-item__head">
                      <span className="code-config-guide-item__badge code-config-guide-item__badge--thin">Tier 2 · 仅 Native</span>
                      <strong className="code-config-guide-item__title">Challenger 辩护对抗</strong>
                    </div>
                    <p className="code-config-guide-item__desc">
                      <strong>任务特征：</strong>案卷代码切片与上下文<strong>已在 Prompt 中全量内联</strong>，无需磁盘 I/O。纯静态推理，需高吞吐与严格 JSON 格式。
                    </p>
                    <div className="code-config-guide-item__rec">
                      <span>允许驱动：</span><code>native</code>
                    </div>
                  </div>

                  <div className="code-config-guide-item code-config-guide-item--thin">
                    <div className="code-config-guide-item__head">
                      <span className="code-config-guide-item__badge code-config-guide-item__badge--judge">Tier 3 · 仅 Thick</span>
                      <strong className="code-config-guide-item__title">Judge 终审裁决</strong>
                    </div>
                    <p className="code-config-guide-item__desc">
                      <strong>任务特征：</strong>兼听 Hunter 控告案卷与 Challenger 辩护事实，基于源码做终审定性。需具备最严谨的深度推理与逻辑裁决能力。
                    </p>
                    <div className="code-config-guide-item__rec">
                      <span>允许驱动：</span><code>agy</code> / <code>opencode</code> / <code>codex</code>
                    </div>
                  </div>

                  <div className="code-config-guide-item code-config-guide-item--thin-alt">
                    <div className="code-config-guide-item__head">
                      <span className="code-config-guide-item__badge code-config-guide-item__badge--thin-alt">Tier 4 · 仅 Native</span>
                      <strong className="code-config-guide-item__title">Synthesis 全仓汇总</strong>
                    </div>
                    <p className="code-config-guide-item__desc">
                      <strong>任务特征：</strong>全仓确诊缺陷聚合、态势评分归因与 Markdown/JSON 排版。内存纯文本直传直出，零 CLI 进程开销。
                    </p>
                    <div className="code-config-guide-item__rec">
                      <span>允许驱动：</span><code>native</code>
                    </div>
                  </div>
                </div>
              </div>

              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '0.85rem' }}>
                <h4 style={{ margin: 0, fontSize: '0.95rem' }}>各阶段算力阶梯绑定 (4 阶梯架构)</h4>
                <span style={{ fontSize: '0.78rem', color: 'var(--color-text-muted)' }}>点击节点可多选形成混合资源池，由 ModelDispatcher 动态负载打散与故障转移</span>
              </div>
              <div className="code-config-grid-4">
                {renderTierResourcePoolSelector('tier1_hunter')}
                {renderTierResourcePoolSelector('tier2_challenger')}
                {renderTierResourcePoolSelector('tier3_judge')}
                {renderTierResourcePoolSelector('tier4_synthesis')}
              </div>
            </div>
          </div>

          {/* 内置微任务路由 */}
          <div className="code-config-card">
            <div className="code-config-card__header">
              <h3 className="code-config-card__title">内置微任务路由 (Utility Tools)</h3>
            </div>
            <div className="code-config-grid-3">
              <div className="code-config-field">
                <label className="code-config-label">默认算力资源</label>
                <select
                  className="code-config-select"
                  value={scannerConfig.tools?.default_resource || 'native'}
                  onChange={e => setScannerConfig({
                    ...scannerConfig,
                    tools: { ...scannerConfig.tools, default_resource: e.target.value }
                  })}
                >
                  {llmConfig.resources.map(r => (
                    <option key={r.id} value={r.id}>{r.id} ({r.driver} / {r.model})</option>
                  ))}
                </select>
              </div>

              <div className="code-config-field">
                <label className="code-config-label">JSON 修复 (repair_json)</label>
                <select
                  className="code-config-select"
                  value={scannerConfig.tools?.overrides?.repair_json || scannerConfig.tools?.default_resource || 'native'}
                  onChange={e => setScannerConfig({
                    ...scannerConfig,
                    tools: { ...scannerConfig.tools, overrides: { ...scannerConfig.tools?.overrides, repair_json: e.target.value } }
                  })}
                >
                  {llmConfig.resources.map(r => (
                    <option key={r.id} value={r.id}>{r.id} ({r.driver} / {r.model})</option>
                  ))}
                </select>
              </div>

              <div className="code-config-field">
                <label className="code-config-label">缺陷匹配 (finding_match)</label>
                <select
                  className="code-config-select"
                  value={scannerConfig.tools?.overrides?.finding_match || scannerConfig.tools?.default_resource || 'native'}
                  onChange={e => setScannerConfig({
                    ...scannerConfig,
                    tools: { ...scannerConfig.tools, overrides: { ...scannerConfig.tools?.overrides, finding_match: e.target.value } }
                  })}
                >
                  {llmConfig.resources.map(r => (
                    <option key={r.id} value={r.id}>{r.id} ({r.driver} / {r.model})</option>
                  ))}
                </select>
              </div>

              <div className="code-config-field">
                <label className="code-config-label">反馈提炼 (feedback_extraction)</label>
                <select
                  className="code-config-select"
                  value={scannerConfig.tools?.overrides?.feedback_extraction || scannerConfig.tools?.default_resource || 'native'}
                  onChange={e => setScannerConfig({
                    ...scannerConfig,
                    tools: { ...scannerConfig.tools, overrides: { ...scannerConfig.tools?.overrides, feedback_extraction: e.target.value } }
                  })}
                >
                  {llmConfig.resources.map(r => (
                    <option key={r.id} value={r.id}>{r.id} ({r.driver} / {r.model})</option>
                  ))}
                </select>
              </div>
            </div>
          </div>

          {/* 确定性解码 */}
          <div className="code-config-card">
            <div className="code-config-card__header">
              <div>
                <h3 className="code-config-card__title">确定性解码 (Determinism)</h3>
                <span className="code-config-card__desc">控制跨轮报告可比性；不支持 seed 的驱动仅能通过 temperature 降低抖动</span>
              </div>
              <label className="code-config-toggle">
                <input
                  type="checkbox"
                  checked={scannerConfig.determinism?.enabled ?? true}
                  onChange={e => setScannerConfig({
                    ...scannerConfig,
                    determinism: { ...scannerConfig.determinism, enabled: e.target.checked }
                  })}
                />
                <span className="code-config-toggle__slider" />
              </label>
            </div>

            <div className="code-config-grid-3">
              <div className="code-config-field">
                <label className="code-config-label">采样温度 (Temperature)</label>
                <input
                  type="number"
                  step="0.05"
                  min="0"
                  max="2"
                  className="code-config-input"
                  value={scannerConfig.determinism?.temperature ?? 0}
                  onChange={e => setScannerConfig({
                    ...scannerConfig,
                    determinism: { ...scannerConfig.determinism, temperature: Math.max(0, parseFloat(e.target.value) || 0) }
                  })}
                />
              </div>

              <div className="code-config-switch-row">
                <div className="code-config-switch-info">
                  <span className="code-config-switch-title">任务内绑定模型</span>
                </div>
                <label className="code-config-toggle">
                  <input
                    type="checkbox"
                    checked={scannerConfig.determinism?.bind_model_per_task ?? true}
                    onChange={e => setScannerConfig({
                      ...scannerConfig,
                      determinism: { ...scannerConfig.determinism, bind_model_per_task: e.target.checked }
                    })}
                  />
                  <span className="code-config-toggle__slider" />
                </label>
              </div>

              <div className="code-config-switch-row">
                <div className="code-config-switch-info">
                  <span className="code-config-switch-title">优先下发 Seed</span>
                </div>
                <label className="code-config-toggle">
                  <input
                    type="checkbox"
                    checked={scannerConfig.determinism?.prefer_seed ?? true}
                    onChange={e => setScannerConfig({
                      ...scannerConfig,
                      determinism: { ...scannerConfig.determinism, prefer_seed: e.target.checked }
                    })}
                  />
                  <span className="code-config-toggle__slider" />
                </label>
              </div>

              <div className="code-config-field" style={{ gridColumn: 'span 3' }}>
                <label className="code-config-label">Seed 策略</label>
                <input
                  className="code-config-input"
                  value={scannerConfig.determinism?.seed_policy || ''}
                  onChange={e => setScannerConfig({
                    ...scannerConfig,
                    determinism: { ...scannerConfig.determinism, seed_policy: e.target.value }
                  })}
                  placeholder="例如: repo+task+commit+chunk"
                />
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Tab 3: 跨轮对账与缺陷治理 */}
      {activeTab === 'governance' && (
        <div className="code-config-panel">
          {/* 跨轮身份与候选阈值 */}
          <div className="code-config-card">
            <div className="code-config-card__header">
              <div>
                <h3 className="code-config-card__title">跨轮身份与候选阈值 (Identity)</h3>
                <span className="code-config-card__desc">控制候选召回、强身份归并、灰区自动裁决和新缺陷判定边界</span>
              </div>
            </div>
            <div className="code-config-grid-3">
              <div className="code-config-field">
                <label className="code-config-label">对账算法版本</label>
                <input className="code-config-input" value={govConfig.identity?.algorithm_version ?? 'v1'} onChange={e => setGovConfig({ ...govConfig, identity: { ...govConfig.identity, algorithm_version: e.target.value } })} />
              </div>
              <div className="code-config-field">
                <label className="code-config-label">单 Observation 最大候选数</label>
                <input type="number" min="1" className="code-config-input" value={govConfig.identity?.max_candidates_per_observation ?? 64} onChange={e => setGovConfig({ ...govConfig, identity: { ...govConfig.identity, max_candidates_per_observation: Math.max(1, parseInt(e.target.value) || 64) } })} />
              </div>
              <div className="code-config-field">
                <label className="code-config-label">单报告最大候选边数</label>
                <input type="number" min="1" className="code-config-input" value={govConfig.identity?.max_candidate_edges_per_report ?? 20000} onChange={e => setGovConfig({ ...govConfig, identity: { ...govConfig.identity, max_candidate_edges_per_report: Math.max(1, parseInt(e.target.value) || 20000) } })} />
              </div>
              <div className="code-config-field">
                <label className="code-config-label">全局指派最大宽度</label>
                <input type="number" min="1" className="code-config-input" value={govConfig.identity?.max_assignment_width ?? 16} onChange={e => setGovConfig({ ...govConfig, identity: { ...govConfig.identity, max_assignment_width: Math.max(1, parseInt(e.target.value) || 16) } })} />
              </div>
              <div className="code-config-field">
                <label className="code-config-label">强身份阈值</label>
                <input type="number" step="0.01" min="0" max="1" className="code-config-input" value={govConfig.identity?.strong_same_threshold ?? 0.90} onChange={e => setGovConfig({ ...govConfig, identity: { ...govConfig.identity, strong_same_threshold: Math.max(0, parseFloat(e.target.value) || 0) } })} />
              </div>
              <div className="code-config-field">
                <label className="code-config-label">自动指派阈值</label>
                <input type="number" step="0.01" min="0" max="1" className="code-config-input" value={govConfig.identity?.assign_band ?? 0.65} onChange={e => setGovConfig({ ...govConfig, identity: { ...govConfig.identity, assign_band: Math.max(0, parseFloat(e.target.value) || 0) } })} />
              </div>
              <div className="code-config-field">
                <label className="code-config-label">候选拒绝阈值</label>
                <input type="number" step="0.01" min="0" max="1" className="code-config-input" value={govConfig.identity?.reject_below ?? 0.45} onChange={e => setGovConfig({ ...govConfig, identity: { ...govConfig.identity, reject_below: Math.max(0, parseFloat(e.target.value) || 0) } })} />
              </div>
              <div className="code-config-field">
                <label className="code-config-label">AI 仲裁置信阈值</label>
                <input type="number" step="0.01" min="0" max="1" className="code-config-input" value={govConfig.identity?.ai_arbitration_confidence ?? 0.70} onChange={e => setGovConfig({ ...govConfig, identity: { ...govConfig.identity, ai_arbitration_confidence: Math.max(0, parseFloat(e.target.value) || 0) } })} />
              </div>
              <div className="code-config-field">
                <label className="code-config-label">灰区兜底合并分</label>
                <input type="number" step="0.01" min="0" max="1" className="code-config-input" value={govConfig.identity?.gray_zone_fallback_merge_score ?? 0.60} onChange={e => setGovConfig({ ...govConfig, identity: { ...govConfig.identity, gray_zone_fallback_merge_score: Math.max(0, parseFloat(e.target.value) || 0) } })} />
              </div>
              <div className="code-config-field">
                <label className="code-config-label">灰区自动裁决</label>
                <label className="code-config-toggle">
                  <input
                    type="checkbox"
                    checked={govConfig.identity?.auto_resolve_gray_zone ?? true}
                    onChange={e => setGovConfig({ ...govConfig, identity: { ...govConfig.identity, auto_resolve_gray_zone: e.target.checked } })}
                  />
                  <span className="code-config-toggle__slider" />
                </label>
              </div>
            </div>
          </div>

          {/* 跨轮疑似重复仲裁 */}
          <div className="code-config-card">
            <div className="code-config-card__header">
              <div>
                <h3 className="code-config-card__title">跨轮疑似重复仲裁 (AI Arbitration)</h3>
                <span className="code-config-card__desc">候选分落在灰区时，调用 AI 比较稳定代码身份；未决结果按阈值自动兜底</span>
              </div>
              <label className="code-config-toggle">
                <input
                  type="checkbox"
                  checked={govConfig.arbitration?.enabled ?? true}
                  onChange={e => setGovConfig({
                    ...govConfig,
                    arbitration: {
                      enabled: e.target.checked,
                      max_calls_per_report: govConfig.arbitration?.max_calls_per_report ?? 20,
                      context_lines: govConfig.arbitration?.context_lines ?? 8,
                      timeout_seconds: govConfig.arbitration?.timeout_seconds ?? 30
                    }
                  })}
                />
                <span className="code-config-toggle__slider" />
              </label>
            </div>

            <div className="code-config-grid-3">
              <div className="code-config-field">
                <label className="code-config-label">单报告最大 AI 仲裁次数</label>
                <input type="number" min="0" className="code-config-input" value={govConfig.arbitration?.max_calls_per_report ?? 20} onChange={e => setGovConfig({ ...govConfig, arbitration: { enabled: govConfig.arbitration?.enabled ?? true, max_calls_per_report: Math.max(0, parseInt(e.target.value) || 0), context_lines: govConfig.arbitration?.context_lines ?? 8, timeout_seconds: govConfig.arbitration?.timeout_seconds ?? 30 } })} />
              </div>
              <div className="code-config-field">
                <label className="code-config-label">源码上下文行数</label>
                <input type="number" min="0" className="code-config-input" value={govConfig.arbitration?.context_lines ?? 8} onChange={e => setGovConfig({ ...govConfig, arbitration: { enabled: govConfig.arbitration?.enabled ?? true, max_calls_per_report: govConfig.arbitration?.max_calls_per_report ?? 20, context_lines: Math.max(0, parseInt(e.target.value) || 0), timeout_seconds: govConfig.arbitration?.timeout_seconds ?? 30 } })} />
              </div>
              <div className="code-config-field">
                <label className="code-config-label">仲裁调用超时 (秒)</label>
                <input type="number" min="1" className="code-config-input" value={govConfig.arbitration?.timeout_seconds ?? 30} onChange={e => setGovConfig({ ...govConfig, arbitration: { enabled: govConfig.arbitration?.enabled ?? true, max_calls_per_report: govConfig.arbitration?.max_calls_per_report ?? 20, context_lines: govConfig.arbitration?.context_lines ?? 8, timeout_seconds: Math.max(1, parseInt(e.target.value) || 30) } })} />
              </div>
            </div>
          </div>

          {/* 台账生命周期策略 */}
          <div className="code-config-card">
            <div className="code-config-card__header">
              <div>
                <h3 className="code-config-card__title">台账生命周期策略 (Lifecycle)</h3>
                <span className="code-config-card__desc">控制缺陷修复确认、休眠、废弃和高风险严格生命周期策略</span>
              </div>
            </div>
            <div className="code-config-grid-3">
              <div className="code-config-field">
                <label className="code-config-label">高风险严重等级</label>
                <input className="code-config-input" value={(govConfig.lifecycle?.high_risk_severities || []).join(',')} onChange={e => setGovConfig({ ...govConfig, lifecycle: { ...govConfig.lifecycle, high_risk_severities: e.target.value.split(/[,，]/).map(item => item.trim()).filter(Boolean) } })} />
              </div>
              <div className="code-config-field">
                <label className="code-config-label">修复确认轮数</label>
                <input type="number" min="1" className="code-config-input" value={govConfig.lifecycle?.resolved_rounds ?? 2} onChange={e => setGovConfig({ ...govConfig, lifecycle: { ...govConfig.lifecycle, resolved_rounds: Math.max(1, parseInt(e.target.value) || 2) } })} />
              </div>
              <div className="code-config-field">
                <label className="code-config-label">休眠触发轮数</label>
                <input type="number" min="1" className="code-config-input" value={govConfig.lifecycle?.dormant_rounds ?? 2} onChange={e => setGovConfig({ ...govConfig, lifecycle: { ...govConfig.lifecycle, dormant_rounds: Math.max(1, parseInt(e.target.value) || 2) } })} />
              </div>
              <div className="code-config-field">
                <label className="code-config-label">废弃休眠轮数</label>
                <input type="number" min="1" className="code-config-input" value={govConfig.lifecycle?.obsolete_after_dormant_rounds ?? 8} onChange={e => setGovConfig({ ...govConfig, lifecycle: { ...govConfig.lifecycle, obsolete_after_dormant_rounds: Math.max(1, parseInt(e.target.value) || 8) } })} />
              </div>
              <div className="code-config-switch-row">
                <div className="code-config-switch-info">
                  <span className="code-config-switch-title">要求覆盖完整 (Require Coverage)</span>
                  <span className="code-config-switch-desc">覆盖缺口时不轻率判定缺陷已修复</span>
                </div>
                <label className="code-config-toggle">
                  <input type="checkbox" checked={govConfig.lifecycle?.require_coverage ?? true} onChange={e => setGovConfig({ ...govConfig, lifecycle: { ...govConfig.lifecycle, require_coverage: e.target.checked } })} />
                  <span className="code-config-toggle__slider" />
                </label>
              </div>
              <div className="code-config-switch-row">
                <div className="code-config-switch-info">
                  <span className="code-config-switch-title">要求提交变更 (Require Change)</span>
                  <span className="code-config-switch-desc">修复结论要求存在对应的代码变更证据</span>
                </div>
                <label className="code-config-toggle">
                  <input type="checkbox" checked={govConfig.lifecycle?.require_change ?? true} onChange={e => setGovConfig({ ...govConfig, lifecycle: { ...govConfig.lifecycle, require_change: e.target.checked } })} />
                  <span className="code-config-toggle__slider" />
                </label>
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

          </div>
        </Drawer>
      )}
    </div>
  );
}
