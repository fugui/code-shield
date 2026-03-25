import React, { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useToast } from '../components/Toast';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { sshToHttps } from '../utils/urlUtils';


function TaskOverviewTab() {
  const { showToast } = useToast();
  const navigate = useNavigate();
  const [items, setItems] = useState<any[]>([]);
  const [teams, setTeams] = useState<any[]>([]);
  const [taskTypes, setTaskTypes] = useState<any[]>([]);
  
  const [filterTeam, setFilterTeam] = useState<string>('');
  const [filterServiceGroup, setFilterServiceGroup] = useState<string>('');
  const [filterOwner, setFilterOwner] = useState<string>('');
  const [filterTaskType, setFilterTaskType] = useState<string>('');
  const [openMenuRepoId, setOpenMenuRepoId] = useState<number | null>(null);

  const [page, setPage] = useState<number>(1);
  const [pageSize] = useState<number>(15);
  const [totalItems, setTotalItems] = useState<number>(0);
  const [totalPages, setTotalPages] = useState<number>(0);
  
  const [sortOrder, setSortOrder] = useState<'latest_task_time_desc' | 'latest_task_time_asc' | 'status_desc' | 'status_asc'>('latest_task_time_desc');

  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [currentMarkdown, setCurrentMarkdown] = useState<string>('');
  const [loadingMarkdown, setLoadingMarkdown] = useState(false);

  useEffect(() => {
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

  const handleFilterChange = (setter: React.Dispatch<React.SetStateAction<string>>, value: string) => {
    setter(value);
    setPage(1);
  };

  const toggleSort = (field: 'latest_task_time' | 'status') => {
    setSortOrder(prev => {
      if (field === 'latest_task_time') {
        return prev === 'latest_task_time_desc' ? 'latest_task_time_asc' : 'latest_task_time_desc';
      } else {
        return prev === 'status_desc' ? 'status_asc' : 'status_desc';
      }
    });
    setPage(1);
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

  const triggerTask = (repoId: number, taskTypeId: number) => {
    setOpenMenuRepoId(null);
    fetch('/api/tasks/trigger', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ repo_id: repoId, task_type_id: taskTypeId })
    }).then(res => {
      if (res.ok) {
        showToast('任务已成功下发！', 'success');
      } else {
        showToast('触发任务失败', 'error');
      }
    }).catch(err => {
      console.error(err);
      showToast('网络错误', 'error');
    });
  };

  const handleOpenReport = async (reportId: number) => {
    setSidebarOpen(true);
    setLoadingMarkdown(true);
    setCurrentMarkdown('');
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

  const getTaskTypeName = (id: number) => {
    const tt = taskTypes.find(t => t.id === id);
    return tt ? tt.display_name : '-';
  };

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.5rem' }}>
        <div style={{ display: 'flex', gap: '1rem', flexWrap: 'wrap' }}>
          <select value={filterTaskType} onChange={e => handleFilterChange(setFilterTaskType, e.target.value)} style={{ padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', outline: 'none' }}>
            <option value="">全部任务类型</option>
            {taskTypes.map(t => <option key={t.id} value={t.id}>{t.display_name}</option>)}
          </select>
          <select value={filterTeam} onChange={e => handleFilterChange(setFilterTeam, e.target.value)} style={{ padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', outline: 'none' }}>
            <option value="">全部部门</option>
            {teams.map(t => <option key={t.id} value={t.id}>{t.name}</option>)}
          </select>
          <input type="text" placeholder="按服务组过滤..." value={filterServiceGroup} onChange={e => handleFilterChange(setFilterServiceGroup, e.target.value)} style={{ padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', outline: 'none' }} />
          <input type="text" placeholder="按责任人过滤..." value={filterOwner} onChange={e => handleFilterChange(setFilterOwner, e.target.value)} style={{ padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', outline: 'none' }} />
        </div>
      </div>

      <div className="card" style={{ padding: 0, fontSize: '0.875rem' }}>
        <table className="table">
          <thead>
            <tr>
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
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {items.length === 0 ? (
              <tr><td colSpan={8} style={{ textAlign: 'center', padding: '3rem', color: '#94a3b8' }}>暂无代码仓或任务数据</td></tr>
            ) : items.map((item, idx) => (
              <tr key={item.repo.id || idx}>
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
                  {item.latest_task_status === 'none' ? (
                     <span style={{ color: '#aaa', fontSize: '0.875rem' }}>未执行</span>
                  ) : (
                    <span className={`badge ${item.latest_task_status === 'success' ? 'success' : (item.latest_task_status === 'failed' ? 'danger' : item.latest_task_status === 'queued' ? '' : 'warning')}`}>
                      {item.latest_task_status === 'success' ? '完成' : item.latest_task_status === 'failed' ? '失败' : item.latest_task_status === 'queued' ? '排队中' : item.latest_task_status === 'running' ? '执行中' : item.latest_task_status === 'skipped' ? '已跳过' : item.latest_task_status}
                    </span>
                  )}
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
                        </div>
                      )}
                    </div>
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
                        onClick={() => navigate(`/tasks/repo/${item.repo.id}`)}
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
                <td>
                  <div style={{ position: 'relative' }}>
                    <button 
                      className="btn" 
                      onClick={() => setOpenMenuRepoId(openMenuRepoId === item.repo.id ? null : item.repo.id)} 
                      style={{ padding: '0.3rem 0.6rem', fontSize: '0.875rem', background: 'transparent', border: '1px solid var(--primary-color)', color: 'var(--primary-color)', whiteSpace: 'nowrap', display: 'flex', alignItems: 'center', gap: '0.25rem' }}
                    >
                      执行 <span style={{ fontSize: '0.7rem' }}>▾</span>
                    </button>
                    {openMenuRepoId === item.repo.id && (
                      <>
                      <div onClick={() => setOpenMenuRepoId(null)} style={{ position: 'fixed', top: 0, left: 0, right: 0, bottom: 0, zIndex: 99 }} />
                      <div style={{ position: 'absolute', right: 0, top: '100%', marginTop: '4px', background: 'white', border: '1px solid var(--border-color)', borderRadius: '6px', boxShadow: '0 4px 12px rgba(0,0,0,0.12)', zIndex: 100, minWidth: '160px', overflow: 'hidden' }}>
                        {taskTypes.map(tt => (
                          <div
                            key={tt.id}
                            onClick={() => triggerTask(item.repo.id, tt.id)}
                            style={{ padding: '0.5rem 0.75rem', cursor: 'pointer', fontSize: '0.825rem', borderBottom: '1px solid #f1f5f9', display: 'flex', alignItems: 'center', gap: '0.5rem' }}
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

      {/* Markdown Sidebar Drawer */}
      <div style={{ position: 'fixed', top: 0, right: sidebarOpen ? 0 : '-50vw', width: '50vw', height: '100vh', background: 'var(--bg-color)', boxShadow: '-4px 0 15px rgba(0,0,0,0.1)', transition: 'right 0.3s ease-in-out', zIndex: 1000, display: 'flex', flexDirection: 'column' }}>
        <div style={{ padding: '1.5rem', borderBottom: '1px solid var(--border-color)', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <h3 style={{ margin: 0 }}>任务报告详情</h3>
          <button onClick={() => setSidebarOpen(false)} style={{ background: 'transparent', border: 'none', cursor: 'pointer', opacity: 0.6, fontSize: '1.5rem', color: 'var(--text-color)' }}>&times;</button>
        </div>
        <div style={{ padding: '2rem', overflowY: 'auto', flex: 1, backgroundColor: '#ffffff' }}>
          {loadingMarkdown ? (
            <div style={{ textAlign: 'center', marginTop: '3rem', color: '#64748b' }}>
              <span className="spinner"></span> 正在渲染 Markdown...
            </div>
          ) : (
            <div className="markdown-body">
              <ReactMarkdown remarkPlugins={[remarkGfm]}>
                {currentMarkdown || '*暂无任何报告信息*'}
              </ReactMarkdown>
            </div>
          )}
        </div>
      </div>

      {sidebarOpen && (
        <div onClick={() => setSidebarOpen(false)} style={{ position: 'fixed', top: 0, left: 0, right: 0, bottom: 0, background: 'rgba(0,0,0,0.5)', zIndex: 999 }} />
      )}

      <style>{`
        .spinner { display: inline-block; width: 12px; height: 12px; border: 2px solid rgba(100, 116, 139, 0.3); border-radius: 50%; border-top-color: var(--primary-color); animation: spin 1s ease-in-out infinite; vertical-align: middle; margin-right: 5px; }
        @keyframes spin { to { transform: rotate(360deg); } }
        .markdown-body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif; line-height: 1.6; color: #24292f; }
        .markdown-body h1, .markdown-body h2, .markdown-body h3, .markdown-body h4 { margin-top: 24px; margin-bottom: 16px; font-weight: 600; line-height: 1.25; }
        .markdown-body h2 { border-bottom: 1px solid #d0d7de; padding-bottom: .3em; }
        .markdown-body blockquote { padding: 0 1em; color: #57606a; border-left: .25em solid #d0d7de; margin: 0 0 16px 0; }
        .markdown-body pre { padding: 16px; overflow: auto; font-size: 85%; line-height: 1.45; background-color: #f6f8fa; border-radius: 6px; }
        .markdown-body code { padding: .2em .4em; margin: 0; font-size: 85%; background-color: rgba(175, 184, 193, 0.2); border-radius: 6px; }
        .markdown-body pre > code { padding: 0; margin: 0; font-size: 100%; background-color: transparent; border: 0; }
        .markdown-body ul, .markdown-body ol { margin-top: 0; margin-bottom: 16px; padding-left: 2em; }
      `}</style>
    </div>
  );
}

export default TaskOverviewTab;
