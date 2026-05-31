import React, { useState, useEffect } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import ScheduleSidebar, { ScheduleFormData } from '../components/ScheduleSidebar';
import { useToast } from '../components/Toast';
import { appNavigatePath } from '../config';
import ReportSidebar from '../components/ReportSidebar';

type ScanTab = 'trigger' | 'schedules';

function ScanManagement() {
  const { showToast } = useToast();
  const [activeTab, setActiveTab] = useState<ScanTab>('trigger');

  // --- Manual Trigger State ---
  const [repos, setRepos] = useState<any[]>([]);
  const [teams, setTeams] = useState<any[]>([]);
  const [taskTypes, setTaskTypes] = useState<any[]>([]);
  const [selectedRepoIds, setSelectedRepoIds] = useState<number[]>([]);
  const [openBatchMenu, setOpenBatchMenu] = useState(false);
  const [filterTeam, setFilterTeam] = useState('');
  const [filterSearch, setFilterSearch] = useState('');
  const [recentLogs, setRecentLogs] = useState<any[]>([]);
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [currentMarkdown, setCurrentMarkdown] = useState('');
  const [loadingMarkdown, setLoadingMarkdown] = useState(false);
  const [currentReportId, setCurrentReportId] = useState<number | undefined>(undefined);
  const [currentPage, setCurrentPage] = useState(1);
  const [pageSize, setPageSize] = useState(15);

  // --- Schedule State ---
  const [schedules, setSchedules] = useState<any[]>([]);
  const [isSidebarOpen, setIsSidebarOpen] = useState(false);
  const [editingSchedule, setEditingSchedule] = useState<any>(null);

  // --- Invalid Reports ---
  const [invalidReportCount, setInvalidReportCount] = useState<number | null>(null);

  const fetchRepos = async () => {
    try {
      const res = await fetch('/api/repos?pageSize=10000');
      if (res.ok) {
        const data = await res.json();
        setRepos(data.items || data || []);
      }
    } catch (err) { console.error(err); }
  };

  const fetchTeams = async () => {
    try {
      const res = await fetch('/api/teams');
      if (res.ok) setTeams(await res.json());
    } catch (err) { console.error(err); }
  };

  const fetchTaskTypes = async () => {
    try {
      const res = await fetch('/api/task-types?active_only=true');
      if (res.ok) setTaskTypes(await res.json());
    } catch (err) { console.error(err); }
  };

  const fetchSchedules = async () => {
    try {
      const res = await fetch('/api/schedules');
      if (res.ok) setSchedules(await res.json());
    } catch (err) { console.error(err); }
  };

  const fetchRecentLogs = async () => {
    try {
      const res = await fetch('/api/executions');
      if (res.ok) {
        const data = await res.json();
        setRecentLogs(data);
      }
    } catch (err) {
      console.error('Failed to fetch recent execution logs:', err);
    }
  };

  useEffect(() => {
    fetchTeams();
    fetchTaskTypes();
    if (activeTab === 'trigger') fetchRepos();
    if (activeTab === 'schedules') { fetchSchedules(); fetchRepos(); }
  }, [activeTab]);

  useEffect(() => {
    if (activeTab === 'trigger') {
      fetchRecentLogs();
      const interval = setInterval(fetchRecentLogs, 5000);
      return () => clearInterval(interval);
    }
  }, [activeTab]);

  // Filter repos
  const filteredRepos = repos.filter(r => {
    if (filterTeam && String(r.team_id) !== filterTeam) return false;
    if (filterSearch && !r.name.toLowerCase().includes(filterSearch.toLowerCase())) return false;
    return true;
  });

  useEffect(() => {
    setCurrentPage(1);
  }, [filterTeam, filterSearch]);

  const totalPages = Math.ceil(filteredRepos.length / pageSize) || 1;
  const activePage = currentPage > totalPages ? totalPages : currentPage;
  const paginatedRepos = filteredRepos.slice((activePage - 1) * pageSize, activePage * pageSize);

  const handleBatchTrigger = async (taskTypeId: number) => {
    setOpenBatchMenu(false);
    const count = selectedRepoIds.length;
    if (count === 0) return;

    showToast(`正在下发 ${count} 个代码仓的任务...`, 'info');
    let successCount = 0;
    let failCount = 0;

    const promises = selectedRepoIds.map(repoId =>
      fetch('/api/tasks/trigger', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ repo_id: repoId, task_type_id: taskTypeId })
      }).then(res => { if (res.ok) successCount++; else failCount++; })
        .catch(() => { failCount++; })
    );

    await Promise.all(promises);

    if (failCount === 0) {
      showToast(`成功下发全部 ${successCount} 个任务！`, 'success');
    } else if (successCount === 0) {
      showToast(`触发任务失败，共 ${failCount} 个失败`, 'error');
    } else {
      showToast(`部分下发成功：${successCount} 成功，${failCount} 失败`, 'info');
    }
    setSelectedRepoIds([]);
    fetchRecentLogs();
  };

  const handleClearInvalidReports = async () => {
    if (!window.confirm('确认清除所有不是"完成"状态的无效报告记录吗？进行中的任务可能会受影响。')) return;
    try {
      const res = await fetch('/api/tasks/invalid-reports', { method: 'DELETE' });
      if (res.ok) {
        const data = await res.json();
        showToast(`成功清除 ${data.deleted} 条无效报告记录`, 'success');
      } else {
        const err = await res.json();
        showToast(`清除失败: ${err.error || '未知错误'}`, 'error');
      }
    } catch (err) {
      showToast('请求失败，请检查网络连接', 'error');
    }
  };

  // --- Schedule handlers ---
  const handleSaveSchedule = async (form: ScheduleFormData) => {
    try {
      const url = editingSchedule ? `/api/schedules/${editingSchedule.id}` : '/api/schedules';
      const method = editingSchedule ? 'PUT' : 'POST';
      const res = await fetch(url, {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(form)
      });
      if (res.ok) {
        setIsSidebarOpen(false);
        setEditingSchedule(null);
        fetchSchedules();
      } else {
        const d = await res.json();
        showToast((editingSchedule ? '更新' : '新建') + '调度失败: ' + d.error, 'error');
      }
    } catch (err) { console.error(err); }
  };

  const handleDeleteSchedule = async (id: number) => {
    if (!window.confirm('确认删除该定时策略吗？')) return;
    try {
      const res = await fetch(`/api/schedules/${id}`, { method: 'DELETE' });
      if (res.ok) fetchSchedules();
    } catch (err) { console.error(err); }
  };

  const toggleScheduleStatus = async (sched: any) => {
    try {
      const res = await fetch(`/api/schedules/${sched.id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ...sched, is_active: !sched.is_active })
      });
      if (res.ok) fetchSchedules();
    } catch (err) { console.error(err); }
  };

  const handleTriggerSchedule = async (id: number) => {
    try {
      showToast('正在触发任务，请耐心等待...', 'info');
      const res = await fetch(`/api/schedules/${id}/trigger`, { method: 'POST' });
      if (res.ok) {
        showToast('任务已成功触发', 'success');
      } else {
        const d = await res.json();
        showToast('触发失败: ' + d.error, 'error');
      }
    } catch (err) { console.error(err); }
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
        fetchRecentLogs();
      } else {
        showToast(data.error || '删除失败', 'error');
      }
    } catch {
      showToast('网络异常，删除失败', 'error');
    }
  };

  const getRepoStatus = (repoId: number) => {
    const latestLog = recentLogs.find(log => log.repo_id === repoId);
    if (!latestLog) return { status: 'none', text: '未分析', badgeCls: 'info', isRunning: false, isPending: false, log: null };

    const isRunning = ['running', 'cloning', 'pre_processing', 'analyzing', 'post_processing'].includes(latestLog.status);
    const isPending = latestLog.status === 'pending' || latestLog.status === 'queued';

    let text = latestLog.status;
    let badgeCls = 'info';

    switch (latestLog.status) {
      case 'success':
        text = '完成';
        badgeCls = 'success';
        break;
      case 'failed':
        text = '失败';
        badgeCls = 'danger';
        break;
      case 'skipped':
        text = '已跳过';
        badgeCls = 'info';
        break;
      case 'pending':
      case 'queued':
        text = '排队中';
        badgeCls = 'warning';
        break;
      case 'cloning':
        text = '克隆中';
        badgeCls = 'primary';
        break;
      case 'pre_processing':
        text = '前置检查';
        badgeCls = 'primary';
        break;
      case 'analyzing':
        const report = latestLog.task_report;
        if (report && report.total_chunks > 1) {
          text = `分析中 (${report.processed_chunks}/${report.total_chunks})`;
        } else {
          text = '分析中';
        }
        badgeCls = 'primary';
        break;
      case 'post_processing':
        text = '结果分析';
        badgeCls = 'primary';
        break;
    }

    return { status: latestLog.status, text, badgeCls, isRunning, isPending, log: latestLog };
  };

  const tabStyle = (t: ScanTab) => ({
    background: 'transparent',
    border: 'none',
    padding: '0.75rem 0',
    fontWeight: 600 as const,
    fontSize: '1rem',
    cursor: 'pointer' as const,
    color: activeTab === t ? 'var(--primary-color)' : 'var(--text-color)',
    borderBottom: activeTab === t ? '2px solid var(--primary-color)' : '2px solid transparent',
    marginBottom: '-1px',
  });

  return (
    <div>
      <div style={{ display: 'flex', gap: '2rem', borderBottom: '1px solid var(--border-color)', marginBottom: '1.5rem' }}>
        <button onClick={() => setActiveTab('trigger')} style={tabStyle('trigger')}>手动触发</button>
        <button onClick={() => setActiveTab('schedules')} style={tabStyle('schedules')}>定时策略</button>
      </div>

      {activeTab === 'trigger' && (
        <div>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.5rem' }}>
            <div style={{ display: 'flex', gap: '1rem', flexWrap: 'wrap' }}>
              <select value={filterTeam} onChange={e => setFilterTeam(e.target.value)} style={{ padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', outline: 'none' }}>
                <option value="">全部部门</option>
                {teams.map(t => <option key={t.id} value={t.id}>{t.name}</option>)}
              </select>
              <input type="text" placeholder="搜索代码仓名称..." value={filterSearch} onChange={e => setFilterSearch(e.target.value)} style={{ padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', outline: 'none', minWidth: '200px' }} />
            </div>
            <div style={{ display: 'flex', gap: '0.75rem', alignItems: 'center', position: 'relative' }}>
              <div style={{ position: 'relative' }}>
                <button
                  className="btn"
                  disabled={selectedRepoIds.length === 0}
                  onClick={() => setOpenBatchMenu(prev => !prev)}
                  style={{
                    padding: '0.4rem 0.8rem', borderRadius: '4px',
                    background: selectedRepoIds.length === 0 ? 'var(--border-color)' : 'var(--primary-color)',
                    color: selectedRepoIds.length === 0 ? '#94a3b8' : 'white',
                    border: 'none', cursor: selectedRepoIds.length === 0 ? 'not-allowed' : 'pointer',
                    fontSize: '0.875rem', display: 'flex', alignItems: 'center', gap: '0.25rem'
                  }}
                >
                  执行 {selectedRepoIds.length > 0 && `(${selectedRepoIds.length})`} <span style={{ fontSize: '0.7rem' }}>▾</span>
                </button>
                {openBatchMenu && selectedRepoIds.length > 0 && (
                  <>
                    <div onClick={() => setOpenBatchMenu(false)} style={{ position: 'fixed', top: 0, left: 0, right: 0, bottom: 0, zIndex: 99 }} />
                    <div style={{ position: 'absolute', right: 0, top: '100%', marginTop: '4px', background: 'white', border: '1px solid var(--border-color)', borderRadius: '6px', boxShadow: '0 4px 12px rgba(0,0,0,0.12)', zIndex: 100, minWidth: '160px', overflow: 'hidden' }}>
                      {taskTypes.map(tt => (
                        <div
                          key={tt.id}
                          onClick={() => handleBatchTrigger(tt.id)}
                          style={{ padding: '0.5rem 0.75rem', cursor: 'pointer', fontSize: '0.825rem', borderBottom: '1px solid #f1f5f9', display: 'flex', alignItems: 'center', gap: '0.5rem', color: 'var(--text-color)' }}
                          onMouseEnter={e => e.currentTarget.style.backgroundColor = '#f8fafc'}
                          onMouseLeave={e => e.currentTarget.style.backgroundColor = 'white'}
                        >
                          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="var(--primary-color)" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><polygon points="5 3 19 12 5 21 5 3"></polygon></svg>
                          {tt.display_name}
                        </div>
                      ))}
                    </div>
                  </>
                )}
              </div>
              <button
                className="btn"
                onClick={handleClearInvalidReports}
                style={{
                  background: 'transparent', color: 'var(--danger-color)',
                  border: '1px solid var(--danger-color)', padding: '0.4rem 0.8rem',
                  borderRadius: '4px', cursor: 'pointer', fontSize: '0.875rem'
                }}
                onMouseEnter={e => e.currentTarget.style.backgroundColor = 'rgba(239, 68, 68, 0.1)'}
                onMouseLeave={e => e.currentTarget.style.backgroundColor = 'transparent'}
              >
                清除无效报告
              </button>
            </div>
          </div>

          <div className="card" style={{ padding: 0, fontSize: '0.875rem' }}>
            <table className="table">
              <thead>
                <tr>
                  <th style={{ width: '40px', textAlign: 'center' }}>
                    <input
                      type="checkbox"
                      checked={filteredRepos.length > 0 && selectedRepoIds.length === filteredRepos.length}
                      ref={input => {
                        if (input) {
                          input.indeterminate = selectedRepoIds.length > 0 && selectedRepoIds.length < filteredRepos.length;
                        }
                      }}
                      onChange={e => {
                        if (e.target.checked) {
                          setSelectedRepoIds(filteredRepos.map(r => r.id));
                        } else {
                          setSelectedRepoIds([]);
                        }
                      }}
                      style={{ cursor: 'pointer' }}
                    />
                  </th>
                  <th>代码仓名称</th>
                  <th>归属部门</th>
                  <th>负责人</th>
                  <th>服务组</th>
                  <th style={{ width: '150px' }}>状态</th>
                  <th style={{ width: '180px', textAlign: 'right' }}>操作</th>
                </tr>
              </thead>
              <tbody>
                {paginatedRepos.length === 0 ? (
                  <tr><td colSpan={7} style={{ textAlign: 'center', padding: '3rem', color: '#94a3b8' }}>暂无代码仓数据</td></tr>
                ) : paginatedRepos.map(r => {
                  const statusInfo = getRepoStatus(r.id);
                  const canCancel = statusInfo.isRunning || statusInfo.isPending;
                  return (
                    <tr key={r.id}>
                      <td style={{ textAlign: 'center' }}>
                        <input
                          type="checkbox"
                          checked={selectedRepoIds.includes(r.id)}
                          onChange={e => {
                            if (e.target.checked) {
                              setSelectedRepoIds(prev => [...prev, r.id]);
                            } else {
                              setSelectedRepoIds(prev => prev.filter(id => id !== r.id));
                            }
                          }}
                          style={{ cursor: 'pointer' }}
                        />
                      </td>
                      <td style={{ fontWeight: 500 }}>{r.name}</td>
                      <td>{r.team?.name || teams.find(t => t.id === r.team_id)?.name || '-'}</td>
                      <td>{r.owner?.name || r.owner_id || '-'}</td>
                      <td style={{ color: '#64748b' }}>{r.service_group || '-'}</td>
                      <td>
                        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                          {statusInfo.status !== 'none' ? (
                            <>
                              <span className={`badge ${statusInfo.badgeCls}`}>{statusInfo.text}</span>
                              {statusInfo.isRunning && <span className="spinner-mini" />}
                            </>
                          ) : (
                            <span style={{ color: '#94a3b8' }}>未分析</span>
                          )}
                        </div>
                      </td>
                      <td style={{ textAlign: 'right' }}>
                        <div style={{ display: 'flex', gap: '0.5rem', justifyContent: 'flex-end', alignItems: 'center' }}>
                          {statusInfo.status === 'success' && statusInfo.log.task_report && (
                            <button
                              onClick={() => handleOpenReport(statusInfo.log.task_report.id)}
                              style={{
                                background: 'transparent',
                                border: '1px solid var(--primary-color)',
                                color: 'var(--primary-color)',
                                padding: '0.2rem 0.5rem',
                                borderRadius: '4px',
                                fontSize: '0.75rem',
                                cursor: 'pointer',
                                transition: 'all 0.2s',
                              }}
                              onMouseEnter={e => {
                                e.currentTarget.style.backgroundColor = 'var(--primary-color)';
                                e.currentTarget.style.color = 'white';
                              }}
                              onMouseLeave={e => {
                                e.currentTarget.style.backgroundColor = 'transparent';
                                e.currentTarget.style.color = 'var(--primary-color)';
                              }}
                            >
                              查看报告
                            </button>
                          )}
                          {canCancel && (
                            <button
                              onClick={() => deletePending(statusInfo.log.id, statusInfo.isRunning)}
                              style={{
                                background: 'transparent',
                                border: '1px solid var(--danger-color)',
                                color: 'var(--danger-color)',
                                padding: '0.2rem 0.4rem',
                                borderRadius: '4px',
                                fontSize: '0.75rem',
                                cursor: 'pointer',
                                display: 'inline-flex',
                                alignItems: 'center',
                                gap: '0.2rem',
                                transition: 'all 0.2s',
                              }}
                              title={statusInfo.isRunning ? "强杀进程并删除该任务" : "取消排队任务"}
                              onMouseEnter={e => {
                                e.currentTarget.style.backgroundColor = 'var(--danger-color)';
                                e.currentTarget.style.color = 'white';
                              }}
                              onMouseLeave={e => {
                                e.currentTarget.style.backgroundColor = 'transparent';
                                e.currentTarget.style.color = 'var(--danger-color)';
                              }}
                            >
                              <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                                <polyline points="3 6 5 6 21 6"></polyline>
                                <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
                              </svg>
                              {statusInfo.isRunning ? "终止" : "取消"}
                            </button>
                          )}
                          {!canCancel && statusInfo.status !== 'success' && (
                            <span style={{ color: '#94a3b8', fontSize: '0.75rem' }}>-</span>
                          )}
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>

          {/* Pagination Controls */}
          {filteredRepos.length > 0 && (
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginTop: '1rem', padding: '0.5rem 1rem', background: 'var(--card-bg)', border: '1px solid var(--border-color)', borderRadius: '6px' }}>
              <div style={{ color: '#64748b', fontSize: '0.875rem' }}>
                共 {filteredRepos.length} 个代码仓
                {selectedRepoIds.length > 0 && (
                  <span style={{ marginLeft: '1rem', color: 'var(--primary-color)', fontWeight: 500 }}>
                    已选择 {selectedRepoIds.length} 个
                  </span>
                )}
              </div>
              <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                <button
                  disabled={activePage === 1}
                  onClick={() => setCurrentPage(prev => Math.max(prev - 1, 1))}
                  style={{
                    padding: '0.3rem 0.6rem', border: '1px solid var(--border-color)', background: 'transparent',
                    borderRadius: '4px', cursor: activePage === 1 ? 'not-allowed' : 'pointer',
                    color: activePage === 1 ? '#cbd5e1' : 'var(--text-color)', fontSize: '0.825rem',
                    transition: 'all 0.2s'
                  }}
                  onMouseEnter={e => { if (activePage !== 1) e.currentTarget.style.background = 'rgba(0,0,0,0.02)'; }}
                  onMouseLeave={e => { e.currentTarget.style.background = 'transparent'; }}
                >
                  上一页
                </button>
                
                {/* Page numbers */}
                {Array.from({ length: Math.min(5, totalPages) }, (_, i) => {
                  let pageNum = activePage;
                  if (activePage <= 3) pageNum = i + 1;
                  else if (activePage >= totalPages - 2) pageNum = totalPages - 4 + i;
                  else pageNum = activePage - 2 + i;

                  // Guard pageNum bounds
                  if (pageNum < 1 || pageNum > totalPages) return null;

                  return (
                    <button
                      key={pageNum}
                      onClick={() => setCurrentPage(pageNum)}
                      style={{
                        minWidth: '28px', height: '28px', padding: '0 0.3rem',
                        border: '1px solid',
                        borderColor: activePage === pageNum ? 'var(--primary-color)' : 'var(--border-color)',
                        background: activePage === pageNum ? 'var(--primary-color)' : 'transparent',
                        color: activePage === pageNum ? 'white' : 'var(--text-color)',
                        borderRadius: '4px', cursor: 'pointer', fontSize: '0.825rem', fontWeight: activePage === pageNum ? 600 : 400,
                        transition: 'all 0.2s'
                      }}
                      onMouseEnter={e => { if (activePage !== pageNum) e.currentTarget.style.background = 'rgba(37,99,235,0.04)'; }}
                      onMouseLeave={e => { if (activePage !== pageNum) e.currentTarget.style.background = 'transparent'; }}
                    >
                      {pageNum}
                    </button>
                  );
                })}

                <button
                  disabled={activePage === totalPages}
                  onClick={() => setCurrentPage(prev => Math.min(prev + 1, totalPages))}
                  style={{
                    padding: '0.3rem 0.6rem', border: '1px solid var(--border-color)', background: 'transparent',
                    borderRadius: '4px', cursor: activePage === totalPages ? 'not-allowed' : 'pointer',
                    color: activePage === totalPages ? '#cbd5e1' : 'var(--text-color)', fontSize: '0.825rem',
                    transition: 'all 0.2s'
                  }}
                  onMouseEnter={e => { if (activePage !== totalPages) e.currentTarget.style.background = 'rgba(0,0,0,0.02)'; }}
                  onMouseLeave={e => { e.currentTarget.style.background = 'transparent'; }}
                >
                  下一页
                </button>

                <select
                  value={pageSize}
                  onChange={e => {
                    setPageSize(Number(e.target.value));
                    setCurrentPage(1);
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

        </div>
      )}

      {activeTab === 'schedules' && (
        <div>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.5rem' }}>
            <h3 style={{ margin: 0 }}>定时任务策略配置</h3>
            <button className="btn" onClick={() => { setEditingSchedule(null); setIsSidebarOpen(true); }}>
              + 新增定时策略
            </button>
          </div>

          <div className="card" style={{ padding: 0 }}>
            <table style={{ width: '100%', borderCollapse: 'collapse' }}>
              <thead>
                <tr style={{ borderBottom: '1px solid var(--border-color)', color: '#64748b', fontSize: '0.875rem', textAlign: 'left' }}>
                  <th style={{ padding: '1rem' }}>策略名称</th>
                  <th style={{ padding: '1rem' }}>任务类型</th>
                  <th style={{ padding: '1rem' }}>Cron 表达式</th>
                  <th style={{ padding: '1rem' }}>目标代码仓</th>
                  <th style={{ padding: '1rem' }}>状态</th>
                  <th style={{ padding: '1rem', textAlign: 'right' }}>操作</th>
                </tr>
              </thead>
              <tbody>
                {schedules.length === 0 ? (
                  <tr>
                    <td colSpan={6} style={{ padding: '2rem 0', textAlign: 'center', color: '#64748b' }}>暂无可用的定时任务策略，点击右上方新增。</td>
                  </tr>
                ) : (
                  schedules.map(sched => (
                    <tr key={sched.id} style={{ borderBottom: '1px solid var(--border-color)' }}>
                      <td style={{ padding: '1rem', fontWeight: 500 }}>{sched.name}</td>
                      <td style={{ padding: '1rem' }}>
                        <span style={{ display: 'inline-block', padding: '0.15rem 0.5rem', borderRadius: '4px', background: 'rgba(37, 99, 235, 0.08)', color: 'var(--primary-color)', fontSize: '0.75rem', fontWeight: 500 }}>
                          {sched.task_type?.display_name || '-'}
                        </span>
                      </td>
                      <td style={{ padding: '1rem', fontFamily: 'monospace' }}>{sched.cron_expr}</td>
                      <td style={{ padding: '1rem' }}>
                        <span style={{ background: 'var(--bg-color)', padding: '0.25rem 0.6rem', borderRadius: '4px', fontSize: '0.75rem', border: '1px solid var(--border-color)', textTransform: 'capitalize' as const }}>
                          {sched.target_mode === 'all' ? '所有代码仓' : sched.target_mode === 'service_group' ? '按服务组' : sched.target_mode === 'team' ? '按团队' : '指定代码仓'}
                        </span>
                      </td>
                      <td style={{ padding: '1rem' }}>
                        <div
                          style={{ display: 'inline-flex', alignItems: 'center', gap: '0.5rem', cursor: 'pointer' }}
                          onClick={() => toggleScheduleStatus(sched)}
                        >
                          <div style={{ width: 34, height: 20, borderRadius: 10, background: sched.is_active ? 'var(--primary-color)' : '#cbd5e1', position: 'relative', transition: '0.2s' }}>
                            <div style={{ width: 16, height: 16, borderRadius: 8, background: 'white', position: 'absolute', top: 2, left: sched.is_active ? 16 : 2, transition: '0.2s' }} />
                          </div>
                          <span style={{ fontSize: '0.875rem', color: sched.is_active ? 'var(--text-color)' : '#64748b' }}>
                            {sched.is_active ? '已启用' : '已停用'}
                          </span>
                        </div>
                      </td>
                      <td style={{ padding: '1rem', textAlign: 'right', display: 'flex', gap: '0.75rem', justifyContent: 'flex-end' }}>
                        <button
                          onClick={() => handleTriggerSchedule(sched.id)}
                          style={{ padding: '0.4rem 0.8rem', fontSize: '0.75rem', background: 'transparent', border: '1px solid var(--border-color)', color: 'var(--success-color)', cursor: 'pointer', borderRadius: '4px', fontWeight: 500, transition: 'all 0.15s' }}
                          onMouseEnter={e => { e.currentTarget.style.background = 'rgba(16,185,129,0.06)'; }}
                          onMouseLeave={e => { e.currentTarget.style.background = 'transparent'; }}
                        >触发</button>
                        <button
                          onClick={() => { setEditingSchedule(sched); setIsSidebarOpen(true); }}
                          style={{ padding: '0.4rem 0.8rem', fontSize: '0.75rem', background: 'transparent', border: '1px solid var(--border-color)', color: 'var(--primary-color)', cursor: 'pointer', borderRadius: '4px', fontWeight: 500, transition: 'all 0.15s' }}
                          onMouseEnter={e => { e.currentTarget.style.background = 'rgba(37,99,235,0.06)'; }}
                          onMouseLeave={e => { e.currentTarget.style.background = 'transparent'; }}
                        >编辑</button>
                        <button
                          onClick={() => handleDeleteSchedule(sched.id)}
                          style={{ padding: '0.4rem 0.8rem', fontSize: '0.75rem', background: 'transparent', border: 'none', color: '#dc2626', cursor: 'pointer' }}
                        >删除</button>
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>

          <ScheduleSidebar
            isOpen={isSidebarOpen}
            onClose={() => { setIsSidebarOpen(false); setEditingSchedule(null); }}
            onSave={handleSaveSchedule}
            editingSchedule={editingSchedule}
            teams={teams}
            repos={repos}
          />
        </div>
      )}

      {/* Slideout report viewer sidebar */}
      <ReportSidebar open={sidebarOpen} onClose={() => setSidebarOpen(false)} markdown={currentMarkdown} loading={loadingMarkdown} reportId={currentReportId} />

      <style>{`
        .spinner-mini {
          width: 12px;
          height: 12px;
          border: 2px solid rgba(37, 99, 235, 0.15);
          border-top-color: var(--primary-color);
          border-radius: 50%;
          animation: spin 0.8s linear infinite;
          display: inline-block;
        }
        .badge.primary {
          background: rgba(37, 99, 235, 0.12);
          color: var(--primary-color);
        }
        @keyframes spin {
          to { transform: rotate(360deg); }
        }
      `}</style>
    </div>
  );
}

export default ScanManagement;
