import React, { useEffect, useState } from 'react';
import { useToast } from '../components/Toast';

function ExecutionLogs() {
  const [logs, setLogs] = useState<any[]>([]);
  const { showToast } = useToast();

  const fetchLogs = async () => {
    try {
      const res = await fetch('/api/executions');
      if (res.ok) {
        setLogs(await res.json());
      }
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
    } catch (err) {
      showToast('请求失败，请检查网络连接', 'error');
    }
  };

  useEffect(() => {
    fetchLogs();
    const interval = setInterval(fetchLogs, 5000); // Poll every 5 seconds
    return () => clearInterval(interval);
  }, []);

  const formatDate = (dateStr: string) => {
    if (!dateStr) return '-';
    const d = new Date(dateStr);
    return isNaN(d.getTime()) ? '-' : d.toLocaleString();
  };

  const calculateDuration = (startStr: string, endStr: string) => {
    if (!startStr || !endStr) return '-';
    const start = new Date(startStr);
    const end = new Date(endStr);
    if (isNaN(start.getTime()) || isNaN(end.getTime())) return '-';
    
    const diff = Math.floor((end.getTime() - start.getTime()) / 1000); // in seconds
    if (diff < 60) return `${diff}s`;
    return `${Math.floor(diff / 60)}m ${diff % 60}s`;
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
                <td colSpan={7} style={{ padding: '3rem 1rem', textAlign: 'center', color: '#64748b' }}>暂无任何任务执行记录。</td>
              </tr>
            ) : (
              logs.map(log => (
                <tr key={log.id} style={{ borderBottom: '1px solid var(--border-color)', fontSize: '0.875rem' }}>
                  <td style={{ padding: '1rem', color: '#64748b' }}>#{log.id}</td>
                  <td style={{ padding: '1rem', fontWeight: 500 }}>{log.repo?.name || `Repo ${log.repo_id}`}</td>
                  <td style={{ padding: '1rem' }}>
                    <span style={{ textTransform: 'capitalize', display: 'inline-block', padding: '0.1rem 0.4rem', borderRadius: '4px', background: 'var(--bg-color)', border: '1px solid var(--border-color)', fontSize: '0.75rem' }}>
                      {log.trigger_type}
                    </span>
                  </td>
                  <td style={{ padding: '1rem' }}>{log.schedule ? log.schedule.name : '-'}</td>
                  <td style={{ padding: '1rem', color: '#64748b' }}>{formatDate(log.start_time)}</td>
                  <td style={{ padding: '1rem', color: '#64748b' }}>{calculateDuration(log.start_time, log.end_time)}</td>
                  <td style={{ padding: '1rem' }}>
                    <div style={{ display: 'flex', flexDirection: 'column', gap: '0.25rem' }}>
                      <span className={`badge ${log.status === 'success' ? 'success' : (log.status === 'failed' ? 'danger' : (log.status === 'running' ? 'primary' : 'warning'))}`} style={{ alignSelf: 'flex-start' }}>
                        {log.status === 'success' ? '执行成功' : log.status === 'failed' ? '执行失败' : log.status === 'running' ? '运行中...' : '排队中'}
                      </span>
                      {log.status === 'failed' && log.error_message && (
                        <span style={{ fontSize: '0.75rem', color: 'var(--danger-color)', maxWidth: '200px', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }} title={log.error_message}>
                          {log.error_message}
                        </span>
                      )}
                    </div>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}

export default ExecutionLogs;

