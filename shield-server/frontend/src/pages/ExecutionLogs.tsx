import React, { useEffect, useState } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { useToast } from '../components/Toast';

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
      const res = await fetch(`/api/reviews/${reportId}/report`);
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

  const handleNotify = async (reportId: number) => {
    try {
      const res = await fetch(`/api/reviews/${reportId}/notify`, { method: 'POST' });
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
    };
    const s = map[status] || { cls: 'warning', label: status };
    return <span className={`badge ${s.cls}`}>{s.label}</span>;
  };

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.5rem' }}>
        <h2 style={{ margin: 0 }}>检视任务执行历史</h2>
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
              <th style={{ padding: '1rem', fontWeight: 600 }}>触发方式</th>
              <th style={{ padding: '1rem', fontWeight: 600 }}>关联调度策略</th>
              <th style={{ padding: '1rem', fontWeight: 600 }}>开始时间</th>
              <th style={{ padding: '1rem', fontWeight: 600 }}>执行耗时</th>
              <th style={{ padding: '1rem', fontWeight: 600 }}>状态</th>
            </tr>
          </thead>
          <tbody>
            {logs.length === 0 ? (
              <tr>
                <td colSpan={8} style={{ padding: '3rem 1rem', textAlign: 'center', color: '#64748b' }}>暂无任何任务执行记录。</td>
              </tr>
            ) : logs.map(log => {
              const expanded = expandedIds.has(log.id);
              const report = log.review_report;
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
                    <td style={{ padding: '1rem', fontWeight: 500 }}>{log.repo?.name || `Repo ${log.repo_id}`}</td>
                    <td style={{ padding: '1rem' }}>
                      <span style={{ textTransform: 'capitalize', display: 'inline-block', padding: '0.1rem 0.4rem', borderRadius: '4px', background: 'var(--bg-color)', border: '1px solid var(--border-color)', fontSize: '0.75rem' }}>
                        {log.trigger_type}
                      </span>
                    </td>
                    <td style={{ padding: '1rem' }}>{log.schedule ? log.schedule.name : '-'}</td>
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
                  </tr>

                  {expanded && hasReport && (
                    <tr style={{ borderBottom: '1px solid var(--border-color)' }}>
                      <td colSpan={8} style={{ padding: '0 1rem 1.25rem 3.5rem', background: 'var(--bg-color)' }}>
                        {/* Issue counts */}
                        {report.status === 'success' && (
                          <div style={{ display: 'flex', gap: '1rem', marginBottom: '0.75rem', flexWrap: 'wrap' }}>
                            {[
                              { label: '阻塞+严重', value: report.critical_issues, color: '#dc2626' },
                              { label: '主要',     value: report.major_issues,    color: '#d97706' },
                              { label: '提示+建议', value: report.minor_issues,    color: '#2563eb' },
                              { label: '合计',     value: report.issue_count,     color: '#475569' },
                            ].map(item => (
                              <div key={item.label} style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', background: '#fff', border: `1px solid ${item.color}22`, borderRadius: '8px', padding: '0.5rem 1rem', minWidth: '80px' }}>
                                <span style={{ fontSize: '1.25rem', fontWeight: 700, color: item.color }}>{item.value ?? 0}</span>
                                <span style={{ fontSize: '0.7rem', color: '#64748b', marginTop: '0.1rem' }}>{item.label}</span>
                              </div>
                            ))}
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

      {/* Markdown Sidebar */}
      <div style={{ position: 'fixed', top: 0, right: sidebarOpen ? 0 : '-50vw', width: '50vw', height: '100vh', background: 'var(--bg-color)', boxShadow: '-4px 0 15px rgba(0,0,0,0.1)', transition: 'right 0.3s ease-in-out', zIndex: 1000, display: 'flex', flexDirection: 'column' }}>
        <div style={{ padding: '1.5rem', borderBottom: '1px solid var(--border-color)', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <h3 style={{ margin: 0 }}>定向代码巡检报告</h3>
          <button onClick={() => setSidebarOpen(false)} style={{ background: 'transparent', border: 'none', cursor: 'pointer', opacity: 0.6, fontSize: '1.5rem', color: 'var(--text-color)' }}>&times;</button>
        </div>
        <div style={{ padding: '2rem', overflowY: 'auto', flex: 1, backgroundColor: '#ffffff' }}>
          {loadingMarkdown ? (
            <div style={{ textAlign: 'center', marginTop: '3rem', color: '#64748b' }}>
              <span className="spinner"></span> 正在渲染 Markdown...
            </div>
          ) : (
            <div className="markdown-body">
              <ReactMarkdown remarkPlugins={[remarkGfm]}>{currentMarkdown || '*暂无任何报告信息*'}</ReactMarkdown>
            </div>
          )}
        </div>
      </div>
      {sidebarOpen && <div onClick={() => setSidebarOpen(false)} style={{ position: 'fixed', top: 0, left: 0, right: 0, bottom: 0, background: 'rgba(0,0,0,0.5)', zIndex: 999 }} />}

      <style>{`
        .spinner { display:inline-block; width:12px; height:12px; border:2px solid rgba(100,116,139,0.3); border-radius:50%; border-top-color:var(--primary-color); animation:spin 1s ease-in-out infinite; vertical-align:middle; margin-right:5px; }
        @keyframes spin { to { transform: rotate(360deg); } }
        .markdown-body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif; line-height:1.6; color:#24292f; }
        .markdown-body h1,.markdown-body h2,.markdown-body h3,.markdown-body h4 { margin-top:24px; margin-bottom:16px; font-weight:600; line-height:1.25; }
        .markdown-body h2 { border-bottom:1px solid #d0d7de; padding-bottom:.3em; }
        .markdown-body blockquote { padding:0 1em; color:#57606a; border-left:.25em solid #d0d7de; margin:0 0 16px 0; }
        .markdown-body pre { padding:16px; overflow:auto; font-size:85%; line-height:1.45; background-color:#f6f8fa; border-radius:6px; }
        .markdown-body code { padding:.2em .4em; font-size:85%; background-color:rgba(175,184,193,0.2); border-radius:6px; }
        .markdown-body pre>code { padding:0; font-size:100%; background-color:transparent; border:0; }
        .markdown-body ul,.markdown-body ol { margin-top:0; margin-bottom:16px; padding-left:2em; }
      `}</style>
    </div>
  );
}

export default ExecutionLogs;
