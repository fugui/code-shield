import React, { useState, useEffect, useCallback, useRef } from 'react';
import { useToast } from '../components/Toast';
import { EmptyState } from '@code/common';
import './SystemDebug.css';

interface SystemInfo {
  go_version: string;
  num_cpu: number;
  num_goroutine: number;
  server_start_time: string;
  uptime_seconds: number;
  uptime_formatted: string;
}

interface MemoryInfo {
  alloc_bytes: number;
  alloc_formatted: string;
  total_alloc_bytes: number;
  total_alloc_fmt: string;
  sys_bytes: number;
  sys_formatted: string;
  heap_alloc_bytes: number;
  heap_alloc_fmt: string;
  heap_sys_bytes: number;
  heap_sys_fmt: string;
  heap_inuse_bytes: number;
  heap_idle_bytes: number;
  heap_released_fmt: string;
  heap_objects: number;
  num_gc: number;
  pause_total_ms: number;
  last_gc_time: string;
}

interface EndpointConfig {
  name: string;
  base_url: string;
  model: string;
  concurrent: number;
  weight: number;
}

interface ModelResourceStatus {
  index: number;
  id?: string;
  driver?: string;
  model?: string;
  opencode?: string;
  claude?: string;
  codex?: string;
  agy?: string;
  native?: string;
  concurrent: number;
  active: number;
  limit: number;
  endpoints?: EndpointConfig[];
}

interface ThrottleInfo {
  effective_scale: number;
  throttle_mode: string;
  manual_scale: number;
  scale_expires_at: string | null;
  is_manual: boolean;
  is_work_hours: boolean;
}

interface WorkerPoolInfo {
  worker_count: number;
  active_workers: number;
  max_queue_size: number;
  pending_tasks: number;
  is_paused: boolean;
}

interface RunningTaskInfo {
  report_id: number;
  repo_id: number;
  repo_name: string;
  repo_url: string;
  task_type: string;
  task_display_name: string;
  engine_mode: string;
  status: string;
  start_time: string;
  duration_seconds: number;
  total_chunks: number;
  processed_chunks: number;
  success_chunks: number;
  attempts: number;
}

interface LLMSlotLease {
  lease_id: string;
  server_index: number;
  server_id: string;
  driver: string;
  model: string;
  report_id: number;
  repo_name: string;
  task_type: string;
  stage: string;
  sub_task: string;
  detail?: string;
  start_time: string;
  duration_seconds: number;
}

interface DebateTierItem {
  resource?: string;
  resources?: string[];
  timeout_seconds: number;
}

interface DebatePipelineInfo {
  enabled: boolean;
  fast_pass_enabled: boolean;
  stage_timeout_seconds: number;
  backpressure_threshold: number;
  tiers: {
    tier1_hunter?: DebateTierItem;
    tier2_reasoning?: DebateTierItem;
    tier3_synthesis?: DebateTierItem;
  };
  tools: {
    default_resource: string;
    overrides?: Record<string, string>;
  };
}

interface DailyStatsInfo {
  today_total: number;
  today_success: number;
  today_failed: number;
  today_tier1_tokens: number;
  today_tier2_tokens: number;
  today_new_defects: number;
}

interface DebugOverviewData {
  system: SystemInfo;
  memory: MemoryInfo;
  dispatcher: {
    throttle_info: ThrottleInfo;
    resources: ModelResourceStatus[];
    total_active_slots: number;
    total_limit_slots: number;
    total_raw_slots: number;
    active_leases?: LLMSlotLease[];
  };
  active_leases?: LLMSlotLease[];
  workers: WorkerPoolInfo;
  active_tasks: RunningTaskInfo[];
  debate_pipeline: DebatePipelineInfo;
  daily_stats: DailyStatsInfo;
}

