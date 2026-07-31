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
          <span className="px-2.5 py-1 text-xs font-medium rounded-full bg-blue-50 text-blue-700 border border-blue-200 inline-flex items-center gap-1">
            <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" />
            </svg>
            手动单仓触发
          </span>
        );
      case 'manual_batch':
        return (
          <span className="px-2.5 py-1 text-xs font-medium rounded-full bg-purple-50 text-purple-700 border border-purple-200 inline-flex items-center gap-1">
            <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10" />
            </svg>
            批量快速补扫
          </span>
        );
      case 'cron_auto':
        return (
          <span className="px-2.5 py-1 text-xs font-medium rounded-full bg-emerald-50 text-emerald-700 border border-emerald-200 inline-flex items-center gap-1">
            <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            定时自动触发
          </span>
        );
      case 'cron_manual':
        return (
          <span className="px-2.5 py-1 text-xs font-medium rounded-full bg-amber-50 text-amber-700 border border-amber-200 inline-flex items-center gap-1">
            <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z" />
            </svg>
            定时策略手动触发
          </span>
        );
      default:
        return <span className="px-2.5 py-1 text-xs font-medium rounded-full bg-gray-100 text-gray-700">{type}</span>;
    }
  };

  const renderExecStatusBadge = (status: string) => {
    switch (status) {
      case 'success':
        return <span className="px-2 py-0.5 text-xs font-medium rounded bg-green-100 text-green-800">成功</span>;
      case 'failed':
        return <span className="px-2 py-0.5 text-xs font-medium rounded bg-red-100 text-red-800">失败</span>;
      case 'synthesis':
        return <span className="px-2 py-0.5 text-xs font-medium rounded bg-purple-100 text-purple-800 animate-pulse">报告总结中</span>;
      case 'analyzing':
      case 'cloning':
      case 'pre_processing':
      case 'post_processing':
      case 'merging':
      case 'running':
        return <span className="px-2 py-0.5 text-xs font-medium rounded bg-blue-100 text-blue-800 animate-pulse">进行中</span>;
      case 'pending':
      case 'queued':
        return <span className="px-2 py-0.5 text-xs font-medium rounded bg-yellow-100 text-yellow-800">排队中</span>;
      case 'skipped':
        return <span className="px-2 py-0.5 text-xs font-medium rounded bg-gray-100 text-gray-600">跳过</span>;
      default:
        return <span className="px-2 py-0.5 text-xs font-medium rounded bg-gray-100 text-gray-700">{status}</span>;
    }
  };

  const totalPages = Math.ceil(total / pageSize) || 1;

  return (
    <div className="space-y-6">
      {/* 顶部统计卡片 */}
      {stats && (
        <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
          <div className="bg-white p-5 rounded-xl border border-gray-200 shadow-sm flex items-center justify-between">
            <div>
              <p className="text-xs font-semibold text-gray-500 uppercase tracking-wider">今日触发批次</p>
              <h4 className="text-2xl font-extrabold text-gray-900 mt-1">{stats.today_batches}</h4>
              <p className="text-xs text-gray-400 mt-0.5">历史总计 {stats.total_batches} 次下发</p>
            </div>
            <div className="w-12 h-12 bg-blue-50 text-blue-600 rounded-xl flex items-center justify-center">
              <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M13 10V3L4 14h7v7l9-11h-7z" />
              </svg>
            </div>
          </div>

          <div className="bg-white p-5 rounded-xl border border-gray-200 shadow-sm flex items-center justify-between">
            <div>
              <p className="text-xs font-semibold text-gray-500 uppercase tracking-wider">手动操作 (单仓/批量)</p>
              <h4 className="text-2xl font-extrabold text-purple-700 mt-1">{stats.manual_count}</h4>
              <p className="text-xs text-gray-400 mt-0.5">人类主动交互下发</p>
            </div>
            <div className="w-12 h-12 bg-purple-50 text-purple-600 rounded-xl flex items-center justify-center">
              <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" />
              </svg>
            </div>
          </div>

          <div className="bg-white p-5 rounded-xl border border-gray-200 shadow-sm flex items-center justify-between">
            <div>
              <p className="text-xs font-semibold text-gray-500 uppercase tracking-wider">定时任务触发</p>
              <h4 className="text-2xl font-extrabold text-emerald-700 mt-1">{stats.cron_count}</h4>
              <p className="text-xs text-gray-400 mt-0.5">自动 Cron 策略下发</p>
            </div>
            <div className="w-12 h-12 bg-emerald-50 text-emerald-600 rounded-xl flex items-center justify-center">
              <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
            </div>
          </div>

          <div className="bg-white p-5 rounded-xl border border-gray-200 shadow-sm flex items-center justify-between">
            <div>
              <p className="text-xs font-semibold text-gray-500 uppercase tracking-wider">触发累积覆盖仓库</p>
              <h4 className="text-2xl font-extrabold text-amber-600 mt-1">{stats.total_repos_scanned}</h4>
              <p className="text-xs text-gray-400 mt-0.5">累积扫描任务项</p>
            </div>
            <div className="w-12 h-12 bg-amber-50 text-amber-600 rounded-xl flex items-center justify-center">
              <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2" />
              </svg>
            </div>
          </div>
        </div>
      )}

      {/* 过滤工具栏 */}
      <div className="bg-white p-4 rounded-xl border border-gray-200 shadow-sm space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div className="flex flex-wrap items-center gap-3">
            {/* 触发方式 */}
            <select
              value={triggerType}
              onChange={(e) => { setTriggerType(e.target.value); setPage(1); }}
              className="px-3 py-2 text-sm border border-gray-300 rounded-lg bg-white focus:outline-none focus:ring-2 focus:ring-blue-500"
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
              className="px-3 py-2 text-sm border border-gray-300 rounded-lg bg-white focus:outline-none focus:ring-2 focus:ring-blue-500"
            >
              <option value="">全部任务类型</option>
              {taskTypes.map((tt) => (
                <option key={tt.id} value={tt.id}>{tt.display_name}</option>
              ))}
            </select>

            {/* 搜索框 */}
            <div className="relative">
              <input
                type="text"
                placeholder="搜索批次号 / 操作人 / 摘要..."
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && fetchLogs()}
                className="w-64 px-3 py-2 text-sm border border-gray-300 rounded-lg pl-9 focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
              <svg className="w-4 h-4 text-gray-400 absolute left-3 top-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
              </svg>
            </div>

            <button
              onClick={() => { setTriggerType(''); setSelectedTaskTypeId(''); setSearch(''); setPage(1); }}
              className="px-3 py-2 text-sm border border-gray-300 rounded-lg hover:bg-gray-50 text-gray-600 font-medium transition"
            >
              重置筛选
            </button>
          </div>

          <button
            onClick={fetchLogs}
            className="px-4 py-2 text-sm font-medium text-white bg-blue-600 hover:bg-blue-700 rounded-lg transition shadow-sm flex items-center gap-1.5"
          >
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
            </svg>
            刷新数据
          </button>
        </div>
      </div>

      {/* 审计日志主表格 */}
      <div className="bg-white rounded-xl border border-gray-200 shadow-sm overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-left border-collapse">
            <thead>
              <tr className="bg-gray-50/80 border-b border-gray-200 text-xs font-semibold text-gray-500 uppercase tracking-wider">
                <th className="py-3.5 px-4">触发批次 & 时间</th>
                <th className="py-3.5 px-4">触发方式</th>
                <th className="py-3.5 px-4">操作人 / 来源</th>
                <th className="py-3.5 px-4">任务类型</th>
                <th className="py-3.5 px-4">目标摘要</th>
                <th className="py-3.5 px-4 text-center">覆盖仓库数</th>
                <th className="py-3.5 px-4 text-center">下发情况</th>
                <th className="py-3.5 px-4 text-right">明细操作</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-200 text-sm">
              {loading ? (
                <tr>
                  <td colSpan={8} className="text-center py-12 text-gray-400">
                    <div className="inline-flex items-center gap-2">
                      <svg className="w-5 h-5 animate-spin text-blue-600" fill="none" viewBox="0 0 24 24">
                        <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                        <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
                      </svg>
                      正在加载审计日志...
                    </div>
                  </td>
                </tr>
              ) : logs.length === 0 ? (
                <tr>
                  <td colSpan={8} className="text-center py-12 text-gray-400">
                    暂无符合要求的任务触发审计日志
                  </td>
                </tr>
              ) : (
                logs.map((item) => (
                  <tr key={item.id} className="hover:bg-gray-50/80 transition-colors">
                    <td className="py-3.5 px-4">
                      <div className="font-mono font-semibold text-gray-900 text-xs">{item.trigger_batch}</div>
                      <div className="text-xs text-gray-400 mt-0.5">
                        {new Date(item.created_at).toLocaleString('zh-CN')}
                      </div>
                    </td>
                    <td className="py-3.5 px-4">
                      {renderTriggerTypeBadge(item.trigger_type)}
                    </td>
                    <td className="py-3.5 px-4">
                      <div className="font-medium text-gray-800 flex items-center gap-1.5">
                        {item.trigger_type.startsWith('cron_auto') ? (
                          <span className="w-6 h-6 rounded-full bg-emerald-100 text-emerald-700 flex items-center justify-center text-xs">🤖</span>
                        ) : (
                          <span className="w-6 h-6 rounded-full bg-blue-100 text-blue-700 flex items-center justify-center text-xs font-bold">
                            {item.operator_name ? item.operator_name[0].toUpperCase() : 'U'}
                          </span>
                        )}
                        <span>{item.operator_name || '未知操作人'}</span>
                      </div>
                      {item.client_ip && (
                        <div className="text-xs text-gray-400 font-mono mt-0.5">IP: {item.client_ip}</div>
                      )}
                    </td>
                    <td className="py-3.5 px-4">
                      <span className="font-medium text-gray-700">
                        {item.task_type?.display_name || `ID: ${item.task_type_id}`}
                      </span>
                    </td>
                    <td className="py-3.5 px-4 max-w-xs truncate" title={item.target_summary}>
                      <span className="text-gray-600">{item.target_summary}</span>
                    </td>
                    <td className="py-3.5 px-4 text-center">
                      <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-semibold bg-gray-100 text-gray-800">
                        {item.total_repos} 个仓库
                      </span>
                    </td>
                    <td className="py-3.5 px-4 text-center">
                      <div className="inline-flex items-center gap-1.5 text-xs">
                        <span className="text-emerald-600 font-medium">成功 {item.success_count}</span>
                        {item.skip_count > 0 && (
                          <span className="text-gray-400">| 跳过 {item.skip_count}</span>
                        )}
                      </div>
                    </td>
                    <td className="py-3.5 px-4 text-right">
                      <button
                        onClick={() => handleOpenDetail(item)}
                        className="px-3 py-1.5 text-xs font-medium text-blue-600 bg-blue-50 hover:bg-blue-100 rounded-lg transition border border-blue-200"
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

        {/* 分页控制 */}
        {total > 0 && (
          <div className="px-4 py-3 border-t border-gray-200 bg-gray-50 flex items-center justify-between">
            <span className="text-xs text-gray-500">
              显示第 {(page - 1) * pageSize + 1} 至 {Math.min(page * pageSize, total)} 条，共 {total} 条
            </span>
            <div className="flex items-center gap-2">
              <button
                disabled={page <= 1}
                onClick={() => setPage(page - 1)}
                className="px-3 py-1 text-xs border border-gray-300 rounded-md disabled:opacity-50 disabled:cursor-not-allowed hover:bg-white bg-gray-50 text-gray-700"
              >
                上一页
              </button>
              <span className="text-xs font-medium text-gray-700 px-2">{page} / {totalPages}</span>
              <button
                disabled={page >= totalPages}
                onClick={() => setPage(page + 1)}
                className="px-3 py-1 text-xs border border-gray-300 rounded-md disabled:opacity-50 disabled:cursor-not-allowed hover:bg-white bg-gray-50 text-gray-700"
              >
                下一页
              </button>
            </div>
          </div>
        )}
      </div>

      {/* 批次明细抽屉 Modal / Drawer */}
      {drawerOpen && selectedLog && (
        <div className="fixed inset-0 z-50 overflow-hidden bg-black/40 backdrop-blur-sm flex justify-end">
          <div className="w-full max-w-3xl bg-white h-full shadow-2xl flex flex-col transform transition-transform animate-slide-in">
            {/* Header */}
            <div className="px-6 py-4 border-b border-gray-200 flex items-center justify-between bg-gray-50">
              <div>
                <div className="flex items-center gap-2">
                  <h3 className="text-lg font-bold text-gray-900">触发批次明细与受影响代码仓</h3>
                  {renderTriggerTypeBadge(selectedLog.trigger_type)}
                </div>
                <p className="text-xs text-gray-500 mt-1 font-mono">
                  批次号: {selectedLog.trigger_batch} | 时间: {new Date(selectedLog.created_at).toLocaleString('zh-CN')}
                </p>
              </div>
              <button
                onClick={() => setDrawerOpen(false)}
                className="text-gray-400 hover:text-gray-600 p-2 rounded-lg hover:bg-gray-200/60 transition"
              >
                <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M6 18L18 6M6 6l12 12" />
                </svg>
              </button>
            </div>

            {/* Info Summary */}
            <div className="p-6 bg-blue-50/50 border-b border-blue-100 grid grid-cols-2 gap-4 text-sm">
              <div>
                <span className="text-xs text-gray-500">操作人 / 客户端 IP:</span>
                <p className="font-semibold text-gray-800">{selectedLog.operator_name} ({selectedLog.client_ip || '内部/System'})</p>
              </div>
              <div>
                <span className="text-xs text-gray-500">任务类型:</span>
                <p className="font-semibold text-gray-800">{selectedLog.task_type?.display_name}</p>
              </div>
              <div className="col-span-2">
                <span className="text-xs text-gray-500">触发目标与筛选摘要:</span>
                <p className="font-medium text-gray-700 mt-0.5">{selectedLog.target_summary}</p>
              </div>
            </div>

            {/* Sub-tasks list */}
            <div className="flex-1 overflow-y-auto p-6 space-y-4">
              <h4 className="text-sm font-bold text-gray-900 flex items-center justify-between">
                <span>下发的仓库任务列表 ({detailExecLogs.length} 个)</span>
                <span className="text-xs font-normal text-gray-500">
                  成功: {selectedLog.success_count} / 跳过: {selectedLog.skip_count}
                </span>
              </h4>

              {loadingDetail ? (
                <div className="text-center py-12 text-gray-400">正在获取关联仓库任务...</div>
              ) : detailExecLogs.length === 0 ? (
                <div className="text-center py-12 text-gray-400 border border-dashed rounded-lg">
                  未查到由该批次直接创建的仓库子任务日志（可能任务被直接跳过或被用户手工清除）
                </div>
              ) : (
                <div className="space-y-3">
                  {detailExecLogs.map((logItem) => (
                    <div key={logItem.id} className="p-4 rounded-xl border border-gray-200 hover:border-blue-300 transition bg-white shadow-sm flex items-center justify-between">
                      <div className="space-y-1">
                        <div className="flex items-center gap-2">
                          <span className="font-bold text-gray-900">{logItem.repo?.name || `Repo #${logItem.repo_id}`}</span>
                          {logItem.repo?.branch && (
                            <span className="text-xs font-mono bg-gray-100 text-gray-600 px-2 py-0.5 rounded">
                              {logItem.repo.branch}
                            </span>
                          )}
                          {renderExecStatusBadge(logItem.status)}
                        </div>
                        {logItem.repo?.url && (
                          <p className="text-xs text-gray-400 font-mono truncate max-w-md">{logItem.repo.url}</p>
                        )}
                        {logItem.error_message && (
                          <p className="text-xs text-red-600 bg-red-50 p-1.5 rounded mt-1">{logItem.error_message}</p>
                        )}
                      </div>

                      <div className="text-right flex items-center gap-3">
                        {logItem.task_report && (
                          <div className="text-center px-3 py-1 bg-gray-50 rounded-lg">
                            <div className="text-xs text-gray-400">评分</div>
                            <div className="text-sm font-bold text-blue-600">{logItem.task_report.score}</div>
                          </div>
                        )}
                        {logItem.task_report_id && (
                          <a
                            href={`/reports?report_id=${logItem.task_report_id}`}
                            target="_blank"
                            rel="noreferrer"
                            className="px-3 py-1.5 text-xs font-medium text-blue-600 border border-blue-200 rounded-lg hover:bg-blue-50 transition"
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
    </div>
  );
}

export default AuditLogs;
