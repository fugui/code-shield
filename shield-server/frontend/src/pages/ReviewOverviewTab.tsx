import React, { useEffect, useState, useCallback } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { useToast } from '../components/Toast';
import { sshToHttps } from '../utils/urlUtils';
import ReportSidebar from '../components/ReportSidebar';
import { appNavigatePath } from '../config';

type SortOrder = 'latest_task_time_desc' | 'latest_task_time_asc' | 'status_desc' | 'status_asc';

function TaskOverviewTab() {
  const { showToast } = useToast();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();

  // Read filter state from URL search params, falling back to defaults
  const filterTeam = searchParams.get('team') || '';
  const filterServiceGroup = searchParams.get('sg') || '';
  const filterOwner = searchParams.get('owner') || '';
  const filterTaskType = searchParams.get('tt') || '';
  const sortOrder: SortOrder = (searchParams.get('sort') as SortOrder) || 'latest_task_time_desc';
  const page = parseInt(searchParams.get('page') || '1', 10) || 1;

  // Helper to update a single search param while preserving others
  const updateParam = useCallback((key: string, value: string) => {
    setSearchParams(prev => {
      const next = new URLSearchParams(prev);
      if (value) {
        next.set(key, value);
      } else {
        next.delete(key);
      }
      return next;
    }, { replace: true });
  }, [setSearchParams]);

  const setPage = (p: number) => updateParam('page', p <= 1 ? '' : p.toString());

  const [items, setItems] = useState<any[]>([]);
  const [teams, setTeams] = useState<any[]>([]);
  const [taskTypes, setTaskTypes] = useState<any[]>([]);
  const [selectedRepoIds, setSelectedRepoIds] = useState<number[]>([]);
  const [openBatchMenu, setOpenBatchMenu] = useState(false);

  const [pageSize] = useState<number>(15);
  const [totalItems, setTotalItems] = useState<number>(0);
  const [totalPages, setTotalPages] = useState<number>(0);

  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [currentMarkdown, setCurrentMarkdown] = useState<string>('');
  const [loadingMarkdown, setLoadingMarkdown] = useState(false);
  const [currentReportId, setCurrentReportId] = useState<number | undefined>(undefined);

  const [isAdmin, setIsAdmin] = useState(false);

  useEffect(() => {
    fetch('/api/me')
      .then(res => res.ok ? res.json() : null)
      .then(data => { if (data) setIsAdmin(!!data.is_admin); })
      .catch(() => {});

    fetch('/api/teams')
      .then(res => res.json())
      .then(data => setTeams(data || []))
      .catch(console.error);
    fetch('/api/task-types?active_only=true')
      .then(res => res.json())
      .then(data => {
        setTaskTypes(data || []);
      })
      .catch(console.error);
  }, []);

  useEffect(() => {
    setSelectedRepoIds([]);
    fetchOverview();
  }, [page, filterTeam, filterServiceGroup, filterOwner, filterTaskType, sortOrder]);

  const fetchOverview = () => {
    const params = new URLSearchParams({
      page: page.toString(),
      pageSize: pageSize.toString(),
      sort: sortOrder,
    });
    if (filterTeam) params.append('team_id', filterTeam);
    if (filterServiceGroup) params.append('service_group', filterServiceGroup);
    if (filterOwner) params.append('owner', filterOwner);
    if (filterTaskType) params.append('task_type_id', filterTaskType);

    fetch(`/api/tasks/overview?${params.toString()}`)
      .then(res => res.json())
      .then(data => {
        setItems(data.items || []);
        setTotalItems(data.total || 0);
        setTotalPages(data.totalPages || 0);
      })
      .catch(console.error);
  };

  const handleFilterChange = (paramKey: string, value: string) => {
    setSearchParams(prev => {
      const next = new URLSearchParams(prev);
      if (value) {
        next.set(paramKey, value);
      } else {
        next.delete(paramKey);
      }
      next.delete('page'); // reset page to 1
      return next;
    }, { replace: true });
  };

  const handleClearInvalidReports = async () => {
    if (!window.confirm('确认清除所有不是“完成”状态的无效报告记录吗？进行中的任务可能会受影响。')) return;
    try {
      const res = await fetch('/api/tasks/invalid-reports', { method: 'DELETE' });
      if (res.ok) {
        const data = await res.json();
        showToast(`成功清除 ${data.deleted} 条无效报告记录`, 'success');
        fetchOverview();
      } else {
        const err = await res.json();
        showToast(`清除失败: ${err.error || '未知错误'}`, 'error');
      }
    } catch (err) {
      showToast('请求失败，请检查网络连接', 'error');
    }
  };

  const toggleSort = (field: 'latest_task_time' | 'status') => {
    setSearchParams(prev => {
      const next = new URLSearchParams(prev);
      const currentSort: SortOrder = (prev.get('sort') as SortOrder) || 'latest_task_time_desc';
      let newSort: SortOrder;
      if (field === 'latest_task_time') {
        newSort = currentSort === 'latest_task_time_desc' ? 'latest_task_time_asc' : 'latest_task_time_desc';
      } else {
        newSort = currentSort === 'status_desc' ? 'status_asc' : 'status_desc';
      }
      if (newSort === 'latest_task_time_desc') {
        next.delete('sort');
      } else {
        next.set('sort', newSort);
      }
      next.delete('page'); // reset page to 1
      return next;
    }, { replace: true });
  };

  const getSortIcon = (field: 'latest_task_time' | 'status') => {
    if (field === 'latest_task_time') {
      if (sortOrder === 'latest_task_time_desc') return ' ↓';
      if (sortOrder === 'latest_task_time_asc') return ' ↑';
      return '';
    } else {
      if (sortOrder === 'status_desc') return ' ↓';
      if (sortOrder === 'status_asc') return ' ↑';
      return '';
    }
  };

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
      })
        .then(res => {
          if (res.ok) {
            successCount++;
          } else {
            failCount++;
          }
        })
        .catch(err => {
          console.error(err);
          failCount++;
        })
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
    fetchOverview();
  };

  const handleOpenReport = async (reportId: number) => {
    setSidebarOpen(true);
    setLoadingMarkdown(true);
    setCurrentMarkdown('');
    setCurrentReportId(reportId);
    try {
      const res = await fetch(`/api/tasks/${reportId}/report`);
      if (res.ok) {
        const text = await res.text();
        setCurrentMarkdown(text);
      } else {
        const errData = await res.json();
        setCurrentMarkdown(`### 获取报告数据失败\n\n原因: ${errData.error || 'Server error'}`);
      }
    } catch (err) {
      setCurrentMarkdown('### 获取报告数据失败\n\n原因:网络请求异常。');
    } finally {
      setLoadingMarkdown(false);
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
    } catch (err) {
      console.error('Failed to send notification:', err);
      showToast('网络异常，发送失败', 'error');
    }
  };

  const handleResume = async (reportId: number) => {
    try {
      const res = await fetch(`/api/tasks/${reportId}/resume`, { method: 'POST' });
      if (res.ok) {
        showToast('恢复任务已入队，等待排队执行', 'success');
        fetchOverview();
      } else {
        const data = await res.json();
        showToast(`恢复失败: ${data.error || '未知错误'}`, 'error');
      }
    } catch (err) {
      console.error('Failed to resume task:', err);
      showToast('网络异常，恢复失败', 'error');
    }
  };

  const getTaskTypeName = (id: number) => {
    const tt = taskTypes.find(t => t.id === id);
    return tt ? tt.display_name : '-';
  };

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.5rem' }}>
        <div style={{ display: 'flex', gap: '1rem', flexWrap: 'wrap' }}>
          <select value={filterTaskType} onChange={e => handleFilterChange('tt', e.target.value)} style={{ padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', outline: 'none' }}>
            <option value="">全部任务类型</option>
            {taskTypes.map(t => <option key={t.id} value={t.id}>{t.display_name}</option>)}
          </select>
          <select value={filterTeam} onChange={e => handleFilterChange('team', e.target.value)} style={{ padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', outline: 'none' }}>
            <option value="">全部部门</option>
            {teams.map(t => <option key={t.id} value={t.id}>{t.name}</option>)}
          </select>
          <input type="text" placeholder="按服务组过滤..." value={filterServiceGroup} onChange={e => handleFilterChange('sg', e.target.value)} style={{ padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', outline: 'none' }} />
          <input type="text" placeholder="按责任人过滤..." value={filterOwner} onChange={e => handleFilterChange('owner', e.target.value)} style={{ padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', outline: 'none' }} />
        </div>
        <div style={{ display: 'flex', gap: '0.75rem', alignItems: 'center', position: 'relative' }}>
          <div style={{ position: 'relative' }}>
            <button
              className="btn"
              disabled={selectedRepoIds.length === 0}
              onClick={() => setOpenBatchMenu(prev => !prev)}
              style={{
                padding: '0.4rem 0.8rem',
                borderRadius: '4px',
                background: selectedRepoIds.length === 0 ? 'var(--border-color)' : 'var(--primary-color)',
                color: selectedRepoIds.length === 0 ? '#94a3b8' : 'white',
                border: 'none',
                cursor: selectedRepoIds.length === 0 ? 'not-allowed' : 'pointer',
                fontSize: '0.875rem',
                display: 'flex',
                alignItems: 'center',
                gap: '0.25rem'
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

          {isAdmin && (
            <button 
              className="btn"
              onClick={handleClearInvalidReports}
              style={{ 
                background: 'transparent', 
                color: 'var(--danger-color)', 
                border: '1px solid var(--danger-color)', 
                padding: '0.4rem 0.8rem', 
                borderRadius: '4px', 
                cursor: 'pointer',
                fontSize: '0.875rem'
              }}
              onMouseEnter={e => e.currentTarget.style.backgroundColor = 'rgba(239, 68, 68, 0.1)'}
              onMouseLeave={e => e.currentTarget.style.backgroundColor = 'transparent'}
            >
              清除无效报告
            </button>
          )}
        </div>
      </div>

      <div className="card" style={{ padding: 0, fontSize: '0.875rem' }}>
        <table className="table">
          <thead>
            <tr>
              <th style={{ width: '40px', textAlign: 'center' }}>
                <input
                  type="checkbox"
                  checked={items.length > 0 && selectedRepoIds.length === items.length}
                  ref={input => {
                    if (input) {
                      input.indeterminate = selectedRepoIds.length > 0 && selectedRepoIds.length < items.length;
                    }
                  }}
                  onChange={e => {
                    if (e.target.checked) {
                      setSelectedRepoIds(items.map(item => item.repo.id));
                    } else {
                      setSelectedRepoIds([]);
                    }
                  }}
                  style={{ cursor: 'pointer' }}
                />
              </th>
              <th style={{ width: '320px' }}>代码仓</th>
              <th style={{ width: '160px' }}>归属部门</th>
              <th>负责人</th>
              <th>任务类型</th>
              <th
                onClick={() => toggleSort('latest_task_time')}
                style={{ cursor: 'pointer', userSelect: 'none', color: sortOrder.startsWith('latest_task_time') ? 'var(--primary-color)' : 'inherit' }}
                title="点击切换排序方式"
              >
                最近执行时间
                {getSortIcon('latest_task_time')}
              </th>
              <th
                onClick={() => toggleSort('status')}
                style={{ cursor: 'pointer', userSelect: 'none', color: sortOrder.startsWith('status') ? 'var(--primary-color)' : 'inherit' }}
                title="点击切换排序方式"
              >
                状态
                {getSortIcon('status')}
              </th>
              <th>评分</th>
              <th>历史报告</th>
            </tr>
          </thead>
          <tbody>
            {items.length === 0 ? (
              <tr><td colSpan={9} style={{ textAlign: 'center', padding: '3rem', color: '#94a3b8' }}>暂无代码仓或任务数据</td></tr>
            ) : items.map((item, idx) => (
              <tr key={item.repo.id || idx}>
                <td style={{ textAlign: 'center' }}>
                  <input
                    type="checkbox"
                    checked={selectedRepoIds.includes(item.repo.id)}
                    onChange={e => {
                      if (e.target.checked) {
                        setSelectedRepoIds(prev => [...prev, item.repo.id]);
                      } else {
                        setSelectedRepoIds(prev => prev.filter(id => id !== item.repo.id));
                      }
                    }}
                    style={{ cursor: 'pointer' }}
                  />
                </td>
                <td style={{ fontWeight: 500, width: '320px', maxWidth: '320px' }}>
                  {(() => {
                    const shortName = item.repo.name?.includes(':') ? item.repo.name.split(':').pop() : item.repo.name;
                    return item.repo.url ? (
                      <a
                        href={sshToHttps(item.repo.url)}
                        target="_blank"
                        rel="noreferrer"
                        style={{ color: 'var(--primary-color)', textDecoration: 'none', display: 'flex', alignItems: 'center', gap: '0.3rem', overflow: 'hidden', maxWidth: '100%' }}
                        onMouseEnter={e => e.currentTarget.style.textDecoration = 'underline'}
                        onMouseLeave={e => e.currentTarget.style.textDecoration = 'none'}
                        title={item.repo.name + (item.repo.url ? '\n' + item.repo.url : '')}
                      >
                        <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', direction: 'rtl', unicodeBidi: 'plaintext', flex: 1 }}>{shortName}</span>
                        <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={{ opacity: 0.6, flexShrink: 0 }}>
                          <path d="M18 13v6a2 2 0 01-2 2H5a2 2 0 01-2-2V8a2 2 0 012-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/>
                        </svg>
                      </a>
                    ) : (
                      <span style={{ color: 'var(--primary-color)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', direction: 'rtl', unicodeBidi: 'plaintext', display: 'block' }}>{shortName}</span>
                    );
                  })()}
                </td>
                <td>{item.repo.team?.name || '未知'}</td>
                <td>
                  {item.repo.owner ? (
                    <span>
                      <span>{item.repo.owner.name}</span>
                      <br/>
                      <span style={{ color: '#94a3b8', fontSize: '0.78rem' }}>{item.repo.owner.id}</span>
                    </span>
                  ) : (
                    <span style={{ color: '#94a3b8' }}>{item.repo.owner_id || '-'}</span>
                  )}
                </td>
                <td>
                  {item.task_type_id ? (
                    <span style={{ display: 'inline-block', padding: '0.15rem 0.5rem', borderRadius: '4px', background: 'rgba(37, 99, 235, 0.08)', color: 'var(--primary-color)', fontSize: '0.75rem', fontWeight: 500 }}>
                      {getTaskTypeName(item.task_type_id)}
                    </span>
                  ) : (
                    <span style={{ color: '#aaa' }}>-</span>
                  )}
                </td>
                <td>
                  {item.latest_task_time ? (
                    <span style={{ color: '#64748b', fontSize: '0.875rem' }}>{item.latest_task_time}</span>
                  ) : (
                    <span style={{ color: '#aaa', fontSize: '0.875rem' }}>无数据</span>
                  )}
                </td>
                <td>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', flexWrap: 'wrap' }}>
                    {item.latest_task_status === 'none' ? (
                       <span style={{ color: '#aaa', fontSize: '0.875rem' }}>未执行</span>
                    ) : (
                      <span className={`badge ${
                        item.latest_task_status === 'success' ? 'success' : 
                        item.latest_task_status === 'failed' ? 'danger' : 
                        item.latest_task_status === 'queued' ? '' : 
                        (item.latest_task_status === 'running' || 
                         item.latest_task_status === 'cloning' || 
                         item.latest_task_status === 'pre_processing' || 
                         item.latest_task_status === 'analyzing' || 
                         item.latest_task_status === 'post_processing') ? 'warning' : 'info'
                      }`}>
                        {
                          item.latest_task_status === 'success' ? '完成' : 
                          item.latest_task_status === 'failed' ? '失败' : 
                          item.latest_task_status === 'queued' ? '排队中' : 
                          item.latest_task_status === 'running' ? '执行中' : 
                          item.latest_task_status === 'skipped' ? '已跳过' : 
                          item.latest_task_status === 'cloning' ? '克隆中' :
                          item.latest_task_status === 'pre_processing' ? '检查中' :
                          item.latest_task_status === 'analyzing' ? (item.total_chunks > 1 ? `分析中 (${item.processed_chunks}/${item.total_chunks})` : '分析中') :
                          item.latest_task_status === 'post_processing' ? '处理中' :
                          item.latest_task_status
                        }
                      </span>
                    )}
                    {(item.latest_task_status === 'success' || item.latest_task_status === 'failed') && item.total_chunks > 0 && (() => {
                      const success = item.success_chunks ?? 0;
                      const total = item.total_chunks;
                      const allSuccess = success === total;
                      if (allSuccess) return null;
                      return (
                        <span 
                          style={{
                            display: 'inline-block',
                            padding: '0.1rem 0.4rem',
                            borderRadius: '4px',
                            fontSize: '0.75rem',
                            fontWeight: 600,
                            background: 'rgba(245, 158, 11, 0.08)',
                            color: '#d97706',
                            border: '1px solid rgba(245, 158, 11, 0.15)',
                          }}
                          title={`分片进度：成功 ${success} / 总数 ${total}`}
                        >
                          {success}/{total}
                        </span>
                      );
                    })()}
                  </div>
                </td>
                <td>
                  {item.latest_task_status === 'success' ? (
                    <div style={{ display: 'flex', gap: '0.75rem', alignItems: 'center' }}>
                      <span style={{ fontWeight: 700, fontSize: '1rem', color: item.latest_task_score >= 20 ? '#ef4444' : item.latest_task_score >= 10 ? '#f59e0b' : '#22c55e' }}>
                        {item.latest_task_score}
                      </span>
                      {item.latest_task_id && (
                        <div style={{ display: 'flex', gap: '0.25rem' }}>
                          <button 
                            onClick={() => handleOpenReport(item.latest_task_id)}
                            style={{ background: 'transparent', border: 'none', cursor: 'pointer', display: 'flex', alignItems: 'center', padding: '0.25rem', borderRadius: '4px', color: 'var(--primary-color)' }}
                            title="查看详细报告"
                            onMouseEnter={e => e.currentTarget.style.backgroundColor = 'rgba(37, 99, 235, 0.1)'}
                            onMouseLeave={e => e.currentTarget.style.backgroundColor = 'transparent'}
                          >
                            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                              <circle cx="11" cy="11" r="8"></circle><line x1="21" y1="21" x2="16.65" y2="16.65"></line>
                            </svg>
                          </button>
                          <button 
                            onClick={() => handleNotify(item.latest_task_id)}
                            style={{ background: 'transparent', border: 'none', cursor: 'pointer', display: 'flex', alignItems: 'center', padding: '0.25rem', borderRadius: '4px', color: '#10b981' }}
                            title="手动发送报告通知给相关责任人"
                            onMouseEnter={e => e.currentTarget.style.backgroundColor = 'rgba(16, 185, 129, 0.1)'}
                            onMouseLeave={e => e.currentTarget.style.backgroundColor = 'transparent'}
                          >
                            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                              <rect x="2" y="4" width="20" height="16" rx="2"></rect><polyline points="2,4 12,13 22,4"></polyline>
                            </svg>
                          </button>
                          {item.total_chunks > 0 && (item.success_chunks ?? 0) !== item.total_chunks && (
                            <button
                              onClick={() => handleResume(item.latest_task_id)}
                              style={{ background: 'transparent', border: 'none', cursor: 'pointer', display: 'flex', alignItems: 'center', padding: '0.25rem', borderRadius: '4px', color: '#f59e0b' }}
                              title="恢复：重试失败的分片并重新生成报告"
                              onMouseEnter={e => e.currentTarget.style.backgroundColor = 'rgba(245, 158, 11, 0.1)'}
                              onMouseLeave={e => e.currentTarget.style.backgroundColor = 'transparent'}
                            >
                              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                                <polyline points="1 4 1 10 7 10"></polyline>
                                <path d="M3.51 15a9 9 0 1 0 2.13-9.36L1 10"></path>
                              </svg>
                            </button>
                          )}
                        </div>
                      )}
                    </div>
                  ) : item.latest_task_status === 'failed' && item.latest_task_id && item.total_chunks > 0 && (item.success_chunks ?? 0) !== item.total_chunks ? (
                    <button
                      onClick={() => handleResume(item.latest_task_id)}
                      style={{ background: 'transparent', border: '1px solid #f59e0b', cursor: 'pointer', display: 'flex', alignItems: 'center', gap: '0.3rem', padding: '0.25rem 0.5rem', borderRadius: '4px', color: '#f59e0b', fontSize: '0.8rem' }}
                      title="恢复：重试失败的分片并重新生成报告"
                      onMouseEnter={e => { e.currentTarget.style.backgroundColor = 'rgba(245, 158, 11, 0.1)'; }}
                      onMouseLeave={e => { e.currentTarget.style.backgroundColor = 'transparent'; }}
                    >
                      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                        <polyline points="1 4 1 10 7 10"></polyline>
                        <path d="M3.51 15a9 9 0 1 0 2.13-9.36L1 10"></path>
                      </svg>
                      恢复
                    </button>
                  ) : (
                    <span style={{ color: '#aaa', fontSize: '0.875rem' }}>-</span>
                  )}
                </td>
                <td>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '0.4rem' }}>
                    <span style={{ fontSize: '0.875rem', color: item.report_count > 0 ? 'var(--text-color)' : '#aaa' }}>
                      {item.report_count}
                    </span>
                    {item.report_count > 0 && (
                      <button
                        onClick={() => navigate(appNavigatePath(`/tasks/repo/${item.repo.id}`), { state: { returnSearch: searchParams.toString() } })}
                        style={{ background: 'transparent', border: 'none', cursor: 'pointer', display: 'flex', alignItems: 'center', padding: '0.2rem', borderRadius: '4px', color: 'var(--primary-color)' }}
                        title="查看历史报告"
                        onMouseEnter={e => e.currentTarget.style.backgroundColor = 'rgba(37, 99, 235, 0.1)'}
                        onMouseLeave={e => e.currentTarget.style.backgroundColor = 'transparent'}
                      >
                        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                          <path d="M9 18l6-6-6-6"></path>
                        </svg>
                      </button>
                    )}
                  </div>
                </td>

              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {totalPages > 0 && (
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginTop: '1rem', padding: '0.5rem', background: 'white', borderRadius: '6px', border: '1px solid var(--border-color)' }}>
          <span style={{ fontSize: '0.875rem', color: 'var(--text-color)' }}>
            共 {totalItems} 条记录，当前第 {page} / {totalPages} 页
          </span>
          <div style={{ display: 'flex', gap: '0.5rem' }}>
            <button className="btn" disabled={page === 1} onClick={() => setPage(page - 1)} style={{ background: page === 1 ? '#f1f5f9' : 'white', color: page === 1 ? '#94a3b8' : 'var(--text-color)', border: '1px solid var(--border-color)' }}>上一页</button>
            <button className="btn" disabled={page >= totalPages} onClick={() => setPage(page + 1)} style={{ background: page >= totalPages ? '#f1f5f9' : 'white', color: page >= totalPages ? '#94a3b8' : 'var(--text-color)', border: '1px solid var(--border-color)' }}>下一页</button>
          </div>
        </div>
      )}

      <ReportSidebar open={sidebarOpen} onClose={() => setSidebarOpen(false)} markdown={currentMarkdown} loading={loadingMarkdown} reportId={currentReportId} />
    </div>
  );
}

export default TaskOverviewTab;
