import React, { useEffect, useState } from 'react';
import { useToast } from '../components/Toast';
import ReportSidebar from '../components/ReportSidebar';

function ExecutionLogs() {
  const [logs, setLogs] = useState<any[]>([]);
  const [expandedIds, setExpandedIds] = useState<Set<number>>(new Set());
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [currentMarkdown, setCurrentMarkdown] = useState('');
  const [loadingMarkdown, setLoadingMarkdown] = useState(false);
  const { showToast } = useToast();

  const fetchLogs = async () => {
    try {
      const res = await fetch('/api/executions');
      if (res.ok) setLogs(await res.json());
    } catch (err) {
      console.error('Failed to fetch execution logs:', err);
    }
  };

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

  const deletePending = async (logId: number) => {
    if (!window.confirm('确认删除该排队中的任务？此操作不可恢复。')) return;
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

  const toggleExpand = (id: number) => {
    setExpandedIds(prev => {
      const next = new Set(prev);
      next.has(id) ? next.delete(id) : next.add(id);
      return next;
    });
  };

  useEffect(() => {
    fetchLogs();
    const interval = setInterval(fetchLogs, 5000);
    return () => clearInterval(interval);
  }, []);

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

  const statusBadge = (status: string) => {
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
    return <span className={`badge ${s.cls}`}>{s.label}</span>;
  };

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.5rem' }}>
        <h2 style={{ margin: 0 }}>任务执行历史</h2>
        <div style={{ display: 'flex', gap: '0.5rem' }}>
          <button className="btn" onClick={fetchLogs} style={{ background: 'transparent', color: 'var(--text-color)', border: '1px solid var(--border-color)' }}>
            刷新列表
          </button>
          <button className="btn" onClick={clearCompleted} style={{ background: 'transparent', color: 'var(--danger-color)', border: '1px solid var(--danger-color)' }}>
            清除已完成
          </button>
        </div>
      </div>

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
                    <td style={{ padding: '1rem', fontWeight: 500 }}>{log.repo_name || `Repo ${log.repo_id}`}</td>
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
                        {statusBadge(log.status)}
                        {log.status === 'failed' && log.error_message && (
                          <span style={{ fontSize: '0.75rem', color: 'var(--danger-color)', maxWidth: '200px', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }} title={log.error_message}>
                            {log.error_message}
                          </span>
                        )}
                      </div>
                    </td>
                    <td style={{ padding: '1rem' }}>
                      {log.status === 'pending' && (
                        <button
                          className="btn"
                          onClick={e => { e.stopPropagation(); deletePending(log.id); }}
                          style={{ background: 'transparent', color: 'var(--danger-color)', border: '1px solid var(--danger-color)', fontSize: '0.8rem', padding: '0.25rem 0.65rem' }}
                          title="删除该排队任务"
                        >
                          删除
                        </button>
                      )}
                    </td>
                  </tr>

                  {expanded && hasReport && (
                    <tr style={{ borderBottom: '1px solid var(--border-color)' }}>
                      <td colSpan={10} style={{ padding: '0 1rem 1.25rem 3.5rem', background: 'var(--bg-color)' }}>
                        {/* Score */}
                        {report.status === 'success' && (
                          <div style={{ display: 'flex', gap: '1rem', marginBottom: '0.75rem', flexWrap: 'wrap' }}>
                            <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', background: '#fff', border: '1px solid #47556922', borderRadius: '8px', padding: '0.5rem 1rem', minWidth: '80px' }}>
                              <span style={{ fontSize: '1.25rem', fontWeight: 700, color: report.score >= 20 ? '#ef4444' : report.score >= 10 ? '#f59e0b' : '#22c55e' }}>{report.score ?? 0}</span>
                              <span style={{ fontSize: '0.7rem', color: '#64748b', marginTop: '0.1rem' }}>综合评分</span>
                            </div>
                          </div>
                        )}

                        {/* AI Summary */}
                        {report.ai_summary && (
                          <div style={{ padding: '0.75rem 1rem', background: '#f0f9ff', borderRadius: '6px', border: '1px solid #bae6fd', color: '#0369a1', fontSize: '0.875rem', marginBottom: '0.75rem', lineHeight: 1.6 }}>
                            {report.ai_summary}
                          </div>
                        )}

                        {/* Action buttons */}
                        {report.status === 'success' && (
                          <div style={{ display: 'flex', gap: '0.5rem' }}>
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
                      </td>
                    </tr>
                  )}
                </React.Fragment>
              );
            })}
          </tbody>
        </table>
      </div>

      <ReportSidebar open={sidebarOpen} onClose={() => setSidebarOpen(false)} markdown={currentMarkdown} loading={loadingMarkdown} />
    </div>
  );
}

export default ExecutionLogs;
