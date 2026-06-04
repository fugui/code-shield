import React, { useEffect, useState, useCallback } from 'react';
import { useToast } from '../components/Toast';
import ReportSidebar from '../components/ReportSidebar';
import { sshToHttps } from '../utils/urlUtils';

interface ExecutionLogsProps {
  embedded?: boolean;
}

function ExecutionLogs({ embedded = false }: ExecutionLogsProps) {
  const [logs, setLogs] = useState<any[]>([]);
  const [expandedIds, setExpandedIds] = useState<Set<number>>(new Set());
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [currentMarkdown, setCurrentMarkdown] = useState('');
  const [loadingMarkdown, setLoadingMarkdown] = useState(false);
  const [currentReportId, setCurrentReportId] = useState<number | undefined>(undefined);
  const { showToast } = useToast();

  const [page, setPage] = useState<number>(1);
  const [pageSize, setPageSize] = useState<number>(15);
  const [totalItems, setTotalItems] = useState<number>(0);
  const [totalPages, setTotalPages] = useState<number>(0);

  const fetchLogs = useCallback(async () => {
    try {
      const res = await fetch(`/api/executions?page=${page}&pageSize=${pageSize}`);
      if (res.ok) {
        const data = await res.json();
        setLogs(data.items || []);
        setTotalItems(data.total || 0);
        setTotalPages(data.totalPages || 0);
      }
    } catch (err) {
      console.error('Failed to fetch execution logs:', err);
    }
  }, [page, pageSize]);

  const clearCompleted = async () => {
    if (!window.confirm('确认清除所有已完成（成功/失败/已跳过）的执行记录？进行中的任务不受影响。')) return;
    try {
      const res = await fetch('/api/executions/completed', { method: 'DELETE' });
      if (res.ok) {
        const data = await res.json();
        showToast(`已清除 ${data.deleted} 条记录`, 'success');
        fetchLogs();
      } else {
        showToast('清除失败，请稍后重试', 'error');
      }
    } catch {
      showToast('请求失败，请检查网络连接', 'error');
    }
  };

  const handleOpenReport = async (reportId: number) => {
    setSidebarOpen(true);
    setLoadingMarkdown(true);
    setCurrentMarkdown('');
    setCurrentReportId(reportId);
    try {
      const res = await fetch(`/api/tasks/${reportId}/report`);
      if (res.ok) {
        setCurrentMarkdown(await res.text());
      } else {
        const err = await res.json();
        setCurrentMarkdown(`### 获取报告失败\n\n原因: ${err.error || 'Server error'}`);
      }
    } catch {
      setCurrentMarkdown('### 获取报告失败\n\n原因: 网络请求异常。');
    } finally {
      setLoadingMarkdown(false);
    }
  };

  const deletePending = async (logId: number, isRunning: boolean) => {
    const message = isRunning
      ? '该任务正在分析运行中，确认要【强杀进程】并删除该执行记录吗？\n警告：此操作将立即中断分析任务且不可恢复。'
      : '确认删除该排队中的任务？此操作不可恢复。';
    if (!window.confirm(message)) return;
    try {
      const res = await fetch(`/api/executions/${logId}`, { method: 'DELETE' });
      const data = await res.json();
      if (res.ok) {
        showToast(data.message || '任务已删除', 'success');
        fetchLogs();
      } else {
        showToast(data.error || '删除失败', 'error');
      }
    } catch {
      showToast('网络异常，删除失败', 'error');
    }
  };

  const handleNotify = async (reportId: number) => {
    try {
      const res = await fetch(`/api/tasks/${reportId}/notify`, { method: 'POST' });
      if (res.ok) {
        showToast('通知已成功发送！', 'success');
      } else {
        const data = await res.json();
        showToast(`发送通知失败: ${data.error || '未知错误'}`, 'error');
      }
    } catch {
      showToast('网络异常，发送失败', 'error');
    }
  };

  const [summaries, setSummaries] = useState<Record<number, any>>({});
  const [loadingSummaries, setLoadingSummaries] = useState<Record<number, boolean>>({});

  const fetchSummary = async (reportId: number) => {
    setLoadingSummaries(prev => ({ ...prev, [reportId]: true }));
    try {
      const res = await fetch(`/api/tasks/${reportId}/summary`);
      if (res.ok) {
        const data = await res.json();
        setSummaries(prev => ({ ...prev, [reportId]: data }));
      }
    } catch (err) {
      console.error('Failed to fetch summary:', err);
    } finally {
      setLoadingSummaries(prev => ({ ...prev, [reportId]: false }));
    }
  };

  const toggleExpand = (id: number) => {
    setExpandedIds(prev => {
      const next = new Set(prev);
      const isExpanding = !next.has(id);
      next.has(id) ? next.delete(id) : next.add(id);

      const log = logs.find(l => l.id === id);
      if (isExpanding && log?.task_report?.id && !summaries[log.task_report.id]) {
        fetchSummary(log.task_report.id);
      }

      return next;
    });
  };

  useEffect(() => {
    fetchLogs();
    const interval = setInterval(fetchLogs, 5000);
    return () => clearInterval(interval);
  }, [fetchLogs]);

  const formatDuration = (seconds: number) => {
    if (seconds == null) return '-';
    const s = Math.round(seconds);
    return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`;
  };

  const formatDate = (dateStr: string) => {
    if (!dateStr) return '-';
    const d = new Date(dateStr);
    return isNaN(d.getTime()) ? '-' : d.toLocaleString();
  };

  const calcDuration = (startStr: string, endStr: string) => {
    if (!startStr || !endStr) return '-';
    const diff = Math.floor((new Date(endStr).getTime() - new Date(startStr).getTime()) / 1000);
    if (isNaN(diff)) return '-';
    return diff < 60 ? `${diff}s` : `${Math.floor(diff / 60)}m ${diff % 60}s`;
  };

  const statusBadge = (log: any) => {
    const status = (log.status === 'running' && log.task_report?.status) ? log.task_report.status : log.status;
    const map: Record<string, { cls: string; label: string }> = {
      success: { cls: 'success', label: '执行成功' },
      failed:  { cls: 'danger',  label: '执行失败' },
      running: { cls: 'primary', label: '运行中...' },
      skipped: { cls: 'info',    label: '已跳过' },
      pending: { cls: 'warning', label: '排队中' },
      cloning: { cls: 'primary', label: '代码克隆中...' },
      pre_processing: { cls: 'primary', label: '前置检查中...' },
      analyzing: { cls: 'primary', label: 'AI 检视中...' },
      post_processing: { cls: 'primary', label: '结果分析中...' },
    };
    const s = map[status] || { cls: 'warning', label: status };

    if (log.engine_mode === 'chunked' && status === 'analyzing' && log.task_report) {
      const { processed_chunks, total_chunks } = log.task_report;
      if (total_chunks > 0) {
        return <span className={`badge ${s.cls}`}>{`AI 检视中 (${processed_chunks}/${total_chunks})...`}</span>;
      }
    }

    return <span className={`badge ${s.cls}`}>{s.label}</span>;
  };

  return (
    <div>
      {!embedded ? (
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.5rem' }}>
          <h2 style={{ margin: 0 }}>执行日志</h2>
          <div style={{ display: 'flex', gap: '0.5rem' }}>
            <button className="btn" onClick={fetchLogs} style={{ background: 'transparent', color: 'var(--text-color)', border: '1px solid var(--border-color)' }}>
              刷新列表
            </button>
            <button className="btn" onClick={clearCompleted} style={{ background: 'transparent', color: 'var(--danger-color)', border: '1px solid var(--danger-color)' }}>
              清除已完成
            </button>
          </div>
        </div>
      ) : (
        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.5rem', marginBottom: '1.5rem' }}>
          <button className="btn" onClick={fetchLogs} style={{ background: 'transparent', color: 'var(--text-color)', border: '1px solid var(--border-color)' }}>
            刷新列表
          </button>
          <button className="btn" onClick={clearCompleted} style={{ background: 'transparent', color: 'var(--danger-color)', border: '1px solid var(--danger-color)' }}>
            清除已完成
          </button>
        </div>
      )}

      <div className="card" style={{ padding: '0', overflow: 'hidden' }}>
        <table style={{ width: '100%', borderCollapse: 'collapse' }}>
          <thead>
            <tr style={{ borderBottom: '1px solid var(--border-color)', color: '#64748b', fontSize: '0.875rem', textAlign: 'left', background: 'var(--bg-color)' }}>
              <th style={{ padding: '1rem', fontWeight: 600, width: '2rem' }}></th>
              <th style={{ padding: '1rem', fontWeight: 600 }}>任务 ID</th>
              <th style={{ padding: '1rem', fontWeight: 600 }}>所属代码仓</th>
              <th style={{ padding: '1rem', fontWeight: 600 }}>任务类型</th>
              <th style={{ padding: '1rem', fontWeight: 600 }}>触发方式</th>
              <th style={{ padding: '1rem', fontWeight: 600 }}>执行引擎</th>
              <th style={{ padding: '1rem', fontWeight: 600 }}>开始时间</th>
              <th style={{ padding: '1rem', fontWeight: 600 }}>执行耗时</th>
              <th style={{ padding: '1rem', fontWeight: 600 }}>状态</th>
              <th style={{ padding: '1rem', fontWeight: 600 }}>操作</th>
            </tr>
          </thead>
          <tbody>
            {logs.length === 0 ? (
              <tr>
                <td colSpan={10} style={{ padding: '3rem 1rem', textAlign: 'center', color: '#64748b' }}>暂无任何任务执行记录。</td>
              </tr>
            ) : logs.map(log => {
              const expanded = expandedIds.has(log.id);
              const report = log.task_report;
              const hasReport = !!report;
              const isRunning = ['running', 'cloning', 'pre_processing', 'analyzing', 'post_processing'].includes(log.status);
              const isPending = log.status === 'pending' || log.status === 'queued';
              const canCancel = isRunning || isPending;

              return (
                <React.Fragment key={log.id}>
                  <tr
                    style={{ borderBottom: expanded ? 'none' : '1px solid var(--border-color)', fontSize: '0.875rem', cursor: hasReport ? 'pointer' : 'default', background: expanded ? 'var(--bg-color)' : 'transparent' }}
                    onClick={() => hasReport && toggleExpand(log.id)}
                  >
                    <td style={{ padding: '1rem', color: '#94a3b8', textAlign: 'center' }}>
                      {hasReport ? (expanded ? '▼' : '▶') : ''}
                    </td>
                    <td style={{ padding: '1rem', color: '#64748b' }}>#{log.id}</td>
                    <td style={{ padding: '1rem', fontWeight: 500 }}>
                      {log.repo_url ? (
                        <a
                          href={sshToHttps(log.repo_url)}
                          target="_blank"
                          rel="noreferrer"
                          style={{ color: 'var(--primary-color)', textDecoration: 'none' }}
                          onMouseEnter={e => e.currentTarget.style.textDecoration = 'underline'}
                          onMouseLeave={e => e.currentTarget.style.textDecoration = 'none'}
                          onClick={e => e.stopPropagation()}
                        >
                          {log.repo_name || `Repo ${log.repo_id}`}
                        </a>
                      ) : (
                        log.repo_name || `Repo ${log.repo_id}`
                      )}
                    </td>
                    <td style={{ padding: '1rem' }}>
                      <span style={{ display: 'inline-block', padding: '0.15rem 0.5rem', borderRadius: '4px', background: 'rgba(37, 99, 235, 0.08)', color: 'var(--primary-color)', fontSize: '0.75rem', fontWeight: 500 }}>
                        {log.task_type_name || '-'}
                      </span>
                    </td>
                    <td style={{ padding: '1rem' }}>
                      <span style={{ textTransform: 'capitalize', display: 'inline-block', padding: '0.1rem 0.4rem', borderRadius: '4px', background: 'var(--bg-color)', border: '1px solid var(--border-color)', fontSize: '0.75rem' }}>
                        {log.trigger_type}
                      </span>
                    </td>
                    <td style={{ padding: '1rem' }}>
                      <span style={{ display: 'inline-block', padding: '0.1rem 0.4rem', borderRadius: '4px', background: log.engine_mode === 'chunked' ? 'rgba(168, 85, 247, 0.08)' : 'var(--bg-color)', border: '1px solid ' + (log.engine_mode === 'chunked' ? 'rgba(168, 85, 247, 0.2)' : 'var(--border-color)'), fontSize: '0.75rem', color: log.engine_mode === 'chunked' ? '#7c3aed' : '#64748b' }}>
                        {log.engine_mode === 'chunked' ? '分片模式' : '单次模式'}
                      </span>
                    </td>
                    <td style={{ padding: '1rem', color: '#64748b' }}>{formatDate(log.start_time)}</td>
                    <td style={{ padding: '1rem', color: '#64748b' }}>{calcDuration(log.start_time, log.end_time)}</td>
                    <td style={{ padding: '1rem' }}>
                      <div style={{ display: 'flex', flexDirection: 'column', gap: '0.25rem' }}>
                        {statusBadge(log)}
                        {log.status === 'failed' && log.error_message && (
                          <span style={{ fontSize: '0.75rem', color: 'var(--danger-color)', maxWidth: '200px', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }} title={log.error_message}>
                            {log.error_message}
                          </span>
                        )}
                      </div>
                    </td>
                    <td style={{ padding: '1rem' }}>
                      {canCancel && (
                        <button
                          className="btn"
                          onClick={e => { e.stopPropagation(); deletePending(log.id, isRunning); }}
                          style={{
                            background: 'transparent',
                            color: 'var(--danger-color)',
                            border: '1px solid var(--danger-color)',
                            padding: '0.35rem',
                            display: 'inline-flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                            borderRadius: '4px',
                            cursor: 'pointer',
                            transition: 'all 0.2s ease',
                          }}
                          title={isRunning ? "强杀并删除该运行中的任务" : "删除该排队任务"}
                        >
                          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                            <polyline points="3 6 5 6 21 6"></polyline>
                            <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
                          </svg>
                        </button>
                      )}
                    </td>
                  </tr>

                  {expanded && hasReport && (
                    <tr style={{ borderBottom: '1px solid var(--border-color)' }}>
                      <td colSpan={10} style={{ padding: '1.25rem 1.5rem 1.5rem 3.5rem', background: 'var(--bg-color)' }}>
                        <div style={{ display: 'flex', gap: '1.5rem', flexWrap: 'wrap' }}>
                          
                          {/* Left Panel: Score, Summary & Buttons */}
                          <div style={{ flex: '1.2', minWidth: '320px', display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
                            {/* Score */}
                            {report.status === 'success' && (
                              <div style={{ display: 'flex', gap: '1rem', flexWrap: 'wrap' }}>
                                <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', background: '#fff', border: '1px solid #47556922', borderRadius: '8px', padding: '0.5rem 1rem', minWidth: '80px', boxShadow: '0 1px 2px rgba(0,0,0,0.02)' }}>
                                  <span style={{ fontSize: '1.25rem', fontWeight: 700, color: report.score >= 20 ? '#ef4444' : report.score >= 10 ? '#f59e0b' : '#22c55e' }}>{report.score ?? 0}</span>
                                  <span style={{ fontSize: '0.7rem', color: '#64748b', marginTop: '0.1rem' }}>风险评分</span>
                                </div>
                              </div>
                            )}

                            {/* AI Summary */}
                            {report.ai_summary && (
                              <div style={{ padding: '0.75rem 1rem', background: '#f0f9ff', borderRadius: '6px', border: '1px solid #bae6fd', color: '#0369a1', fontSize: '0.875rem', lineHeight: 1.6 }}>
                                <div style={{ fontWeight: 600, marginBottom: '0.2rem', fontSize: '0.8rem' }}>🤖 AI 审计摘要</div>
                                {report.ai_summary}
                              </div>
                            )}

                            {/* Action buttons */}
                            {report.status === 'success' && (
                              <div style={{ display: 'flex', gap: '0.5rem', marginTop: '0.25rem' }}>
                                <button
                                  className="btn"
                                  onClick={e => { e.stopPropagation(); handleNotify(report.id); }}
                                  style={{ background: 'transparent', color: 'var(--primary-color)', border: '1px solid var(--primary-color)', fontSize: '0.85rem' }}
                                >
                                  通知责任人
                                </button>
                                <button
                                  className="btn"
                                  onClick={e => { e.stopPropagation(); handleOpenReport(report.id); }}
                                  style={{ background: 'var(--success-color)', borderColor: 'var(--success-color)', fontSize: '0.85rem' }}
                                >
                                  查看报告
                                </button>
                              </div>
                            )}
                          </div>

                          {/* Right Panel: Diagnostics Snapshot */}
                          <div style={{ flex: '1', minWidth: '300px', background: 'var(--card-bg)', border: '1px solid var(--border-color)', borderRadius: '8px', padding: '1rem', display: 'flex', flexDirection: 'column', gap: '0.5rem', boxShadow: '0 1px 3px rgba(0,0,0,0.02)' }}>
                            <h4 style={{ margin: 0, fontSize: '0.85rem', color: 'var(--text-color)', display: 'flex', alignItems: 'center', gap: '0.35rem', borderBottom: '1px solid var(--border-color)', paddingBottom: '0.5rem', fontWeight: 600 }}>
                              🔬 运行轨迹与诊断快照
                            </h4>
                            {loadingSummaries[report.id] ? (
                              <div style={{ padding: '1.5rem', textAlign: 'center', color: '#64748b', fontSize: '0.8rem' }}>
                                <span className="report-sidebar-spinner" style={{ display: 'inline-block', width: '10px', height: '10px', border: '2px solid rgba(100,116,139,0.3)', borderRadius: '50%', borderTopColor: 'var(--primary-color)', animation: 'report-sidebar-spin 1s infinite', verticalAlign: 'middle', marginRight: '5px' }} />
                                正在获取运行轨迹...
                              </div>
                            ) : summaries[report.id] ? (() => {
                              const s = summaries[report.id];
                              const chunks = s.analysis?.chunks || [];
                              const failedChunk = chunks.find((c: any) => c.status === 'failed');
                              return (
                                <div style={{ fontSize: '0.8rem', display: 'flex', flexDirection: 'column', gap: '0.5rem', color: '#475569' }}>
                                  <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                                    <span>⏱️ 静态分析耗时:</span>
                                    <strong style={{ color: '#0f172a' }}>{formatDuration(s.analysis?.duration_seconds)}</strong>
                                  </div>
                                  <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                                    <span>🎨 综合报告耗时:</span>
                                    <strong style={{ color: '#0f172a' }}>{formatDuration(s.synthesis?.duration_seconds)}</strong>
                                  </div>
                                  {chunks.length > 0 && (
                                    <div>
                                      <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: '0.35rem' }}>
                                        <span>🧩 分片扫描进度:</span>
                                        <strong>{s.analysis?.success_chunks} / {s.analysis?.total_chunks} 成功</strong>
                                      </div>
                                      
                                      {/* Mini Chunk Color Grid */}
                                      <div style={{ display: 'flex', flexWrap: 'wrap', gap: '4px', background: '#f8fafc', padding: '0.5rem', borderRadius: '6px', border: '1px solid #e2e8f0' }}>
                                        {chunks.map((c: any, idx: number) => {
                                          const isChunkFailed = c.status === 'failed';
                                          return (
                                            <div
                                              key={c.chunk_name || idx}
                                              style={{
                                                width: '14px',
                                                height: '14px',
                                                borderRadius: '3px',
                                                background: isChunkFailed ? '#ef4444' : '#10b981',
                                                border: `1px solid ${isChunkFailed ? '#dc2626' : '#059669'}`,
                                                cursor: 'pointer',
                                                transition: 'transform 0.15s ease',
                                              }}
                                              title={`${c.chunk_name} (耗时: ${formatDuration(c.duration_seconds)}, 状态: ${c.status === 'success' ? '成功' : '失败'})`}
                                              onMouseEnter={e => e.currentTarget.style.transform = 'scale(1.2)'}
                                              onMouseLeave={e => e.currentTarget.style.transform = 'scale(1)'}
                                            />
                                          );
                                        })}
                                      </div>
                                    </div>
                                  )}

                                  {/* Error diagnosis box */}
                                  {failedChunk && (
                                    <div style={{ marginTop: '0.25rem', background: '#fee2e2', color: '#991b1b', border: '1px solid #fca5a5', padding: '0.6rem 0.75rem', borderRadius: '6px', fontSize: '0.75rem', lineHeight: '1.4' }}>
                                      <div style={{ fontWeight: 600, display: 'flex', alignItems: 'center', gap: '0.25rem', marginBottom: '0.2rem' }}>
                                        <span>🚨 故障分片:</span>
                                        <span style={{ fontFamily: 'monospace' }}>{failedChunk.chunk_name}</span>
                                      </div>
                                      <div style={{ fontFamily: 'monospace', wordBreak: 'break-all', maxHeight: '60px', overflowY: 'auto', background: 'rgba(255,255,255,0.4)', padding: '0.3rem', borderRadius: '4px' }}>
                                        {failedChunk.error_message}
                                      </div>
                                    </div>
                                  )}
                                </div>
                              );
                            })() : (log.engine_mode === 'chunked' && (isRunning || isPending)) ? (
                              <div style={{ fontSize: '0.8rem', display: 'flex', flexDirection: 'column', gap: '0.75rem', color: '#475569', padding: '0.5rem 0' }}>
                                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                                  <span style={{ fontWeight: 500, color: '#475569', display: 'flex', alignItems: 'center', gap: '0.25rem' }}>
                                    🧩 分片扫描进度
                                  </span>
                                  {report.total_chunks > 0 ? (
                                    <strong style={{ color: 'var(--primary-color)', fontSize: '0.875rem' }}>
                                      {report.processed_chunks} / {report.total_chunks} 已处理
                                    </strong>
                                  ) : (
                                    <span style={{ color: '#94a3b8', fontStyle: 'italic' }}>准备中...</span>
                                  )}
                                </div>

                                <div style={{ width: '100%', height: '8px', background: '#f1f5f9', borderRadius: '999px', overflow: 'hidden', border: '1px solid #e2e8f0' }}>
                                  <div style={{
                                    width: report.total_chunks > 0 ? `${Math.min(100, (report.processed_chunks / report.total_chunks) * 100)}%` : '0%',
                                    height: '100%',
                                    background: 'linear-gradient(90deg, var(--primary-color) 0%, #60a5fa 100%)',
                                    borderRadius: '999px',
                                    transition: 'width 0.8s cubic-bezier(0.4, 0, 0.2, 1)',
                                  }} />
                                </div>

                                <div style={{ display: 'flex', justifyContent: 'space-between', color: '#64748b', fontSize: '0.75rem', marginTop: '0.25rem' }}>
                                  <span>
                                    {report.total_chunks > 0
                                      ? "正在执行分片并发扫描..."
                                      : "正在初始化代码仓或分析范围..."}
                                  </span>
                                  <span>
                                    {report.total_chunks > 0 && `${Math.round((report.processed_chunks / report.total_chunks) * 100)}%`}
                                  </span>
                                </div>
                              </div>
                            ) : (
                              <div style={{ color: '#94a3b8', fontSize: '0.75rem', fontStyle: 'italic', textAlign: 'center', padding: '1rem 0' }}>
                                无分片诊断数据 (单次任务模式)
                              </div>
                            )}
                          </div>

                        </div>
                      </td>
                    </tr>
                  )}
                </React.Fragment>
              );
            })}
          </tbody>
        </table>
      </div>

      {/* Pagination Controls */}
      {logs.length > 0 && (
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginTop: '1rem', padding: '0.5rem 1rem', background: 'var(--card-bg)', border: '1px solid var(--border-color)', borderRadius: '6px' }}>
          <div style={{ color: '#64748b', fontSize: '0.875rem' }}>
            共 {totalItems} 条执行记录，当前第 {page} / {totalPages} 页
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <button
              disabled={page === 1}
              onClick={() => setPage(prev => Math.max(prev - 1, 1))}
              style={{
                padding: '0.3rem 0.6rem', border: '1px solid var(--border-color)', background: 'transparent',
                borderRadius: '4px', cursor: page === 1 ? 'not-allowed' : 'pointer',
                color: page === 1 ? 'var(--text-secondary)' : 'var(--text-color)', fontSize: '0.825rem',
                transition: 'all 0.2s'
              }}
              onMouseEnter={e => { if (page !== 1) e.currentTarget.style.background = 'rgba(0,0,0,0.02)'; }}
              onMouseLeave={e => { e.currentTarget.style.background = 'transparent'; }}
            >
              上一页
            </button>
            
            {/* Page numbers */}
            {Array.from({ length: Math.min(5, totalPages) }, (_, i) => {
              let pageNum = page;
              if (page <= 3) pageNum = i + 1;
              else if (page >= totalPages - 2) pageNum = totalPages - 4 + i;
              else pageNum = page - 2 + i;

              // Guard pageNum bounds
              if (pageNum < 1 || pageNum > totalPages) return null;

              return (
                <button
                  key={pageNum}
                  onClick={() => setPage(pageNum)}
                  style={{
                    minWidth: '28px', height: '28px', padding: '0 0.3rem',
                    border: '1px solid',
                    borderColor: page === pageNum ? 'var(--primary-color)' : 'var(--border-color)',
                    background: page === pageNum ? 'var(--primary-color)' : 'transparent',
                    color: page === pageNum ? 'white' : 'var(--text-color)',
                    borderRadius: '4px', cursor: 'pointer', fontSize: '0.825rem', fontWeight: page === pageNum ? 600 : 400,
                    transition: 'all 0.2s'
                  }}
                  onMouseEnter={e => { if (page !== pageNum) e.currentTarget.style.background = 'rgba(37,99,235,0.04)'; }}
                  onMouseLeave={e => { if (page !== pageNum) e.currentTarget.style.background = 'transparent'; }}
                >
                  {pageNum}
                </button>
              );
            })}

            <button
              disabled={page === totalPages}
              onClick={() => setPage(prev => Math.min(prev + 1, totalPages))}
              style={{
                padding: '0.3rem 0.6rem', border: '1px solid var(--border-color)', background: 'transparent',
                borderRadius: '4px', cursor: page === totalPages ? 'not-allowed' : 'pointer',
                color: page === totalPages ? 'var(--text-secondary)' : 'var(--text-color)', fontSize: '0.825rem',
                transition: 'all 0.2s'
              }}
              onMouseEnter={e => { if (page !== totalPages) e.currentTarget.style.background = 'rgba(0,0,0,0.02)'; }}
              onMouseLeave={e => { e.currentTarget.style.background = 'transparent'; }}
            >
              下一页
            </button>

            <select
              value={pageSize}
              onChange={e => {
                setPageSize(Number(e.target.value));
                setPage(1);
              }}
              style={{
                padding: '0.25rem 0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)',
                fontSize: '0.825rem', outline: 'none', background: 'transparent', color: 'var(--text-color)', marginLeft: '0.5rem',
                cursor: 'pointer'
              }}
            >
              <option value="15">15 条/页</option>
              <option value="30">30 条/页</option>
              <option value="50">50 条/页</option>
              <option value="100">100 条/页</option>
            </select>
          </div>
        </div>
      )}

      <ReportSidebar open={sidebarOpen} onClose={() => setSidebarOpen(false)} markdown={currentMarkdown} loading={loadingMarkdown} reportId={currentReportId} />
    </div>
  );
}

export default ExecutionLogs;