export default function SystemDebug() {
  const { showToast } = useToast();
  const [data, setData] = useState<DebugOverviewData | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshInterval, setRefreshInterval] = useState<number>(5); // 默认 5 秒自动轮询实时诊断
  const [lastUpdated, setLastUpdated] = useState<Date>(new Date());
  const [gcTriggering, setGcTriggering] = useState(false);
  const [resettingSlots, setResettingSlots] = useState(false);
  const timerRef = useRef<NodeJS.Timeout | null>(null);

  const fetchOverview = useCallback(async (isSilent = false) => {
    if (!isSilent) setLoading(true);
    try {
      const res = await fetch('/api/admin/debug/overview');
      if (res.ok) {
        const json = await res.json();
        setData(json);
        setLastUpdated(new Date());
      } else {
        if (!isSilent) showToast('获取系统诊断数据失败: ' + res.statusText, 'error');
      }
    } catch (err: any) {
      if (!isSilent) showToast('请求诊断数据异常: ' + err.message, 'error');
    } finally {
      if (!isSilent) setLoading(false);
    }
  }, [showToast]);

  useEffect(() => {
    fetchOverview();
  }, [fetchOverview]);

  // 自动刷新定时器
  useEffect(() => {
    if (timerRef.current) {
      clearInterval(timerRef.current);
      timerRef.current = null;
    }
    if (refreshInterval > 0) {
      timerRef.current = setInterval(() => {
        fetchOverview(true);
      }, refreshInterval * 1000);
    }
    return () => {
      if (timerRef.current) clearInterval(timerRef.current);
    };
  }, [refreshInterval, fetchOverview]);

  const handleTriggerGC = async () => {
    setGcTriggering(true);
    try {
      const res = await fetch('/api/admin/debug/gc', { method: 'POST' });
      if (res.ok) {
        const result = await res.json();
        showToast(`Full GC 执行成功！释放内存: ${result.freed_formatted} (耗时 ${result.duration_ms}ms)`, 'success');
        fetchOverview(true);
      } else {
        showToast('触发 GC 失败', 'error');
      }
    } catch (err: any) {
      showToast('触发 GC 异常: ' + err.message, 'error');
    } finally {
      setGcTriggering(false);
    }
  };

  const handleResetActiveSlots = async () => {
    if (!window.confirm('确定要强制重置所有 LLM 算力节点的活跃槽位计数器为 0 吗？\n该操作将立即清零孤儿泄漏槽位，并广播唤醒所有处于等待状态的分析任务。')) {
      return;
    }
    setResettingSlots(true);
    try {
      const res = await fetch('/api/admin/debug/reset-slots', { method: 'POST' });
      if (res.ok) {
        const result = await res.json();
        showToast(`活跃槽位重置成功！已释放 ${result.cleared} 个活跃槽位并唤醒等待任务。`, 'success');
        fetchOverview(true);
      } else {
        showToast('重置槽位失败', 'error');
      }
    } catch (err: any) {
      showToast('重置槽位异常: ' + err.message, 'error');
    } finally {
      setResettingSlots(false);
    }
  };

  const handleDownloadPprof = (endpoint: string, filename: string) => {
    const url = `/api/admin/debug/pprof/${endpoint}`;
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    showToast(`正在导出 ${filename}...`, 'info');
  };

  const formatDuration = (seconds: number) => {
    if (seconds < 60) return `${seconds} 秒`;
    const mins = Math.floor(seconds / 60);
    const remSecs = seconds % 60;
    return `${mins} 分 ${remSecs} 秒`;
  };

  // 计算 Worker 占用比例
  const workerActive = data?.workers?.active_workers || 0;
  const workerTotal = data?.workers?.worker_count || 5;
  const workerPercent = Math.min(100, Math.round((workerActive / workerTotal) * 100));

  // 计算全局 AI 槽位占用比例
  const activeSlots = data?.dispatcher?.total_active_slots || 0;
  const limitSlots = data?.dispatcher?.total_limit_slots || 1;
  const slotPercent = Math.min(100, Math.round((activeSlots / limitSlots) * 100));

  // 获取当前正在运行的 LLM 算力槽位租约列表
  const activeLeases: LLMSlotLease[] = data?.dispatcher?.active_leases || data?.active_leases || [];

  return (
    <div className="code-debug-container">
      {/* 顶部状态与工具栏 */}
      <div className="code-debug-header">
        <div className="code-debug-header__left">
          <div className="code-debug-header__icon">
            <svg width="22" height="22" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
              <path strokeLinecap="round" strokeLinejoin="round" d="M13 10V3L4 14h7v7l9-11h-7z" />
            </svg>
          </div>
          <div>
            <h2 className="code-debug-header__title">
              扫描任务与 AI 算力实时诊断中心
            </h2>
            <div className="code-debug-header__subtitle">
              上次快照: {lastUpdated.toLocaleTimeString()} · CS-NATIVE-02 任务队列与模型调度器实时全景透视
            </div>
          </div>
        </div>

        <div className="code-debug-header__actions">
          {/* 自动刷新选择 */}
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', fontSize: '0.85rem', color: 'var(--color-text-secondary, #64748b)' }}>
            <span>自动刷新:</span>
            <select
              value={refreshInterval}
              onChange={(e) => setRefreshInterval(Number(e.target.value))}
              className="code-debug-header__select"
            >
              <option value={0}>手动刷新</option>
              <option value={3}>每 3 秒</option>
              <option value={5}>每 5 秒 (推荐)</option>
              <option value={10}>每 10 秒</option>
              <option value={30}>每 30 秒</option>
            </select>
          </div>

          <button
            className="btn btn-secondary"
            onClick={() => fetchOverview(false)}
            disabled={loading}
            style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', padding: '0.45rem 0.9rem', fontSize: '0.85rem' }}
          >
            <svg width="15" height="15" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2" style={{ transform: loading ? 'rotate(180deg)' : 'none', transition: 'transform 0.5s' }}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
            </svg>
            刷新
          </button>

          <button
            className="btn btn-secondary"
            onClick={handleTriggerGC}
            disabled={gcTriggering}
            style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', padding: '0.45rem 0.9rem', fontSize: '0.85rem' }}
          >
            <svg width="15" height="15" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
              <path strokeLinecap="round" strokeLinejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
            </svg>
            {gcTriggering ? 'GC 运行中...' : '手动 Full GC'}
          </button>

          <button
            className="btn btn-secondary"
            onClick={() => handleDownloadPprof('heap', `heap-${Date.now()}.pb.gz`)}
            title="下载堆内存 Heap Profile"
            style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', padding: '0.45rem 0.9rem', fontSize: '0.85rem' }}
          >
            <svg width="15" height="15" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
              <path strokeLinecap="round" strokeLinejoin="round" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4" />
            </svg>
            导出 Heap Profile
          </button>
        </div>
      </div>

      {/* 四格核心全局指标大盘 */}
      <div className="code-debug-metrics-grid">
        {/* 卡片 1: 扫描 Worker 工作池 */}
        <div className="code-debug-card">
          <div className="code-debug-card__top">
            <span className="code-debug-card__title">扫描任务 Worker 池</span>
            <span className={`code-debug-card__badge ${data?.workers?.is_paused ? 'code-debug-card__badge--warning' : 'code-debug-card__badge--success'}`}>
              {data?.workers?.is_paused ? '已暂停派发' : '就绪工作'}
            </span>
          </div>
          <div>
            <div className="code-debug-card__value">
              {workerActive} <span style={{ fontSize: '1rem', fontWeight: 500, color: 'var(--color-text-secondary, #64748b)' }}>/ {workerTotal} 运行中</span>
            </div>
            <div className="code-debug-progress-bar">
              <div
                className="code-debug-progress-fill"
                style={{
                  width: `${workerPercent}%`,
                  background: workerPercent >= 100 ? '#ef4444' : (workerPercent > 60 ? '#f59e0b' : '#2563eb')
                }}
              />
            </div>
          </div>
          <div className="code-debug-card__subtext">
            <span>排队待处理: <strong>{data?.workers?.pending_tasks || 0}</strong> 笔 (队列上限: {data?.workers?.max_queue_size || 2000})</span>
          </div>
        </div>

        {/* 卡片 2: AI 算力总槽位负载 */}
        <div className="code-debug-card">
          <div className="code-debug-card__top">
            <span className="code-debug-card__title">AI 算力槽位实时占用</span>
            <span className="code-debug-card__badge code-debug-card__badge--info">
              {data?.dispatcher?.throttle_info?.throttle_mode === 'work_hours' ? '工作时间避峰' : (data?.dispatcher?.throttle_info?.throttle_mode === 'manual' ? '手动覆盖' : '标准全速')}
              （{((data?.dispatcher?.throttle_info?.effective_scale || 1) * 100).toFixed(0)}%）
            </span>
          </div>
          <div>
            <div className="code-debug-card__value">
              {activeSlots} <span style={{ fontSize: '1rem', fontWeight: 500, color: 'var(--color-text-secondary, #64748b)' }}>/ {limitSlots} 槽位</span>
            </div>
            <div className="code-debug-progress-bar">
              <div
                className="code-debug-progress-fill"
                style={{
                  width: `${slotPercent}%`,
                  background: slotPercent >= 100 ? '#ef4444' : (slotPercent > 60 ? '#f59e0b' : '#10b981')
                }}
              />
            </div>
          </div>
          <div className="code-debug-card__subtext">
            <span>原始总并发: {data?.dispatcher?.total_raw_slots || 0} · 纳管节点: {data?.dispatcher?.resources?.length || 0} 台</span>
          </div>
        </div>

        {/* 卡片 3: 今日执行吞吐与效能 */}
        <div className="code-debug-card">
          <div className="code-debug-card__top">
            <span className="code-debug-card__title">今日扫描任务吞吐</span>
            <span className="code-debug-card__badge code-debug-card__badge--success">
              共 {data?.daily_stats?.today_total || 0} 批次
            </span>
          </div>
          <div className="code-debug-card__value">
            <span style={{ color: '#10b981' }}>{data?.daily_stats?.today_success || 0} 成功</span>
            {Boolean(data?.daily_stats?.today_failed) && (
              <span style={{ fontSize: '1.2rem', color: '#ef4444', marginLeft: '0.6rem' }}>
                / {data?.daily_stats?.today_failed} 失败
              </span>
            )}
          </div>
          <div className="code-debug-card__subtext">
            <span>检出缺陷: <strong>{data?.daily_stats?.today_new_defects || 0}</strong> 个 · 今日 Token: {((data?.daily_stats?.today_tier1_tokens || 0) + (data?.daily_stats?.today_tier2_tokens || 0)).toLocaleString()}</span>
          </div>
        </div>

        {/* 卡片 4: 运行时与内存健康 */}
        <div className="code-debug-card">
          <div className="code-debug-card__top">
            <span className="code-debug-card__title">运行时与内存健康</span>
            <span className="code-debug-card__badge code-debug-card__badge--info">
              {data?.system?.num_goroutine || 0} 协程
            </span>
          </div>
          <div className="code-debug-card__value">
            {data?.memory?.heap_alloc_fmt || '-'}
          </div>
          <div className="code-debug-card__subtext">
            <span>GC: {data?.memory?.num_gc || 0} 次 ({data?.memory?.pause_total_ms?.toFixed(1) || 0}ms) · 运行: {data?.system?.uptime_formatted || '-'}</span>
          </div>
        </div>
      </div>

      {/* 版块 1: LLM 模型调度器 (ModelDispatcher) 算力池实时负载 (颠倒提升至上方) */}
      <div className="code-debug-section">
        <div className="code-debug-section__header">
          <div className="code-debug-section__title-group">
            <h3 className="code-debug-section__title">
              LLM 模型调度器 (ModelDispatcher) 算力池实时负载
            </h3>
            <span style={{
              fontSize: '0.75rem',
              padding: '2px 8px',
              borderRadius: '6px',
              background: data?.dispatcher?.throttle_info?.throttle_mode === 'work_hours' ? 'rgba(245, 158, 11, 0.15)' : 'rgba(37, 99, 235, 0.1)',
              color: data?.dispatcher?.throttle_info?.throttle_mode === 'work_hours' ? '#f59e0b' : '#2563eb',
              fontWeight: 600
            }}>
              模式: {data?.dispatcher?.throttle_info?.throttle_mode === 'work_hours' ? '工作时间限流' : (data?.dispatcher?.throttle_info?.throttle_mode === 'manual' ? '手动覆盖' : '标准全速')}
              （比例: {((data?.dispatcher?.throttle_info?.effective_scale || 1) * 100).toFixed(0)}%）
            </span>
          </div>

          <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
            <span style={{ fontSize: '0.8rem', color: 'var(--color-text-secondary, #64748b)' }}>
              总纳管算力节点: {data?.dispatcher?.resources?.length || 0} 台
            </span>
            <button
              className="btn btn-secondary"
              onClick={handleResetActiveSlots}
              disabled={resettingSlots}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: '0.3rem',
                padding: '0.25rem 0.65rem',
                fontSize: '0.78rem',
                borderColor: 'var(--color-danger, #ef4444)',
                color: 'var(--color-danger, #ef4444)'
              }}
              title="当现网因历史孤儿泄漏导致 Active 占满死锁时，点击可一键清零活跃槽位并唤醒等待任务"
            >
              <svg width="13" height="13" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
                <path strokeLinecap="round" strokeLinejoin="round" d="M13 10V3L4 14h7v7l9-11h-7z" />
              </svg>
              {resettingSlots ? '校准中...' : '校准活跃槽位'}
            </button>
          </div>
        </div>

        {(!data?.dispatcher?.resources || data.dispatcher.resources.length === 0) ? (
          <div style={{ padding: '1.5rem', textAlign: 'center', color: 'var(--color-text-secondary, #64748b)', fontSize: '0.9rem' }}>
            当前未配置多服务器模型调度器（运行在默认单直通模式）
          </div>
        ) : (
          <div className="code-debug-resources-grid">
            {data.dispatcher.resources.map((res) => {
              const percent = res.limit > 0 ? Math.min(100, Math.round((res.active / res.limit) * 100)) : 0;
              const isFull = res.active >= res.limit && res.limit > 0;
              const resourceId = res.id || `Server #${res.index}`;
              const driverName = res.driver || (res.agy ? 'agy' : (res.opencode ? 'opencode' : (res.native ? 'native' : 'custom')));
              const modelName = res.model || res.agy || res.opencode || res.native || res.claude || res.codex || '-';

              return (
                <div
                  key={res.index}
                  className={`code-debug-resource-card ${isFull ? 'code-debug-resource-card--full' : ''}`}
                >
                  <div className="code-debug-resource-card__head">
                    <div className="code-debug-resource-card__id">
                      <span>{resourceId}</span>
                      <span style={{
                        fontSize: '0.7rem',
                        padding: '1px 6px',
                        borderRadius: '4px',
                        background: driverName === 'native' ? 'rgba(168, 85, 247, 0.15)' : 'rgba(37, 99, 235, 0.15)',
                        color: driverName === 'native' ? '#a855f7' : '#2563eb',
                        fontWeight: 600,
                        textTransform: 'uppercase'
                      }}>
                        {driverName}
                      </span>
                    </div>
                    <span
                      className="code-debug-resource-card__slots"
                      style={{
                        color: isFull ? '#ef4444' : (res.active > 0 ? '#2563eb' : 'var(--color-text-secondary, #64748b)')
                      }}
                    >
                      槽位: {res.active} / {res.limit} <span style={{ fontSize: '0.75rem', fontWeight: 400 }}>(原始: {res.concurrent})</span>
                    </span>
                  </div>

                  {/* 槽位进度条 */}
                  <div className="code-debug-progress-bar">
                    <div
                      className="code-debug-progress-fill"
                      style={{
                        width: `${percent}%`,
                        background: isFull ? '#ef4444' : (percent > 60 ? '#f59e0b' : '#2563eb')
                      }}
                    />
                  </div>

                  {/* 绑定模型 */}
                  <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', fontSize: '0.8rem' }}>
                    <span style={{ color: 'var(--color-text-secondary, #64748b)' }}>绑定模型:</span>
                    <span style={{ fontFamily: 'monospace', fontWeight: 600, color: 'var(--color-text-primary, #0f172a)' }}>
                      {modelName}
                    </span>
                  </div>

                  {/* 针对 Native 算力节点内聚展示集群端点 Endpoints */}
                  {res.endpoints && res.endpoints.length > 0 && (
                    <div className="code-debug-endpoint-list">
                      <div style={{ fontSize: '0.75rem', fontWeight: 600, color: 'var(--color-text-secondary, #64748b)', marginBottom: '2px' }}>
                        集群负载均衡端点 ({res.endpoints.length} 个):
                      </div>
                      {res.endpoints.map((ep, epIdx) => (
                        <div key={epIdx} className="code-debug-endpoint-item">
                          <span style={{ fontWeight: 600, color: 'var(--color-text-primary, #0f172a)' }}>
                            {ep.name || `Endpoint #${epIdx + 1}`}
                          </span>
                          <span>并发: {ep.concurrent || '-'} · 权重: {ep.weight || 100}%</span>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </div>

      {/* 版块 2: LLM 算力池实时看板 (Live LLM Compute Pool Leases) */}
      <div className="code-debug-section">
        <div className="code-debug-section__header">
          <div className="code-debug-section__title-group">
            <h3 className="code-debug-section__title">
              LLM 算力池实时看板 (Live LLM Compute Pool Leases)
            </h3>
            <span style={{
              fontSize: '0.75rem',
              padding: '2px 8px',
              borderRadius: '999px',
              background: activeLeases.length > 0 ? 'rgba(37, 99, 235, 0.12)' : 'rgba(148, 163, 184, 0.15)',
              color: activeLeases.length > 0 ? '#2563eb' : '#64748b',
              fontWeight: 600
            }}>
              {activeLeases.length} 个算力槽位正在运行
            </span>
          </div>
          <span className="code-debug-section__desc">
            实时透视当前分配出去的每个 AI 算力槽位、对应模型、承载微任务与已运行持续时长
          </span>
        </div>

        {activeLeases.length === 0 ? (
          <div style={{ padding: '2rem 1rem' }}>
            <EmptyState
              title="当前无正在运行的 LLM 算力槽位"
              description="所有 AI 模型算力节点处于就绪空闲状态，等待任务分配。"
            />
          </div>
        ) : (
          <div style={{ overflowX: 'auto' }}>
            <table className="code-debug-table">
              <thead>
                <tr>
                  <th style={{ width: '220px' }}>分配算力节点与模型</th>
                  <th style={{ width: '180px' }}>所属任务 / 仓库</th>
                  <th style={{ width: '150px' }}>执行环节 / 阶段</th>
                  <th>当前微任务内容</th>
                  <th style={{ width: '130px' }}>已工作持续时长</th>
                </tr>
              </thead>
              <tbody>
                {activeLeases.map((lease) => {
                  const isLongRunning = lease.duration_seconds > 300; // > 5分钟
                  const isCritical = lease.duration_seconds > 900; // > 15分钟
                  const driverName = lease.driver || 'custom';

                  return (
                    <tr key={lease.lease_id}>
                      <td>
                        <div style={{ display: 'flex', flexDirection: 'column', gap: '3px' }}>
                          <div style={{ display: 'flex', alignItems: 'center', gap: '0.4rem' }}>
                            <span style={{
                              fontSize: '0.68rem',
                              padding: '1px 5px',
                              borderRadius: '3px',
                              fontWeight: 700,
                              textTransform: 'uppercase',
                              background: driverName === 'native' ? 'rgba(168, 85, 247, 0.15)' : 'rgba(37, 99, 235, 0.15)',
                              color: driverName === 'native' ? '#a855f7' : '#2563eb'
                            }}>
                              {driverName}
                            </span>
                            <span style={{ fontWeight: 600, fontSize: '0.85rem', color: 'var(--color-text-primary, #0f172a)' }}>
                              {lease.server_id || `Server #${lease.server_index}`}
                            </span>
                          </div>
                          <span style={{
                            fontFamily: 'monospace',
                            fontSize: '0.78rem',
                            color: 'var(--color-text-secondary, #64748b)'
                          }}>
                            {lease.model || '-'}
                          </span>
                        </div>
                      </td>
                      <td>
                        {lease.repo_name ? (
                          <div style={{ display: 'flex', flexDirection: 'column', gap: '2px' }}>
                            <div style={{ display: 'flex', alignItems: 'center', gap: '0.35rem' }}>
                              <svg width="14" height="14" fill="currentColor" viewBox="0 0 24 24" style={{ color: 'var(--color-text-secondary, #64748b)', flexShrink: 0 }}>
                                <path d="M12 2C6.477 2 2 6.477 2 12c0 4.42 2.865 8.166 6.839 9.489.5.092.682-.217.682-.482 0-.237-.008-.866-.013-1.7-2.782.603-3.369-1.34-3.369-1.34-.454-1.156-1.11-1.463-1.11-1.463-.908-.62.069-.608.069-.608 1.003.07 1.53 1.03 1.53 1.03.892 1.529 2.341 1.087 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.11-4.555-4.943 0-1.091.39-1.984 1.029-2.683-.103-.253-.446-1.27.098-2.647 0 0 .84-.269 2.75 1.025A9.564 9.564 0 0112 6.844c.85.004 1.705.115 2.504.337 1.909-1.294 2.747-1.025 2.747-1.025.546 1.377.203 2.394.1 2.647.64.699 1.028 1.592 1.028 2.683 0 3.842-2.339 4.687-4.566 4.935.359.309.678.919.678 1.852 0 1.336-.012 2.415-.012 2.743 0 .267.18.578.688.48C19.138 20.161 22 16.416 22 12c0-5.523-4.477-10-10-10z" />
                              </svg>
                              <strong style={{ fontSize: '0.85rem', color: 'var(--color-text-primary, #0f172a)' }}>
                                {lease.repo_name}
                              </strong>
                              {lease.report_id > 0 && (
                                <span style={{ fontFamily: 'monospace', fontSize: '0.75rem', color: 'var(--color-text-secondary, #64748b)' }}>
                                  #{lease.report_id}
                                </span>
                              )}
                            </div>
                            {lease.task_type && (
                              <span style={{
                                fontSize: '0.72rem',
                                color: 'var(--color-text-secondary, #64748b)'
                              }}>
                                {lease.task_type}
                              </span>
                            )}
                          </div>
                        ) : (
                          <span style={{ fontSize: '0.8rem', color: 'var(--color-text-secondary, #64748b)' }}>
                            系统后台任务
                          </span>
                        )}
                      </td>
                      <td>
                        <span style={{
                          fontSize: '0.75rem',
                          padding: '3px 8px',
                          borderRadius: '6px',
                          fontWeight: 600,
                          background: lease.stage.includes('Hunter') || lease.stage.includes('初筛')
                            ? 'rgba(37, 99, 235, 0.12)'
                            : lease.stage.includes('Challenger') || lease.stage.includes('辩护')
                            ? 'rgba(168, 85, 247, 0.12)'
                            : lease.stage.includes('Judge') || lease.stage.includes('终审')
                            ? 'rgba(99, 102, 241, 0.12)'
                            : lease.stage.includes('Synthesis') || lease.stage.includes('汇总')
                            ? 'rgba(16, 185, 129, 0.12)'
                            : 'var(--color-bg-muted, #f1f5f9)',
                          color: lease.stage.includes('Hunter') || lease.stage.includes('初筛')
                            ? '#2563eb'
                            : lease.stage.includes('Challenger') || lease.stage.includes('辩护')
                            ? '#a855f7'
                            : lease.stage.includes('Judge') || lease.stage.includes('终审')
                            ? '#6366f1'
                            : lease.stage.includes('Synthesis') || lease.stage.includes('汇总')
                            ? '#10b981'
                            : 'var(--color-text-primary, #0f172a)'
                        }}>
                          {lease.stage || '推理执行中'}
                        </span>
                      </td>
                      <td>
                        <span style={{ fontSize: '0.82rem', color: 'var(--color-text-primary, #0f172a)' }}>
                          {lease.sub_task || lease.detail || '-'}
                        </span>
                      </td>
                      <td>
                        <div style={{ display: 'flex', alignItems: 'center', gap: '0.35rem' }}>
                          <span style={{
                            fontFamily: 'monospace',
                            fontWeight: 600,
                            fontSize: '0.85rem',
                            color: isCritical
                              ? 'var(--color-danger, #ef4444)'
                              : (isLongRunning ? 'var(--color-warning, #f59e0b)' : 'var(--color-text-primary, #0f172a)')
                          }}>
                            {formatDuration(lease.duration_seconds)}
                          </span>
                          {isCritical && (
                            <span title="持续执行超过 15 分钟，疑似超长任务或异常堵塞" style={{ color: 'var(--color-danger, #ef4444)' }}>
                              ⚠️
                            </span>
                          )}
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* 版块 3: 多智能体对抗辩论流水线阶梯编排与微任务路由 */}
      <div className="code-debug-section">
        <div className="code-debug-section__header">
          <div className="code-debug-section__title-group">
            <h3 className="code-debug-section__title">
              多智能体对抗辩论流水线 (Debate Pipeline) 阶梯编排与微任务路由
            </h3>
            <span style={{
              fontSize: '0.75rem',
              padding: '2px 8px',
              borderRadius: '6px',
              background: data?.debate_pipeline?.fast_pass_enabled ? 'rgba(16, 185, 129, 0.15)' : 'rgba(148, 163, 184, 0.15)',
              color: data?.debate_pipeline?.fast_pass_enabled ? '#10b981' : '#64748b',
              fontWeight: 600
            }}>
              {data?.debate_pipeline?.fast_pass_enabled ? '0 候选快速放行已开启 (节能 80%+)' : '全候选辩论'}
            </span>
          </div>

          <span className="code-debug-section__desc">
            单阶段超时兜底: {data?.debate_pipeline?.stage_timeout_seconds || 1800} 秒 · 背压阈值: {data?.debate_pipeline?.backpressure_threshold || 30} 分片
          </span>
        </div>

        <div className="code-debug-tiers-flow">
          {/* Tier 1 Hunter */}
          <div className="code-debug-tier-box">
            <div className="code-debug-tier-box__head">
              <strong style={{ fontSize: '0.95rem', color: 'var(--color-text-primary, #0f172a)' }}>
                Tier 1: 初筛猎手 (Hunter)
              </strong>
              <div style={{ display: 'flex', gap: '0.3rem', flexWrap: 'wrap' }}>
                {(data?.debate_pipeline?.tiers?.tier1_hunter?.resources?.length
                  ? data.debate_pipeline.tiers.tier1_hunter.resources
                  : [data?.debate_pipeline?.tiers?.tier1_hunter?.resource || 'agy']
                ).map(r => (
                  <span key={r} className="code-debug-tier-box__badge">
                    {r}
                  </span>
                ))}
              </div>
            </div>
            <div className="code-debug-tier-box__role">
              角色职责：Thick Agent 自主遍历文件树，初筛可疑代码片段并生成初始候选点
            </div>
            <div className="code-debug-tier-box__meta">
              <span>单片时限: {data?.debate_pipeline?.tiers?.tier1_hunter?.timeout_seconds || 1200} 秒</span>
              <span>执行引擎: Thick Agent {(data?.debate_pipeline?.tiers?.tier1_hunter?.resources?.length || 0) > 1 ? '(多节点负载打散)' : ''}</span>
            </div>
          </div>

          {/* Tier 2 Reasoning */}
          <div className="code-debug-tier-box">
            <div className="code-debug-tier-box__head">
              <strong style={{ fontSize: '0.95rem', color: 'var(--color-text-primary, #0f172a)' }}>
                Tier 2: 深度对抗与裁决 (Reasoning)
              </strong>
              <div style={{ display: 'flex', gap: '0.3rem', flexWrap: 'wrap' }}>
                {(data?.debate_pipeline?.tiers?.tier2_reasoning?.resources?.length
                  ? data.debate_pipeline.tiers.tier2_reasoning.resources
                  : [data?.debate_pipeline?.tiers?.tier2_reasoning?.resource || 'agy']
                ).map(r => (
                  <span key={r} className="code-debug-tier-box__badge" style={{ background: 'rgba(168, 85, 247, 0.12)', color: '#a855f7' }}>
                    {r}
                  </span>
                ))}
              </div>
            </div>
            <div className="code-debug-tier-box__role">
              角色职责：统一承载 Challenger 辩护与 Judge 终审，事实链交叉推演与反向仲裁
            </div>
            <div className="code-debug-tier-box__meta">
              <span>单片时限: {data?.debate_pipeline?.tiers?.tier2_reasoning?.timeout_seconds || 1800} 秒</span>
              <span>执行引擎: 逻辑强推理 {(data?.debate_pipeline?.tiers?.tier2_reasoning?.resources?.length || 0) > 1 ? '(候选池分流)' : ''}</span>
            </div>
          </div>

          {/* Tier 3 Synthesis */}
          <div className="code-debug-tier-box">
            <div className="code-debug-tier-box__head">
              <strong style={{ fontSize: '0.95rem', color: 'var(--color-text-primary, #0f172a)' }}>
                Tier 3: 全仓态势汇总 (Synthesis)
              </strong>
              <div style={{ display: 'flex', gap: '0.3rem', flexWrap: 'wrap' }}>
                {(data?.debate_pipeline?.tiers?.tier3_synthesis?.resources?.length
                  ? data.debate_pipeline.tiers.tier3_synthesis.resources
                  : [data?.debate_pipeline?.tiers?.tier3_synthesis?.resource || 'native']
                ).map(r => (
                  <span key={r} className="code-debug-tier-box__badge" style={{ background: 'rgba(16, 185, 129, 0.12)', color: '#10b981' }}>
                    {r}
                  </span>
                ))}
              </div>
            </div>
            <div className="code-debug-tier-box__role">
              角色职责：纯文本与 JSON 结构化排版汇总，风险评分与全仓扫描诊断报告聚合
            </div>
            <div className="code-debug-tier-box__meta">
              <span>汇总时限: {data?.debate_pipeline?.tiers?.tier3_synthesis?.timeout_seconds || 300} 秒</span>
              <span>执行引擎: Thin LLM 原生直连</span>
            </div>
          </div>
        </div>

        {/* 微任务工具路由 */}
        <div style={{
          marginTop: '0.5rem',
          padding: '0.85rem 1rem',
          borderRadius: '8px',
          background: 'var(--color-bg-muted, #f8fafc)',
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          flexWrap: 'wrap',
          gap: '0.75rem',
          fontSize: '0.8rem'
        }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <span style={{ fontWeight: 600, color: 'var(--color-text-primary, #0f172a)' }}>场景微任务专有路由:</span>
            <span style={{ color: 'var(--color-text-secondary, #64748b)' }}>默认走 {data?.debate_pipeline?.tools?.default_resource || 'native'}</span>
          </div>
          <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap' }}>
            <span style={{ padding: '2px 8px', borderRadius: '4px', background: 'var(--color-bg-surface, #fff)', border: '1px solid var(--color-border-primary, #e2e8f0)', color: 'var(--color-text-secondary, #64748b)' }}>
              JSON 语法修复 ➔ <strong>native</strong>
            </span>
            <span style={{ padding: '2px 8px', borderRadius: '4px', background: 'var(--color-bg-surface, #fff)', border: '1px solid var(--color-border-primary, #e2e8f0)', color: 'var(--color-text-secondary, #64748b)' }}>
              缺陷指纹语义比对 ➔ <strong>native</strong>
            </span>
            <span style={{ padding: '2px 8px', borderRadius: '4px', background: 'var(--color-bg-surface, #fff)', border: '1px solid var(--color-border-primary, #e2e8f0)', color: 'var(--color-text-secondary, #64748b)' }}>
              研发负样本特征提炼 ➔ <strong>native</strong>
            </span>
          </div>
        </div>
      </div>
    </div>
  );
}
