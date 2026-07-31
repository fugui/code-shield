import React, { useState, useEffect } from 'react';
import { useToast } from '../components/Toast';

interface AuditLogItem {
  id: number;
  trigger_batch: string;
  trigger_type: string;
  operator_id?: number;
  operator_name: string;
  task_type_id: number;
  task_type?: {
    id: number;
    display_name: string;
    name: string;
  };
  target_mode: string;
  target_summary: string;
  schedule_id?: number;
  schedule?: {
    id: number;
    name: string;
  };
  total_repos: number;
  success_count: number;
  skip_count: number;
  client_ip: string;
  remark: string;
  created_at: string;
}

interface ExecLogDetail {
  id: number;
  repo_id: number;
  task_report_id?: number;
  repo: {
    id: number;
    name: string;
    url: string;
    branch: string;
    department?: { name: string };
  };
  status: string;
  start_time: string;
  end_time?: string;
  error_message?: string;
  task_report?: {
    id: number;
    score: number;
    status: string;
    ai_summary: string;
  };
}

interface AuditStats {
  total_batches: number;
  today_batches: number;
  manual_count: number;
  cron_count: number;
  total_repos_scanned: number;
}

function AuditLogs() {
  const { showToast } = useToast();
  const [logs, setLogs] = useState<AuditLogItem[]>([]);
  const [stats, setStats] = useState<AuditStats | null>(null);
  const [loading, setLoading] = useState<boolean>(true);
  const [taskTypes, setTaskTypes] = useState<any[]>([]);

  // Filtering & Pagination State
  const [triggerType, setTriggerType] = useState<string>('');
  const [selectedTaskTypeId, setSelectedTaskTypeId] = useState<string>('');
  const [search, setSearch] = useState<string>('');
  const [page, setPage] = useState<number>(1);
  const [pageSize] = useState<number>(15);
  const [total, setTotal] = useState<number>(0);

  // Drawer Detail State
  const [selectedLog, setSelectedLog] = useState<AuditLogItem | null>(null);
  const [drawerOpen, setDrawerOpen] = useState<boolean>(false);
  const [detailExecLogs, setDetailExecLogs] = useState<ExecLogDetail[]>([]);
  const [loadingDetail, setLoadingDetail] = useState<boolean>(false);

  const fetchStats = async () => {
    try {
      const res = await fetch('/api/audit-logs/stats');
      if (res.ok) setStats(await res.json());
    } catch (err) {
      console.error(err);
    }
  };

  const fetchTaskTypes = async () => {
    try {
      const res = await fetch('/api/task-types');
      if (res.ok) setTaskTypes(await res.json());
    } catch (err) {
      console.error(err);
    }
  };

  const fetchLogs = async () => {
    setLoading(true);
    try {
      const params = new URLSearchParams();
      params.append('page', String(page));
      params.append('pageSize', String(pageSize));
      if (triggerType) params.append('trigger_type', triggerType);
      if (selectedTaskTypeId) params.append('task_type_id', selectedTaskTypeId);
      if (search) params.append('search', search);

      const res = await fetch(`/api/audit-logs?${params.toString()}`);
      if (res.ok) {
        const data = await res.json();
        setLogs(data.items || []);
        setTotal(data.total || 0);
      }
    } catch (err) {
      console.error(err);
      showToast('获取操作审计日志失败', 'error');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchStats();
    fetchTaskTypes();
  }, []);

  useEffect(() => {
    fetchLogs();
  }, [page, triggerType, selectedTaskTypeId, search]);

  const handleOpenDetail = async (item: AuditLogItem) => {
    setSelectedLog(item);
    setDrawerOpen(true);
    setLoadingDetail(true);
    try {
      const res = await fetch(`/api/audit-logs/${item.id}`);
      if (res.ok) {
        const data = await res.json();
        setDetailExecLogs(data.execution_logs || []);
      }
    } catch (err) {
      console.error(err);
      showToast('加载关联任务明细失败', 'error');
    } finally {
      setLoadingDetail(false);
    }
  };

  const renderTriggerTypeBadge = (type: string) => {
    switch (type) {
      case 'manual_single':
        return (
          <span className="audit-badge audit-badge-blue">
            <svg style={{ width: '13px', height: '13px' }} fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" />
            </svg>
            手动单仓触发
          </span>
        );
      case 'manual_batch':
        return (
          <span className="audit-badge audit-badge-purple">
            <svg style={{ width: '13px', height: '13px' }} fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10" />
            </svg>
            批量快速补扫
          </span>
        );
      case 'cron_auto':
        return (
          <span className="audit-badge audit-badge-green">
            <svg style={{ width: '13px', height: '13px' }} fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            定时自动触发
          </span>
        );
      case 'cron_manual':
        return (
          <span className="audit-badge audit-badge-amber">
            <svg style={{ width: '13px', height: '13px' }} fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z" />
            </svg>
            定时策略手动触发
          </span>
        );
      default:
        return <span className="audit-badge audit-badge-gray">{type}</span>;
    }
  };

  const renderExecStatusBadge = (status: string) => {
    switch (status) {
      case 'success':
        return <span className="status-pill status-pill-success">成功</span>;
      case 'failed':
        return <span className="status-pill status-pill-danger">失败</span>;
      case 'synthesis':
        return <span className="status-pill status-pill-purple">报告总结中</span>;
      case 'analyzing':
      case 'cloning':
      case 'pre_processing':
      case 'post_processing':
      case 'merging':
      case 'running':
        return <span className="status-pill status-pill-primary">进行中</span>;
      case 'pending':
      case 'queued':
        return <span className="status-pill status-pill-warning">排队中</span>;
      case 'skipped':
        return <span className="status-pill status-pill-gray">跳过</span>;
      default:
        return <span className="status-pill status-pill-gray">{status}</span>;
    }
  };

  const totalPages = Math.ceil(total / pageSize) || 1;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '1.25rem' }}>
      {/* 顶部统计卡片 */}
      {stats && (
        <div className="audit-stats-grid">
          <div className="audit-card">
            <div>
              <div style={{ fontSize: '0.75rem', fontWeight: 600, color: '#64748b', textTransform: 'uppercase', tracking: '0.05em' }}>今日触发批次</div>
              <div style={{ fontSize: '1.6rem', fontWeight: 800, color: '#0f172a', marginTop: '0.2rem' }}>{stats.today_batches}</div>
              <div style={{ fontSize: '0.75rem', color: '#94a3b8', marginTop: '0.2rem' }}>历史总计 {stats.total_batches} 次下发</div>
            </div>
            <div className="audit-card-icon" style={{ background: '#eff6ff', color: '#2563eb' }}>
              <svg style={{ width: '22px', height: '22px' }} fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M13 10V3L4 14h7v7l9-11h-7z" />
              </svg>
            </div>
          </div>

          <div className="audit-card">
            <div>
              <div style={{ fontSize: '0.75rem', fontWeight: 600, color: '#64748b', textTransform: 'uppercase', tracking: '0.05em' }}>手动操作 (单仓/批量)</div>
              <div style={{ fontSize: '1.6rem', fontWeight: 800, color: '#7c3aed', marginTop: '0.2rem' }}>{stats.manual_count}</div>
              <div style={{ fontSize: '0.75rem', color: '#94a3b8', marginTop: '0.2rem' }}>人类主动交互下发</div>
            </div>
            <div className="audit-card-icon" style={{ background: '#f5f3ff', color: '#7c3aed' }}>
              <svg style={{ width: '22px', height: '22px' }} fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" />
              </svg>
            </div>
          </div>

          <div className="audit-card">
            <div>
              <div style={{ fontSize: '0.75rem', fontWeight: 600, color: '#64748b', textTransform: 'uppercase', tracking: '0.05em' }}>定时任务触发</div>
              <div style={{ fontSize: '1.6rem', fontWeight: 800, color: '#059669', marginTop: '0.2rem' }}>{stats.cron_count}</div>
              <div style={{ fontSize: '0.75rem', color: '#94a3b8', marginTop: '0.2rem' }}>自动 Cron 策略下发</div>
            </div>
            <div className="audit-card-icon" style={{ background: '#ecfdf5', color: '#059669' }}>
              <svg style={{ width: '22px', height: '22px' }} fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
            </div>
          </div>

          <div className="audit-card">
            <div>
              <div style={{ fontSize: '0.75rem', fontWeight: 600, color: '#64748b', textTransform: 'uppercase', tracking: '0.05em' }}>触发累积覆盖仓库</div>
              <div style={{ fontSize: '1.6rem', fontWeight: 800, color: '#d97706', marginTop: '0.2rem' }}>{stats.total_repos_scanned}</div>
              <div style={{ fontSize: '0.75rem', color: '#94a3b8', marginTop: '0.2rem' }}>累积扫描任务项</div>
            </div>
            <div className="audit-card-icon" style={{ background: '#fffbeb', color: '#d97706' }}>
              <svg style={{ width: '22px', height: '22px' }} fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2" />
              </svg>
            </div>
          </div>
        </div>
      )}

      {/* 过滤工具栏 */}
      <div style={{ background: '#ffffff', padding: '1rem', borderRadius: '12px', border: '1px solid var(--border-color, #e2e8f0)', boxShadow: '0 1px 3px rgba(0,0,0,0.04)' }}>
        <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', justifyBetween: 'space-between', gap: '1rem' }}>
          <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: '0.75rem', flex: 1 }}>
            {/* 触发方式 */}
            <select
              value={triggerType}
              onChange={(e) => { setTriggerType(e.target.value); setPage(1); }}
              style={{ padding: '0.45rem 0.75rem', fontSize: '0.875rem', border: '1px solid var(--border-color, #cbd5e1)', borderRadius: '6px', outline: 'none', background: '#fff' }}
            >
              <option value="">全部触发方式</option>
              <option value="manual">手动触发 (全部)</option>
              <option value="manual_single">手动单仓触发</option>
              <option value="manual_batch">批量快速补扫</option>
              <option value="cron">定时任务 (全部)</option>
              <option value="cron_auto">定时任务(自动)</option>
              <option value="cron_manual">定时策略手动触发</option>
            </select>

            {/* 任务类型 */}
            <select
              value={selectedTaskTypeId}
              onChange={(e) => { setSelectedTaskTypeId(e.target.value); setPage(1); }}
              style={{ padding: '0.45rem 0.75rem', fontSize: '0.875rem', border: '1px solid var(--border-color, #cbd5e1)', borderRadius: '6px', outline: 'none', background: '#fff' }}
            >
              <option value="">全部任务类型</option>
              {taskTypes.map((tt) => (
                <option key={tt.id} value={tt.id}>{tt.display_name}</option>
              ))}
            </select>

            {/* 搜索框 */}
            <div style={{ position: 'relative', display: 'inline-flex', alignItems: 'center' }}>
              <input
                type="text"
                placeholder="搜索批次号 / 操作人 / 摘要..."
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && fetchLogs()}
                style={{ padding: '0.45rem 0.75rem 0.45rem 2rem', fontSize: '0.875rem', border: '1px solid var(--border-color, #cbd5e1)', borderRadius: '6px', outline: 'none', width: '240px' }}
              />
              <svg style={{ width: '14px', height: '14px', position: 'absolute', left: '0.6rem', color: '#94a3b8' }} fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
              </svg>
            </div>

            <button
              onClick={() => { setTriggerType(''); setSelectedTaskTypeId(''); setSearch(''); setPage(1); }}
              style={{ padding: '0.45rem 0.75rem', fontSize: '0.875rem', border: '1px solid #cbd5e1', borderRadius: '6px', background: '#f8fafc', color: '#475569', cursor: 'pointer', fontWeight: 500 }}
            >
              重置筛选
            </button>
          </div>

          <button
            onClick={fetchLogs}
            style={{ padding: '0.45rem 0.9rem', fontSize: '0.875rem', fontWeight: 500, color: '#fff', background: 'var(--primary-color, #2563eb)', border: 'none', borderRadius: '6px', cursor: 'pointer', display: 'flex', alignItems: 'center', gap: '0.4rem' }}
          >
            <svg style={{ width: '14px', height: '14px' }} fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
            </svg>
            刷新数据
          </button>
        </div>
      </div>

      {/* 审计日志主表格 */}
      <div style={{ background: '#ffffff', borderRadius: '12px', border: '1px solid var(--border-color, #e2e8f0)', boxShadow: '0 1px 3px rgba(0,0,0,0.04)', overflow: 'hidden' }}>
        <div style={{ overflowX: 'auto' }}>
          <table className="table" style={{ width: '100%', borderCollapse: 'collapse', textAlign: 'left' }}>
            <thead>
              <tr style={{ background: '#f8fafc', borderBottom: '1px solid #e2e8f0', fontSize: '0.75rem', fontWeight: 600, color: '#64748b', textTransform: 'uppercase' }}>
                <th style={{ padding: '0.75rem 1rem' }}>触发批次 & 时间</th>
                <th style={{ padding: '0.75rem 1rem' }}>触发方式</th>
                <th style={{ padding: '0.75rem 1rem' }}>操作人 / 来源</th>
                <th style={{ padding: '0.75rem 1rem' }}>任务类型</th>
                <th style={{ padding: '0.75rem 1rem' }}>目标摘要</th>
                <th style={{ padding: '0.75rem 1rem', textAlign: 'center' }}>覆盖仓库数</th>
                <th style={{ padding: '0.75rem 1rem', textAlign: 'center' }}>下发情况</th>
                <th style={{ padding: '0.75rem 1rem', textAlign: 'right' }}>明细操作</th>
              </tr>
            </thead>
            <tbody style={{ fontSize: '0.875rem' }}>
              {loading ? (
                <tr>
                  <td colSpan={8} style={{ textAlign: 'center', padding: '3rem 1rem', color: '#94a3b8' }}>
                    <div style={{ display: 'inline-flex', alignItems: 'center', gap: '0.5rem' }}>
                      <span className="spinner-mini" style={{ width: '16px', height: '16px' }} />
                      正在加载审计日志...
                    </div>
                  </td>
                </tr>
              ) : logs.length === 0 ? (
                <tr>
                  <td colSpan={8} style={{ textAlign: 'center', padding: '3rem 1rem', color: '#94a3b8' }}>
                    暂无符合要求的任务触发审计日志
                  </td>
                </tr>
              ) : (
                logs.map((item) => (
                  <tr key={item.id} style={{ borderBottom: '1px solid #f1f5f9' }}>
                    <td style={{ padding: '0.75rem 1rem' }}>
                      <div style={{ fontFamily: 'monospace', fontWeight: 600, color: '#0f172a', fontSize: '0.8rem' }}>{item.trigger_batch}</div>
                      <div style={{ fontSize: '0.75rem', color: '#94a3b8', marginTop: '0.15rem' }}>
                        {new Date(item.created_at).toLocaleString('zh-CN')}
                      </div>
                    </td>
                    <td style={{ padding: '0.75rem 1rem' }}>
                      {renderTriggerTypeBadge(item.trigger_type)}
                    </td>
                    <td style={{ padding: '0.75rem 1rem' }}>
                      <div style={{ fontWeight: 500, color: '#334155', display: 'flex', alignItems: 'center', gap: '0.4rem' }}>
                        {item.trigger_type.startsWith('cron_auto') ? (
                          <span style={{ width: '22px', height: '22px', borderRadius: '50%', background: '#d1fae5', color: '#047857', display: 'inline-flex', alignItems: 'center', justifyCenter: 'center', fontSize: '0.7rem' }}>🤖</span>
                        ) : (
                          <span style={{ width: '22px', height: '22px', borderRadius: '50%', background: '#dbeafe', color: '#1d4ed8', display: 'inline-flex', alignItems: 'center', justifyCenter: 'center', fontSize: '0.7rem', fontWeight: 700 }}>
                            {item.operator_name ? item.operator_name[0].toUpperCase() : 'U'}
                          </span>
                        )}
                        <span>{item.operator_name || '未知操作人'}</span>
                      </div>
                      {item.client_ip && (
                        <div style={{ fontSize: '0.75rem', color: '#94a3b8', fontFamily: 'monospace', marginTop: '0.1rem' }}>IP: {item.client_ip}</div>
                      )}
                    </td>
                    <td style={{ padding: '0.75rem 1rem' }}>
                      <span style={{ fontWeight: 500, color: '#334155' }}>
                        {item.task_type?.display_name || `ID: ${item.task_type_id}`}
                      </span>
                    </td>
                    <td style={{ padding: '0.75rem 1rem', maxWidth: '280px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={item.target_summary}>
                      <span style={{ color: '#475569' }}>{item.target_summary}</span>
                    </td>
                    <td style={{ padding: '0.75rem 1rem', textAlign: 'center' }}>
                      <span style={{ padding: '0.2rem 0.5rem', borderRadius: '12px', fontSize: '0.75rem', fontWeight: 600, background: '#f1f5f9', color: '#334155' }}>
                        {item.total_repos} 个仓库
                      </span>
                    </td>
                    <td style={{ padding: '0.75rem 1rem', textAlign: 'center' }}>
                      <div style={{ display: 'inline-flex', alignItems: 'center', gap: '0.4rem', fontSize: '0.75rem' }}>
                        <span style={{ color: '#059669', fontWeight: 500 }}>成功 {item.success_count}</span>
                        {item.skip_count > 0 && (
                          <span style={{ color: '#94a3b8' }}>| 跳过 {item.skip_count}</span>
                        )}
                      </div>
                    </td>
                    <td style={{ padding: '0.75rem 1rem', textAlign: 'right' }}>
                      <button
                        onClick={() => handleOpenDetail(item)}
                        style={{ padding: '0.35rem 0.65rem', fontSize: '0.75rem', fontWeight: 500, color: 'var(--primary-color, #2563eb)', background: '#eff6ff', border: '1px solid #bfdbfe', borderRadius: '6px', cursor: 'pointer' }}
                      >
                        查看涉及仓库 &rarr;
                      </button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>

        {/* 分页 */}
        {total > 0 && (
          <div style={{ padding: '0.75rem 1rem', borderTop: '1px solid #e2e8f0', background: '#f8fafc', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
            <span style={{ fontSize: '0.75rem', color: '#64748b' }}>
              显示第 {(page - 1) * pageSize + 1} 至 {Math.min(page * pageSize, total)} 条，共 {total} 条
            </span>
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
              <button
                disabled={page <= 1}
                onClick={() => setPage(page - 1)}
                style={{ padding: '0.25rem 0.6rem', fontSize: '0.75rem', border: '1px solid #cbd5e1', borderRadius: '4px', background: '#fff', cursor: page <= 1 ? 'not-allowed' : 'pointer', opacity: page <= 1 ? 0.5 : 1 }}
              >
                上一页
              </button>
              <span style={{ fontSize: '0.75rem', fontWeight: 600, color: '#334155' }}>{page} / {totalPages}</span>
              <button
                disabled={page >= totalPages}
                onClick={() => setPage(page + 1)}
                style={{ padding: '0.25rem 0.6rem', fontSize: '0.75rem', border: '1px solid #cbd5e1', borderRadius: '4px', background: '#fff', cursor: page >= totalPages ? 'not-allowed' : 'pointer', opacity: page >= totalPages ? 0.5 : 1 }}
              >
                下一页
              </button>
            </div>
          </div>
        )}
      </div>

      {/* 批次明细抽屉 Modal / Drawer */}
      {drawerOpen && selectedLog && (
        <div style={{ position: 'fixed', top: 0, left: 0, right: 0, bottom: 0, zIndex: 1000, background: 'rgba(0,0,0,0.4)', backdropFilter: 'blur(2px)', display: 'flex', justifyContent: 'flex-end' }}>
          <div style={{ width: '100%', maxWidth: '720px', background: '#ffffff', height: '100%', boxShadow: '-4px 0 24px rgba(0,0,0,0.15)', display: 'flex', flexDirection: 'column' }}>
            {/* Drawer Header */}
            <div style={{ padding: '1rem 1.25rem', borderBottom: '1px solid #e2e8f0', display: 'flex', alignItems: 'center', justifyContent: 'space-between', background: '#f8fafc' }}>
              <div>
                <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                  <h3 style={{ margin: 0, fontSize: '1.1rem', fontWeight: 700, color: '#0f172a' }}>触发批次明细与受影响代码仓</h3>
                  {renderTriggerTypeBadge(selectedLog.trigger_type)}
                </div>
                <p style={{ margin: '0.25rem 0 0 0', fontSize: '0.75rem', color: '#64748b', fontFamily: 'monospace' }}>
                  批次号: {selectedLog.trigger_batch} | 时间: {new Date(selectedLog.created_at).toLocaleString('zh-CN')}
                </p>
              </div>
              <button
                onClick={() => setDrawerOpen(false)}
                style={{ background: 'transparent', border: 'none', fontSize: '1.25rem', cursor: 'pointer', color: '#64748b', padding: '0.25rem 0.5rem', borderRadius: '4px' }}
              >
                ✕
              </button>
            </div>

            {/* Info Summary */}
            <div style={{ padding: '1rem 1.25rem', background: '#f0f9ff', borderBottom: '1px solid #e0f2fe', display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.75rem', fontSize: '0.875rem' }}>
              <div>
                <span style={{ fontSize: '0.75rem', color: '#64748b' }}>操作人 / 客户端 IP:</span>
                <p style={{ margin: '0.1rem 0 0 0', fontWeight: 600, color: '#0f172a' }}>{selectedLog.operator_name} ({selectedLog.client_ip || '内部/System'})</p>
              </div>
              <div>
                <span style={{ fontSize: '0.75rem', color: '#64748b' }}>任务类型:</span>
                <p style={{ margin: '0.1rem 0 0 0', fontWeight: 600, color: '#0f172a' }}>{selectedLog.task_type?.display_name}</p>
              </div>
              <div style={{ gridColumn: 'span 2' }}>
                <span style={{ fontSize: '0.75rem', color: '#64748b' }}>触发目标与筛选摘要:</span>
                <p style={{ margin: '0.1rem 0 0 0', fontWeight: 500, color: '#334155' }}>{selectedLog.target_summary}</p>
              </div>
            </div>

            {/* Sub-tasks list */}
            <div style={{ flex: 1, overflowY: 'auto', padding: '1.25rem', display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', fontSize: '0.875rem', fontWeight: 700, color: '#0f172a' }}>
                <span>下发的仓库任务列表 ({detailExecLogs.length} 个)</span>
                <span style={{ fontSize: '0.75rem', fontWeight: 400, color: '#64748b' }}>
                  成功排队: {selectedLog.success_count} / 跳过: {selectedLog.skip_count}
                </span>
              </div>

              {loadingDetail ? (
                <div style={{ textAlign: 'center', padding: '3rem', color: '#94a3b8' }}>正在获取关联仓库任务...</div>
              ) : detailExecLogs.length === 0 ? (
                <div style={{ textAlign: 'center', padding: '3rem', color: '#94a3b8', border: '1px dashed #cbd5e1', borderRadius: '8px' }}>
                  未查到由该批次直接创建的仓库子任务日志（可能任务被直接跳过或被用户手工清除）
                </div>
              ) : (
                <div style={{ display: 'flex', flexDirection: 'column', gap: '0.65rem' }}>
                  {detailExecLogs.map((logItem) => (
                    <div key={logItem.id} style={{ padding: '0.85rem 1rem', borderRadius: '8px', border: '1px solid #e2e8f0', background: '#fff', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                      <div style={{ display: 'flex', flexDirection: 'column', gap: '0.2rem' }}>
                        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                          <span style={{ fontWeight: 700, color: '#0f172a', fontSize: '0.9rem' }}>{logItem.repo?.name || `Repo #${logItem.repo_id}`}</span>
                          {logItem.repo?.branch && (
                            <span style={{ fontSize: '0.75rem', fontFamily: 'monospace', background: '#f1f5f9', color: '#475569', padding: '0.1rem 0.35rem', borderRadius: '4px' }}>
                              {logItem.repo.branch}
                            </span>
                          )}
                          {renderExecStatusBadge(logItem.status)}
                        </div>
                        {logItem.repo?.url && (
                          <p style={{ margin: 0, fontSize: '0.75rem', color: '#94a3b8', fontFamily: 'monospace' }}>{logItem.repo.url}</p>
                        )}
                        {logItem.error_message && (
                          <p style={{ margin: '0.2rem 0 0 0', fontSize: '0.75rem', color: '#dc2626', background: '#fef2f2', padding: '0.25rem 0.5rem', borderRadius: '4px' }}>{logItem.error_message}</p>
                        )}
                      </div>

                      <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
                        {logItem.task_report && (
                          <div style={{ textAlign: 'center', padding: '0.2rem 0.5rem', background: '#f8fafc', borderRadius: '6px' }}>
                            <div style={{ fontSize: '0.7rem', color: '#94a3b8' }}>得分</div>
                            <div style={{ fontSize: '0.9rem', fontWeight: 700, color: '#2563eb' }}>{logItem.task_report.score}</div>
                          </div>
                        )}
                        {logItem.task_report_id && (
                          <a
                            href={`/reports?report_id=${logItem.task_report_id}`}
                            target="_blank"
                            rel="noreferrer"
                            style={{ padding: '0.3rem 0.6rem', fontSize: '0.75rem', fontWeight: 500, color: '#2563eb', border: '1px solid #bfdbfe', borderRadius: '6px', textDecoration: 'none', background: '#eff6ff' }}
                          >
                            查看报告 &rarr;
                          </a>
                        )}
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>
        </div>
      )}

      {/* Audit Logs Specific Styles */}
      <style>{`
        .audit-stats-grid {
          display: grid;
          grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
          gap: 1rem;
        }
        .audit-card {
          background: #ffffff;
          padding: 1.25rem;
          border-radius: 12px;
          border: 1px solid var(--border-color, #e2e8f0);
          box-shadow: 0 1px 3px rgba(0,0,0,0.04);
          display: flex;
          align-items: center;
          justify-content: space-between;
        }
        .audit-card-icon {
          width: 44px;
          height: 44px;
          border-radius: 10px;
          display: flex;
          align-items: center;
          justify-content: center;
          flex-shrink: 0;
        }
        .audit-card-icon svg {
          width: 22px !important;
          height: 22px !important;
          max-width: 22px !important;
          max-height: 22px !important;
        }
        .audit-badge {
          display: inline-flex;
          align-items: center;
          gap: 0.35rem;
          padding: 0.2rem 0.55rem;
          border-radius: 12px;
          font-size: 0.75rem;
          font-weight: 500;
          line-height: 1.2;
        }
        .audit-badge svg {
          width: 13px !important;
          height: 13px !important;
          max-width: 13px !important;
          max-height: 13px !important;
          flex-shrink: 0;
        }
        .audit-badge-blue { background: #eff6ff; color: #1d4ed8; border: 1px solid #bfdbfe; }
        .audit-badge-purple { background: #f5f3ff; color: #6d28d9; border: 1px solid #ddd6fe; }
        .audit-badge-green { background: #ecfdf5; color: #047857; border: 1px solid #a7f3d0; }
        .audit-badge-amber { background: #fffbeb; color: #b45309; border: 1px solid #fde68a; }
        .audit-badge-gray { background: #f1f5f9; color: #334155; border: 1px solid #e2e8f0; }

        .status-pill {
          display: inline-block;
          padding: 0.15rem 0.45rem;
          border-radius: 4px;
          font-size: 0.75rem;
          font-weight: 500;
        }
        .status-pill-success { background: #d1fae5; color: #065f46; }
        .status-pill-danger { background: #fee2e2; color: #991b1b; }
        .status-pill-primary { background: #dbeafe; color: #1e40af; }
        .status-pill-purple { background: #f3e8ff; color: #6b21a8; }
        .status-pill-warning { background: #fef3c7; color: #92400e; }
        .status-pill-gray { background: #f1f5f9; color: #475569; }
      `}</style>
    </div>
  );
}

export default AuditLogs;
