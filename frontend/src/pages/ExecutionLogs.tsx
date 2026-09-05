import React, { useEffect, useState, useCallback } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Pagination, usePagination } from '@code/common';
import { useToast } from '../components/Toast';
import ReportViewer from '../components/report/ReportViewer';
import { ThrottleControlCard } from '../components/execution/ThrottleControlCard';
import { ExecutionLogItem } from '../components/execution/ExecutionLogItem';
import { apiUrl } from '../config';

interface ExecutionLogsProps {
  embedded?: boolean;
}

function ExecutionLogs({ embedded = false }: ExecutionLogsProps) {
  const [searchParams] = useSearchParams();
  const { page, pageSize, updateParams } = usePagination({ defaultPageSize: 25 });

  const statusGroup = searchParams.get('status_group') || searchParams.get('statusGroup') || 'all';

  const [logs, setLogs] = useState<any[]>([]);
  const [expandedIds, setExpandedIds] = useState<Set<number>>(new Set());
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [currentReportId, setCurrentReportId] = useState<number | undefined>(undefined);
  const { showToast } = useToast();

  const [totalItems, setTotalItems] = useState<number>(0);

  // AI 并发流控状态
  const [isAdmin, setIsAdmin] = useState(false);
  const [sysConfig, setSysConfig] = useState<any>(null);
  const [applyingConfig, setApplyingConfig] = useState(false);
  const [togglingQueue, setTogglingQueue] = useState(false);

  // 批量选中与诊断状态
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set());
  const [summaries, setSummaries] = useState<Record<number, any>>({});
  const [loadingSummaries, setLoadingSummaries] = useState<Record<number, boolean>>({});

  const fetchConfig = useCallback(async () => {
    try {
      const res = await fetch('/api/config');
      if (res.ok) {
        const data = await res.json();
        setSysConfig(data);
      }
    } catch (err) {
      console.error('Failed to fetch config:', err);
    }
  }, []);

  const handleApplyConfig = async (scale: number, duration: number) => {
    setApplyingConfig(true);
    try {
      const res = await fetch('/api/config', {
        method: 'PATCH',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          concurrency_scale: scale,
          duration_hours: duration,
        }),
      });
      if (res.ok) {
        const data = await res.json();
        setSysConfig(data);
        showToast('AI 并发限流调节应用成功', 'success');
      } else {
        const err = await res.json();
        showToast(err.error || '调节失败，请重试', 'error');
      }
    } catch {
      showToast('网络请求异常，调节失败', 'error');
    } finally {
      setApplyingConfig(false);
    }
  };

  const handleToggleQueue = async (paused: boolean) => {
    setTogglingQueue(true);
    try {
      const res = await fetch('/api/config', {
        method: 'PATCH',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          queue_paused: paused,
        }),
      });
      if (res.ok) {
        const data = await res.json();
        setSysConfig(data);
        showToast(paused ? '任务队列已暂停派发 (排空模式)' : '任务队列已恢复正常派发', 'success');
      } else {
        const err = await res.json();
        showToast(err.error || '操作失败，请重试', 'error');
      }
    } catch {
      showToast('网络请求异常，操作失败', 'error');
    } finally {
      setTogglingQueue(false);
    }
  };

  const fetchLogs = useCallback(async () => {
    try {
      const params = new URLSearchParams({
        page: page.toString(),
        pageSize: pageSize.toString(),
      });
      if (statusGroup !== 'all') {
        params.append('status_group', statusGroup);
      }
      const res = await fetch(`/api/executions?${params.toString()}`);
      if (res.ok) {
        const data = await res.json();
        setLogs(data.items || []);
        setTotalItems(data.total || 0);
        const newTotalPages = data.totalPages || 0;
        if (newTotalPages > 0 && page > newTotalPages) {
          updateParams({ page: newTotalPages });
        }
      }
    } catch (err) {
      console.error('Failed to fetch execution logs:', err);
    }
  }, [page, pageSize, statusGroup, updateParams]);

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

  const handleOpenReport = (reportId: number) => {
    setCurrentReportId(reportId);
    setSidebarOpen(true);
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
        setLogs(prev => prev.filter(l => l.id !== logId));
      } else {
        showToast(data.error || '删除失败', 'error');
      }
    } catch {
      showToast('网络异常，删除失败', 'error');
    }
  };

  const toggleSelect = (id: number) => {
    setSelectedIds(prev => {
      const next = new Set(prev);
      next.has(id) ? next.delete(id) : next.add(id);
      return next;
    });
  };

  // 获取当前页所有可删除（非终态）的日志 ID
  const selectableIds = logs.filter(l => !['success', 'failed', 'skipped'].includes(l.status)).map(l => l.id);

  const toggleSelectAll = () => {
    if (selectableIds.every(id => selectedIds.has(id))) {
      setSelectedIds(prev => {
        const next = new Set(prev);
        selectableIds.forEach(id => next.delete(id));
        return next;
      });
    } else {
      setSelectedIds(prev => {
        const next = new Set(prev);
        selectableIds.forEach(id => next.add(id));
        return next;
      });
    }
  };

  const batchDelete = async () => {
    const ids = Array.from(selectedIds);
    if (ids.length === 0) return;
    const hasRunning = ids.some(id => {
      const log = logs.find(l => l.id === id);
      return log && ['running', 'cloning', 'pre_processing', 'analyzing', 'post_processing', 'merging'].includes(log.status);
    });
    const message = hasRunning
      ? `确认批量删除 ${ids.length} 条任务？其中包含运行中的任务，将被强制终止。\n警告：此操作不可恢复。`
      : `确认批量删除 ${ids.length} 条排队任务？此操作不可恢复。`;
    if (!window.confirm(message)) return;
    try {
      const res = await fetch('/api/executions/batch', {
        method: 'DELETE',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ids }),
      });
      const data = await res.json();
      if (res.ok) {
        showToast(data.message || `已删除 ${data.deleted} 条`, 'success');
        setLogs(prev => prev.filter(l => !selectedIds.has(l.id)));
        setSelectedIds(new Set());
      } else {
        showToast(data.error || '批量删除失败', 'error');
      }
    } catch {
      showToast('网络异常，批量删除失败', 'error');
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

  const fetchSummary = async (reportId: number) => {
    setLoadingSummaries(prev => ({ ...prev, [reportId]: true }));
    try {
      const res = await fetch(apiUrl(`/api/tasks/${reportId}/report/diagnostics`));
      if (res.ok) {
        const data = await res.json();
        setSummaries(prev => ({ ...prev, [reportId]: data }));
      }
    } catch (err) {
      console.error('Failed to fetch diagnostics summary:', err);
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
    fetch('/api/me')
      .then(res => (res.ok ? res.json() : null))
      .then(data => {
        if (data) {
          const isShieldAdmin = Array.isArray(data.roles) && (data.roles.includes('super_admin') || data.roles.includes('shield_admin'));
          setIsAdmin(isShieldAdmin);
        }
      })
      .catch(() => {});
    fetchConfig();
  }, [fetchConfig]);

  // 自适应轮询：检测到存在活跃任务时 4 秒快速感应，全部完成时回退至 15 秒心跳
  const hasActiveTasks = logs.some(l =>
    ['running', 'cloning', 'pre_processing', 'analyzing', 'synthesis', 'post_processing', 'merging'].includes(l.status) ||
    (l.task_report && ['cloning', 'pre_processing', 'analyzing', 'synthesis', 'post_processing', 'merging'].includes(l.task_report.status))
  );

  useEffect(() => {
    fetchLogs();
  }, [fetchLogs]);

  useEffect(() => {
    const delay = hasActiveTasks ? 4000 : 15000;
    const interval = setInterval(fetchLogs, delay);
    return () => clearInterval(interval);
  }, [fetchLogs, hasActiveTasks]);

  useEffect(() => {
    const configInterval = setInterval(fetchConfig, 15000);
    return () => clearInterval(configInterval);
  }, [fetchConfig]);

  return (
    <div>
      {/* AI 并发流控与排空模式卡片组件 */}
      <ThrottleControlCard
        sysConfig={sysConfig}
        isAdmin={isAdmin}
        onApplyScale={handleApplyConfig}
        onToggleQueue={handleToggleQueue}
        applyingConfig={applyingConfig}
        togglingQueue={togglingQueue}
      />

      {/* 顶部标题与操作栏 */}
      <div style={{ display: 'flex', justifyContent: embedded ? 'flex-end' : 'space-between', alignItems: 'center', marginBottom: '1.5rem' }}>
        {!embedded && <h2 style={{ margin: 0, color: 'var(--color-text-primary, #0f172a)' }}>执行日志</h2>}
        <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.35rem', marginRight: '0.5rem' }}>
            <label style={{ fontSize: '0.875rem', fontWeight: 600, color: 'var(--color-text-secondary, #64748b)', marginRight: '0.25rem', userSelect: 'none', whiteSpace: 'nowrap' }}>
              状态
            </label>
            <select
              value={statusGroup}
              onChange={e => updateParams({ status_group: e.target.value, page: 1 })}
              style={{
                padding: '0.35rem 0.5rem',
                borderRadius: '6px',
                border: '1px solid var(--color-border-primary, #e2e8f0)',
                outline: 'none',
                fontSize: '0.875rem',
                background: 'var(--color-bg-input, #ffffff)',
                color: 'var(--color-text-primary, #0f172a)',
                cursor: 'pointer',
                height: '32px',
              }}
            >
              <option value="all">全部</option>
              <option value="running">执行中</option>
              <option value="pending">排队中</option>
              <option value="completed">已完成</option>
            </select>
          </div>
          <button
            className="btn"
            onClick={fetchLogs}
            style={{
              background: 'transparent',
              color: 'var(--color-text-primary, #0f172a)',
              border: '1px solid var(--color-border-primary, #e2e8f0)',
            }}
          >
            刷新列表
          </button>
          {selectedIds.size > 0 && (
            <button
              className="btn"
              onClick={batchDelete}
              style={{
                background: 'var(--color-danger, #ef4444)',
                color: '#ffffff',
                border: '1px solid var(--color-danger, #ef4444)',
              }}
            >
              批量删除 ({selectedIds.size})
            </button>
          )}
          <button
            className="btn"
            onClick={clearCompleted}
            style={{
              background: 'transparent',
              color: 'var(--color-danger, #ef4444)',
              border: '1px solid var(--color-danger, #ef4444)',
            }}
          >
            清除已完成
          </button>
        </div>
      </div>

      {/* 日志表格 */}
      <div className="card" style={{ padding: '0', overflowX: 'auto', background: 'var(--color-bg-surface, #ffffff)', border: '1px solid var(--color-border-primary, #e2e8f0)' }}>
        <table style={{ width: '100%', minWidth: '1100px', borderCollapse: 'collapse' }}>
          <thead>
            <tr style={{ borderBottom: '1px solid var(--color-border-primary, #e2e8f0)', color: 'var(--color-text-secondary, #64748b)', fontSize: '0.875rem', textAlign: 'left', background: 'var(--color-bg-muted, #f8fafc)' }}>
              <th style={{ padding: '1rem', fontWeight: 600, width: '2rem' }}>
                {selectableIds.length > 0 && (
                  <input
                    type="checkbox"
                    checked={selectableIds.length > 0 && selectableIds.every(id => selectedIds.has(id))}
                    onChange={toggleSelectAll}
                    style={{ cursor: 'pointer', accentColor: 'var(--color-primary, #2563eb)' }}
                    title="全选/取消全选"
                  />
                )}
              </th>
              <th style={{ padding: '1rem', fontWeight: 600, width: '2rem' }}></th>
              <th style={{ padding: '1rem', fontWeight: 600, width: '80px' }}>任务 ID</th>
              <th style={{ padding: '1rem', fontWeight: 600 }}>所属代码仓</th>
              <th style={{ padding: '1rem', fontWeight: 600, width: '120px' }}>任务类型</th>
              <th style={{ padding: '1rem', fontWeight: 600, width: '100px' }}>触发方式</th>
              <th style={{ padding: '1rem', fontWeight: 600, width: '100px' }}>执行引擎</th>
              <th style={{ padding: '1rem', fontWeight: 600, width: '160px' }}>开始时间</th>
              <th style={{ padding: '1rem', fontWeight: 600, width: '100px' }}>执行耗时</th>
              <th style={{ padding: '1rem', fontWeight: 600, width: '150px' }}>状态</th>
              <th style={{ padding: '1rem', fontWeight: 600, width: '100px' }}>操作</th>
            </tr>
          </thead>
          <tbody>
            {logs.length === 0 ? (
              <tr>
                <td colSpan={11} style={{ padding: '3rem 1rem', textAlign: 'center', color: 'var(--color-text-muted, #64748b)' }}>
                  暂无任何任务执行记录。
                </td>
              </tr>
            ) : (
              logs.map(log => {
                const expanded = expandedIds.has(log.id);
                const isRunning = ['running', 'cloning', 'pre_processing', 'analyzing', 'post_processing', 'merging'].includes(log.status);
                const isPending = log.status === 'pending' || log.status === 'queued';
                const canCancel = isRunning || isPending;
                const reportId = log.task_report?.id;

                return (
                  <ExecutionLogItem
                    key={log.id}
                    log={log}
                    expanded={expanded}
                    selected={selectedIds.has(log.id)}
                    canCancel={canCancel}
                    summary={reportId ? summaries[reportId] : null}
                    loadingSummary={reportId ? !!loadingSummaries[reportId] : false}
                    onToggleExpand={toggleExpand}
                    onToggleSelect={toggleSelect}
                    onDeletePending={deletePending}
                    onNotify={handleNotify}
                    onOpenReport={handleOpenReport}
                  />
                );
              })
            )}
          </tbody>
        </table>
      </div>

      {/* 分页控制栏 */}
      {totalItems > 0 && <Pagination totalItems={totalItems} />}

      {/* 报告查看抽屉 */}
      <ReportViewer
        taskId={currentReportId}
        open={sidebarOpen}
        onClose={() => setSidebarOpen(false)}
        mode="drawer"
      />
    </div>
  );
}

export default ExecutionLogs;
