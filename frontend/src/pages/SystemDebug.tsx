import React, { useState, useEffect, useCallback, useRef } from 'react';
import { useToast } from '../components/Toast';
import { Drawer, EmptyState } from '@code/common';

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

interface ModelResourceStatus {
  index: number;
  opencode: string;
  claude: string;
  codex: string;
  agy: string;
  native: string;
  concurrent: number;
  active: number;
  limit: number;
}

interface ThrottleInfo {
  effective_scale: number;
  throttle_mode: string;
  manual_scale: number;
  scale_expires_at: string | null;
  is_manual: boolean;
  is_work_hours: boolean;
}

interface GoroutineCluster {
  state: string;
  key_function: string;
  location: string;
  count: number;
  sample_stack: string;
  goroutine_ids: number[];
}

interface DebugOverviewData {
  system: SystemInfo;
  memory: MemoryInfo;
  dispatcher: {
    throttle_info: ThrottleInfo;
    resources: ModelResourceStatus[];
  };
  goroutines: {
    total: number;
    clusters: GoroutineCluster[];
  };
}

export default function SystemDebug() {
  const { showToast } = useToast();
  const [data, setData] = useState<DebugOverviewData | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshInterval, setRefreshInterval] = useState<number>(0); // 0 = off, 5/10/30 sec
  const [lastUpdated, setLastUpdated] = useState<Date>(new Date());
  const [selectedCluster, setSelectedCluster] = useState<GoroutineCluster | null>(null);
  const [searchKeyword, setSearchKeyword] = useState('');
  const [selectedStateFilter, setSelectedStateFilter] = useState<string>('all');
  const [gcTriggering, setGcTriggering] = useState(false);
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

  // 轮询定时器
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

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text).then(() => {
      showToast('堆栈内容已复制到剪贴板', 'success');
    }).catch(() => {
      showToast('复制失败，请手动选择复制', 'error');
    });
  };

  // 过滤 Goroutine 聚类
  const filteredClusters = (data?.goroutines.clusters || []).filter(c => {
    if (selectedStateFilter !== 'all' && c.state !== selectedStateFilter) {
      return false;
    }
    if (!searchKeyword.trim()) return true;
    const kw = searchKeyword.toLowerCase();
    return (
      c.key_function.toLowerCase().includes(kw) ||
      c.state.toLowerCase().includes(kw) ||
      c.location.toLowerCase().includes(kw) ||
      c.sample_stack.toLowerCase().includes(kw)
    );
  });

  const stateTypes = Array.from(new Set((data?.goroutines.clusters || []).map(c => c.state)));

  const getStateBadgeStyle = (state: string) => {
    if (state.includes('running')) return { bg: 'rgba(16, 185, 129, 0.15)', color: '#10b981', border: 'rgba(16, 185, 129, 0.3)' };
    if (state.includes('sync.Cond') || state.includes('Wait')) return { bg: 'rgba(239, 68, 68, 0.15)', color: '#ef4444', border: 'rgba(239, 68, 68, 0.3)' };
    if (state.includes('IO') || state.includes('net')) return { bg: 'rgba(59, 130, 246, 0.15)', color: '#3b82f6', border: 'rgba(59, 130, 246, 0.3)' };
    if (state.includes('chan') || state.includes('select')) return { bg: 'rgba(245, 158, 11, 0.15)', color: '#f59e0b', border: 'rgba(245, 158, 11, 0.3)' };
    return { bg: 'rgba(148, 163, 184, 0.15)', color: '#94a3b8', border: 'rgba(148, 163, 184, 0.3)' };
  };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '1.5rem' }}>
      {/* 顶部状态与工具栏 */}
      <div style={{
        display: 'flex',
        justifyContent: 'space-between',
        alignItems: 'center',
        flexWrap: 'wrap',
        gap: '1rem',
        padding: '1.25rem 1.5rem',
        background: 'var(--color-bg-surface, var(--card-bg, #fff))',
        borderRadius: '12px',
        border: '1px solid var(--color-border-primary, var(--border-color, #e2e8f0))',
        boxShadow: '0 1px 3px rgba(0,0,0,0.05)'
      }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '1rem' }}>
          <div style={{
            width: '42px',
            height: '42px',
            borderRadius: '10px',
            background: 'linear-gradient(135deg, rgba(37,99,235,0.15), rgba(79,70,229,0.15))',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            color: 'var(--primary-color, #2563eb)'
          }}>
            <svg width="22" height="22" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
              <path strokeLinecap="round" strokeLinejoin="round" d="M9 3v2m6-2v2M9 19v2m6-2v2M5 9H3m2 6H3m18-6h-2m2 6h-2M7 19h10a2 2 0 002-2V7a2 2 0 00-2-2H7a2 2 0 00-2 2v10a2 2 0 002 2zM9 9h6v6H9V9z" />
            </svg>
          </div>
          <div>
            <h2 style={{ margin: 0, fontSize: '1.15rem', fontWeight: 700, color: 'var(--color-text-primary, var(--text-color, #0f172a))' }}>
              系统性能与堆栈可视化诊断
            </h2>
            <div style={{ fontSize: '0.8rem', color: 'var(--color-text-secondary, #64748b)', marginTop: '2px' }}>
              上次快照: {lastUpdated.toLocaleTimeString()} · Go Runtime 实时可观测面板
            </div>
          </div>
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', flexWrap: 'wrap' }}>
          {/* 自动刷新选择 */}
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', fontSize: '0.85rem', color: 'var(--color-text-secondary, #64748b)' }}>
            <span>自动刷新:</span>
            <select
              value={refreshInterval}
              onChange={(e) => setRefreshInterval(Number(e.target.value))}
              style={{
                padding: '0.35rem 0.6rem',
                borderRadius: '6px',
                border: '1px solid var(--color-border-primary, var(--border-color, #cbd5e1))',
                background: 'var(--color-bg-surface, var(--card-bg, #fff))',
                color: 'var(--color-text-primary, var(--text-color, #0f172a))',
                fontSize: '0.85rem'
              }}
            >
              <option value={0}>手动刷新</option>
              <option value={5}>每 5 秒</option>
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

          {/* pprof 导出下拉/按钮 */}
          <div style={{ position: 'relative', display: 'inline-block' }}>
            <button
              className="btn btn-primary"
              onClick={() => handleDownloadPprof('goroutine?debug=2', `goroutine-dump-${Date.now()}.txt`)}
              style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', padding: '0.45rem 0.9rem', fontSize: '0.85rem' }}
            >
              <svg width="15" height="15" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
                <path strokeLinecap="round" strokeLinejoin="round" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4" />
              </svg>
              导出 Goroutine Dump
            </button>
          </div>

          <button
            className="btn btn-secondary"
            onClick={() => handleDownloadPprof('heap', `heap-${Date.now()}.pb.gz`)}
            title="下载堆内存 Heap Profile (可使用 go tool pprof 分析)"
            style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', padding: '0.45rem 0.9rem', fontSize: '0.85rem' }}
          >
            导出 Heap Profile
          </button>
        </div>
      </div>

      {/* 核心指标卡片区 */}
      <div style={{
        display: 'grid',
        gridTemplateColumns: 'repeat(auto-fit, minmax(240px, 1fr))',
        gap: '1rem'
      }}>
        {/* 卡片 1: Goroutine */}
        <div style={{
          padding: '1.25rem',
          borderRadius: '12px',
          background: 'var(--color-bg-surface, var(--card-bg, #fff))',
          border: '1px solid var(--color-border-primary, var(--border-color, #e2e8f0))',
          boxShadow: '0 1px 3px rgba(0,0,0,0.05)'
        }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.5rem' }}>
            <span style={{ fontSize: '0.85rem', color: 'var(--color-text-secondary, #64748b)', fontWeight: 500 }}>活跃 Goroutines</span>
            <span style={{
              fontSize: '0.75rem',
              padding: '2px 8px',
              borderRadius: '999px',
              background: (data?.system.num_goroutine || 0) > 200 ? 'rgba(239, 68, 68, 0.15)' : 'rgba(16, 185, 129, 0.15)',
              color: (data?.system.num_goroutine || 0) > 200 ? '#ef4444' : '#10b981',
              fontWeight: 600
            }}>
              {(data?.system.num_goroutine || 0) > 200 ? '高负载/阻塞警戒' : '正常'}
            </span>
          </div>
          <div style={{ fontSize: '1.85rem', fontWeight: 800, color: 'var(--color-text-primary, var(--text-color, #0f172a))' }}>
            {data?.system.num_goroutine || 0}
          </div>
          <div style={{ fontSize: '0.75rem', color: 'var(--color-text-secondary, #64748b)', marginTop: '0.5rem' }}>
            CPU 核心数: {data?.system.num_cpu || '-'} · 聚类数: {data?.goroutines.clusters.length || 0} 类
          </div>
        </div>

        {/* 卡片 2: 内存堆使用 */}
        <div style={{
          padding: '1.25rem',
          borderRadius: '12px',
          background: 'var(--color-bg-surface, var(--card-bg, #fff))',
          border: '1px solid var(--color-border-primary, var(--border-color, #e2e8f0))',
          boxShadow: '0 1px 3px rgba(0,0,0,0.05)'
        }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.5rem' }}>
            <span style={{ fontSize: '0.85rem', color: 'var(--color-text-secondary, #64748b)', fontWeight: 500 }}>堆内存占用 (Heap Alloc)</span>
            <span style={{ fontSize: '0.75rem', color: 'var(--color-text-secondary, #64748b)' }}>Sys: {data?.memory.sys_formatted || '-'}</span>
          </div>
          <div style={{ fontSize: '1.85rem', fontWeight: 800, color: 'var(--color-text-primary, var(--text-color, #0f172a))' }}>
            {data?.memory.heap_alloc_fmt || '-'}
          </div>
          <div style={{ fontSize: '0.75rem', color: 'var(--color-text-secondary, #64748b)', marginTop: '0.5rem' }}>
            活动对象: {(data?.memory.heap_objects || 0).toLocaleString()} · 释放: {data?.memory.heap_released_fmt || '-'}
          </div>
        </div>

        {/* 卡片 3: GC 性能与延迟 */}
        <div style={{
          padding: '1.25rem',
          borderRadius: '12px',
          background: 'var(--color-bg-surface, var(--card-bg, #fff))',
          border: '1px solid var(--color-border-primary, var(--border-color, #e2e8f0))',
          boxShadow: '0 1px 3px rgba(0,0,0,0.05)'
        }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.5rem' }}>
            <span style={{ fontSize: '0.85rem', color: 'var(--color-text-secondary, #64748b)', fontWeight: 500 }}>GC 触发轮次</span>
            <span style={{ fontSize: '0.75rem', color: 'var(--color-text-secondary, #64748b)' }}>累计停顿: {data?.memory.pause_total_ms.toFixed(1)} ms</span>
          </div>
          <div style={{ fontSize: '1.85rem', fontWeight: 800, color: 'var(--color-text-primary, var(--text-color, #0f172a))' }}>
            {data?.memory.num_gc || 0} <span style={{ fontSize: '0.9rem', fontWeight: 500, color: 'var(--color-text-secondary, #64748b)' }}>次</span>
          </div>
          <div style={{ fontSize: '0.75rem', color: 'var(--color-text-secondary, #64748b)', marginTop: '0.5rem' }}>
            上次 GC: {data?.memory.last_gc_time || '未发生'}
          </div>
        </div>

        {/* 卡片 4: 系统运行时间 */}
        <div style={{
          padding: '1.25rem',
          borderRadius: '12px',
          background: 'var(--color-bg-surface, var(--card-bg, #fff))',
          border: '1px solid var(--color-border-primary, var(--border-color, #e2e8f0))',
          boxShadow: '0 1px 3px rgba(0,0,0,0.05)'
        }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.5rem' }}>
            <span style={{ fontSize: '0.85rem', color: 'var(--color-text-secondary, #64748b)', fontWeight: 500 }}>运行时间 (Uptime)</span>
            <span style={{ fontSize: '0.75rem', color: 'var(--color-text-secondary, #64748b)' }}>{data?.system.go_version || '-'}</span>
          </div>
          <div style={{ fontSize: '1.35rem', fontWeight: 800, color: 'var(--color-text-primary, var(--text-color, #0f172a))', lineHeight: 1.4 }}>
            {data?.system.uptime_formatted || '-'}
          </div>
          <div style={{ fontSize: '0.75rem', color: 'var(--color-text-secondary, #64748b)', marginTop: '0.5rem' }}>
            启动于: {data?.system.server_start_time || '-'}
          </div>
        </div>
      </div>

      {/* 模型并发调度实时大盘 (ModelDispatcher Concurrency Inspector) */}
      <div style={{
        padding: '1.5rem',
        borderRadius: '12px',
        background: 'var(--color-bg-surface, var(--card-bg, #fff))',
        border: '1px solid var(--color-border-primary, var(--border-color, #e2e8f0))',
        boxShadow: '0 1px 3px rgba(0,0,0,0.05)'
      }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.25rem', flexWrap: 'wrap', gap: '0.5rem' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <h3 style={{ margin: 0, fontSize: '1.05rem', fontWeight: 700, color: 'var(--color-text-primary, var(--text-color, #0f172a))' }}>
              LLM 模型调度器 (ModelDispatcher) 槽位实时负载
            </h3>
            <span style={{
              fontSize: '0.75rem',
              padding: '2px 8px',
              borderRadius: '6px',
              background: data?.dispatcher.throttle_info.throttle_mode === 'work_hours' ? 'rgba(245, 158, 11, 0.15)' : 'rgba(37, 99, 235, 0.1)',
              color: data?.dispatcher.throttle_info.throttle_mode === 'work_hours' ? '#f59e0b' : '#2563eb',
              fontWeight: 600
            }}>
              模式: {data?.dispatcher.throttle_info.throttle_mode === 'work_hours' ? '工作时间限流' : (data?.dispatcher.throttle_info.throttle_mode === 'manual' ? '手动覆盖' : '标准全速')}
              （比例: {((data?.dispatcher.throttle_info.effective_scale || 1) * 100).toFixed(0)}%）
            </span>
          </div>

          <span style={{ fontSize: '0.8rem', color: 'var(--color-text-secondary, #64748b)' }}>
            总纳管服务器: {data?.dispatcher.resources?.length || 0} 台
          </span>
        </div>

        {(!data?.dispatcher.resources || data.dispatcher.resources.length === 0) ? (
          <div style={{ padding: '1.5rem', textAlign: 'center', color: 'var(--color-text-secondary, #64748b)', fontSize: '0.9rem' }}>
            当前未配置多服务器模型调度器（运行在默认单直通模式）
          </div>
        ) : (
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(320px, 1fr))', gap: '1rem' }}>
            {data.dispatcher.resources.map(res => {
              const percent = res.limit > 0 ? Math.min(100, Math.round((res.active / res.limit) * 100)) : 0;
              const isFull = res.active >= res.limit && res.limit > 0;
              return (
                <div key={res.index} style={{
                  padding: '1rem',
                  borderRadius: '8px',
                  background: 'var(--color-bg-muted, rgba(248, 250, 252, 0.6))',
                  border: isFull ? '1px solid rgba(239, 68, 68, 0.4)' : '1px solid var(--color-border-primary, var(--border-color, #e2e8f0))',
                  display: 'flex',
                  flexDirection: 'column',
                  gap: '0.75rem'
                }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                    <span style={{ fontWeight: 700, fontSize: '0.95rem', color: 'var(--color-text-primary, var(--text-color, #0f172a))' }}>
                      Server #{res.index}
                    </span>
                    <span style={{
                      fontSize: '0.85rem',
                      fontWeight: 700,
                      color: isFull ? '#ef4444' : (res.active > 0 ? '#2563eb' : 'var(--color-text-secondary, #64748b)')
                    }}>
                      槽位占用: {res.active} / {res.limit} <span style={{ fontSize: '0.75rem', fontWeight: 400 }}>(原始: {res.concurrent})</span>
                    </span>
                  </div>

                  {/* 槽位进度条 */}
                  <div style={{
                    width: '100%',
                    height: '8px',
                    borderRadius: '999px',
                    background: 'var(--color-border-primary, var(--border-color, #e2e8f0))',
                    overflow: 'hidden'
                  }}>
                    <div style={{
                      width: `${percent}%`,
                      height: '100%',
                      background: isFull ? '#ef4444' : (percent > 60 ? '#f59e0b' : '#2563eb'),
                      borderRadius: '999px',
                      transition: 'width 0.3s'
                    }} />
                  </div>

                  {/* 支持的模型标签 */}
                  <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.35rem', fontSize: '0.75rem' }}>
                    {res.claude && <span style={{ padding: '2px 6px', borderRadius: '4px', background: 'rgba(59, 130, 246, 0.1)', color: '#3b82f6' }}>claude: {res.claude}</span>}
                    {res.opencode && <span style={{ padding: '2px 6px', borderRadius: '4px', background: 'rgba(16, 185, 129, 0.1)', color: '#10b981' }}>opencode: {res.opencode}</span>}
                    {res.native && <span style={{ padding: '2px 6px', borderRadius: '4px', background: 'rgba(168, 85, 247, 0.1)', color: '#a855f7' }}>native: {res.native}</span>}
                    {res.agy && <span style={{ padding: '2px 6px', borderRadius: '4px', background: 'rgba(249, 115, 22, 0.1)', color: '#f97316' }}>agy: {res.agy}</span>}
                    {res.codex && <span style={{ padding: '2px 6px', borderRadius: '4px', background: 'rgba(100, 116, 139, 0.1)', color: '#64748b' }}>codex: {res.codex}</span>}
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>

      {/* Goroutine 智能聚类透视器 (Goroutine Stack Cluster Explorer) */}
      <div style={{
        padding: '1.5rem',
        borderRadius: '12px',
        background: 'var(--color-bg-surface, var(--card-bg, #fff))',
        border: '1px solid var(--color-border-primary, var(--border-color, #e2e8f0))',
        boxShadow: '0 1px 3px rgba(0,0,0,0.05)'
      }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1rem', flexWrap: 'wrap', gap: '0.75rem' }}>
          <div>
            <h3 style={{ margin: 0, fontSize: '1.05rem', fontWeight: 700, color: 'var(--color-text-primary, var(--text-color, #0f172a))' }}>
              Goroutine 堆栈聚类与阻塞透视
            </h3>
            <p style={{ margin: '4px 0 0', fontSize: '0.8rem', color: 'var(--color-text-secondary, #64748b)' }}>
              按等待状态与核心业务调用栈自动聚类，快速识别活锁、假死与高并发阻塞热点
            </p>
          </div>

          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', flexWrap: 'wrap' }}>
            {/* 状态筛选 Tabs */}
            <div style={{ display: 'flex', gap: '0.25rem', background: 'var(--color-bg-muted, #f1f5f9)', padding: '2px', borderRadius: '6px' }}>
              <button
                onClick={() => setSelectedStateFilter('all')}
                style={{
                  padding: '4px 10px',
                  borderRadius: '4px',
                  border: 'none',
                  fontSize: '0.75rem',
                  fontWeight: selectedStateFilter === 'all' ? 600 : 400,
                  background: selectedStateFilter === 'all' ? 'var(--color-bg-surface, #fff)' : 'transparent',
                  color: selectedStateFilter === 'all' ? 'var(--color-text-primary, #0f172a)' : 'var(--color-text-secondary, #64748b)',
                  cursor: 'pointer'
                }}
              >
                全部 ({data?.goroutines.total || 0})
              </button>
              {stateTypes.map(st => (
                <button
                  key={st}
                  onClick={() => setSelectedStateFilter(st)}
                  style={{
                    padding: '4px 8px',
                    borderRadius: '4px',
                    border: 'none',
                    fontSize: '0.75rem',
                    fontWeight: selectedStateFilter === st ? 600 : 400,
                    background: selectedStateFilter === st ? 'var(--color-bg-surface, #fff)' : 'transparent',
                    color: selectedStateFilter === st ? 'var(--color-text-primary, #0f172a)' : 'var(--color-text-secondary, #64748b)',
                    cursor: 'pointer'
                  }}
                >
                  {st}
                </button>
              ))}
            </div>

            {/* 关键字搜索 */}
            <input
              type="text"
              placeholder="搜索函数 / 状态 / 行号..."
              value={searchKeyword}
              onChange={(e) => setSearchKeyword(e.target.value)}
              style={{
                padding: '0.35rem 0.65rem',
                borderRadius: '6px',
                border: '1px solid var(--color-border-primary, var(--border-color, #cbd5e1))',
                background: 'var(--color-bg-surface, var(--card-bg, #fff))',
                color: 'var(--color-text-primary, var(--text-color, #0f172a))',
                fontSize: '0.85rem',
                width: '180px'
              }}
            />
          </div>
        </div>

        {/* 聚类列表表格 */}
        {filteredClusters.length === 0 ? (
          <EmptyState title="未找到匹配的 Goroutine 堆栈" description="当前没有符合条件的协程聚类" />
        ) : (
          <div style={{ overflowX: 'auto' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse', textAlign: 'left', fontSize: '0.85rem' }}>
              <thead>
                <tr style={{ borderBottom: '1px solid var(--color-border-primary, var(--border-color, #e2e8f0))', color: 'var(--color-text-secondary, #64748b)' }}>
                  <th style={{ padding: '0.75rem 0.5rem', width: '90px' }}>协程数量</th>
                  <th style={{ padding: '0.75rem 0.5rem', width: '140px' }}>运行/等待状态</th>
                  <th style={{ padding: '0.75rem 0.5rem' }}>核心业务函数 / 栈帧特征</th>
                  <th style={{ padding: '0.75rem 0.5rem' }}>调用源行号</th>
                  <th style={{ padding: '0.75rem 0.5rem', width: '100px', textAlign: 'right' }}>操作</th>
                </tr>
              </thead>
              <tbody>
                {filteredClusters.map((cluster, idx) => {
                  const badge = getStateBadgeStyle(cluster.state);
                  return (
                    <tr
                      key={idx}
                      style={{
                        borderBottom: '1px solid var(--color-border-primary, var(--border-color, #f1f5f9))',
                        transition: 'background 0.15s'
                      }}
                    >
                      {/* 数量 */}
                      <td style={{ padding: '0.75rem 0.5rem' }}>
                        <span style={{
                          display: 'inline-flex',
                          alignItems: 'center',
                          justifyContent: 'center',
                          minWidth: '28px',
                          padding: '2px 8px',
                          borderRadius: '999px',
                          fontWeight: 700,
                          fontSize: '0.85rem',
                          background: cluster.count > 10 ? 'rgba(239, 68, 68, 0.15)' : 'rgba(37, 99, 235, 0.1)',
                          color: cluster.count > 10 ? '#ef4444' : '#2563eb'
                        }}>
                          {cluster.count}
                        </span>
                      </td>

                      {/* 状态徽章 */}
                      <td style={{ padding: '0.75rem 0.5rem' }}>
                        <span style={{
                          display: 'inline-block',
                          padding: '2px 8px',
                          borderRadius: '4px',
                          fontSize: '0.75rem',
                          fontWeight: 600,
                          background: badge.bg,
                          color: badge.color,
                          border: `1px solid ${badge.border}`
                        }}>
                          {cluster.state}
                        </span>
                      </td>

                      {/* 关键函数 */}
                      <td style={{ padding: '0.75rem 0.5rem' }}>
                        <div style={{ fontWeight: 600, color: 'var(--color-text-primary, var(--text-color, #0f172a))', fontFamily: 'monospace' }}>
                          {cluster.key_function}
                        </div>
                      </td>

                      {/* 源码位置 */}
                      <td style={{ padding: '0.75rem 0.5rem', color: 'var(--color-text-secondary, #64748b)', fontSize: '0.8rem', fontFamily: 'monospace' }}>
                        {cluster.location || '-'}
                      </td>

                      {/* 查看详情 */}
                      <td style={{ padding: '0.75rem 0.5rem', textAlign: 'right' }}>
                        <button
                          className="btn btn-secondary"
                          onClick={() => setSelectedCluster(cluster)}
                          style={{ padding: '0.25rem 0.6rem', fontSize: '0.75rem' }}
                        >
                          查看堆栈
                        </button>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* 堆栈详情抽屉 Drawer */}
      <Drawer
        open={!!selectedCluster}
        onClose={() => setSelectedCluster(null)}
        title={`Goroutine 堆栈详情 (${selectedCluster?.count || 0} 个协程)`}
        width="xl"
      >
        {selectedCluster && (
          <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem', height: '100%' }}>
            <div style={{
              display: 'flex',
              justifyContent: 'space-between',
              alignItems: 'center',
              padding: '0.75rem 1rem',
              background: 'var(--color-bg-muted, #f8fafc)',
              borderRadius: '8px',
              border: '1px solid var(--color-border-primary, var(--border-color, #e2e8f0))'
            }}>
              <div>
                <div style={{ fontSize: '0.8rem', color: 'var(--color-text-secondary, #64748b)' }}>
                  特征状态: <strong>{selectedCluster.state}</strong> · 函数: <strong>{selectedCluster.key_function}</strong>
                </div>
                <div style={{ fontSize: '0.75rem', color: 'var(--color-text-secondary, #64748b)', marginTop: '2px' }}>
                  部分 GID: {selectedCluster.goroutine_ids.join(', ')} {selectedCluster.count > selectedCluster.goroutine_ids.length ? '...' : ''}
                </div>
              </div>

              <button
                className="btn btn-primary"
                onClick={() => copyToClipboard(selectedCluster.sample_stack)}
                style={{ fontSize: '0.8rem', padding: '0.35rem 0.75rem' }}
              >
                复制完整堆栈
              </button>
            </div>

            <div style={{ flex: 1, display: 'flex', flexDirection: 'column' }}>
              <div style={{ fontSize: '0.85rem', fontWeight: 600, marginBottom: '0.5rem', color: 'var(--color-text-primary, #0f172a)' }}>
                完整 Go 调用栈 (Sample Stack Trace):
              </div>
              <pre style={{
                flex: 1,
                margin: 0,
                padding: '1rem',
                borderRadius: '8px',
                background: 'var(--color-bg-muted, #0f172a)',
                color: 'var(--color-text-primary, #e2e8f0)',
                fontFamily: 'Consolas, Monaco, "Courier New", monospace',
                fontSize: '0.8rem',
                lineHeight: 1.5,
                overflowY: 'auto',
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-all',
                border: '1px solid var(--color-border-primary, var(--border-color, #334155))'
              }}>
                {selectedCluster.sample_stack}
              </pre>
            </div>
          </div>
        )}
      </Drawer>
    </div>
  );
}
