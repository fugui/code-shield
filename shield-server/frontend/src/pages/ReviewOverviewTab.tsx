import React, { useEffect, useState, useCallback } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { useToast } from '../components/Toast';
import { sshToHttps } from '../utils/urlUtils';
import ReportSidebar from '../components/ReportSidebar';
import { appNavigatePath } from '../config';

function TaskOverviewTab() {
  const { showToast } = useToast();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();

  // Read filter state from URL search params, falling back to defaults
  const filterTeam = searchParams.get('team') || '';
  const filterServiceGroup = searchParams.get('sg') || '';
  const filterOwner = searchParams.get('owner') || '';
  const filterTaskType = searchParams.get('tt') || '';
  const filterStatus = searchParams.get('status') || '';
  const filterSearch = searchParams.get('search') || '';
  const page = parseInt(searchParams.get('page') || '1', 10) || 1;

  const [items, setItems] = useState<any[]>([]);
  const [teams, setTeams] = useState<any[]>([]);
  const [taskTypes, setTaskTypes] = useState<any[]>([]);

  const [pageSize] = useState<number>(15);
  const [totalItems, setTotalItems] = useState<number>(0);
  const [totalPages, setTotalPages] = useState<number>(0);

  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [currentMarkdown, setCurrentMarkdown] = useState<string>('');
  const [loadingMarkdown, setLoadingMarkdown] = useState(false);
  const [currentReportId, setCurrentReportId] = useState<number | undefined>(undefined);

  useEffect(() => {
    fetch('/api/teams')
      .then(res => res.json())
      .then(data => {
        const list = Array.isArray(data) ? data : (data.items || []);
        setTeams(list);
      })
      .catch(console.error);
    fetch('/api/task-types?active_only=true')
      .then(res => res.json())
      .then(data => {
        setTaskTypes(data || []);
      })
      .catch(console.error);
  }, []);

  const fetchOverview = useCallback(() => {
    const params = new URLSearchParams({
      page: page.toString(),
      pageSize: pageSize.toString(),
    });
    if (filterTeam) params.append('team_id', filterTeam);
    if (filterServiceGroup) params.append('service_group', filterServiceGroup);
    if (filterOwner) params.append('owner', filterOwner);
    if (filterTaskType) params.append('task_type_id', filterTaskType);
    if (filterStatus) params.append('status', filterStatus);
    if (filterSearch) params.append('search', filterSearch);

    fetch(`/api/tasks?${params.toString()}`)
      .then(res => res.json())
      .then(data => {
        setItems(data.items || []);
        setTotalItems(data.total || 0);
        setTotalPages(data.totalPages || 0);
      })
      .catch(console.error);
  }, [page, pageSize, filterTeam, filterServiceGroup, filterOwner, filterTaskType, filterStatus, filterSearch]);

  useEffect(() => {
    fetchOverview();
  }, [fetchOverview]);

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

  const setPage = (p: number) => {
    setSearchParams(prev => {
      const next = new URLSearchParams(prev);
      if (p <= 1) {
        next.delete('page');
      } else {
        next.set('page', p.toString());
      }
      return next;
    }, { replace: true });
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
      setCurrentMarkdown('### 获取报告数据失败\n\n原因: 网络请求异常。');
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

  const renderStatusBadge = (status: string, total: number, processed: number, success: number) => {
    let text = status;
    let cls = 'info';

    switch (status) {
      case 'success':
        text = '完成';
        cls = 'success';
        break;
      case 'failed':
        text = '失败';
        cls = 'danger';
        break;
      case 'queued':
        text = '排队中';
        cls = 'info';
        break;
      case 'running':
        text = '执行中';
        cls = 'warning';
        break;
      case 'skipped':
        text = '已跳过';
        cls = 'info';
        break;
      case 'cloning':
        text = '克隆中';
        cls = 'warning';
        break;
      case 'pre_processing':
        text = '检查中';
        cls = 'warning';
        break;
      case 'analyzing':
        text = total > 1 ? `分析中 (${processed}/${total})` : '分析中';
        cls = 'warning';
        break;
      case 'post_processing':
        text = '处理中';
        cls = 'warning';
        break;
    }

    const chunkBadge = status !== 'none' && total > 0 && success !== total && (
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
          marginLeft: '0.5rem'
        }}
        title={`分片进度：成功 ${success} / 总数 ${total}`}
      >
        {success}/{total}
      </span>
    );

    return (
      <div style={{ display: 'inline-flex', alignItems: 'center' }}>
        <span className={`badge ${cls}`}>{text}</span>
        {chunkBadge}
      </div>
    );
  };

  const getOverviewText = (item: any) => {
    if (item.status !== 'success') return '';

    if (item.metrics) {
      try {
        const m = typeof item.metrics === 'string' ? JSON.parse(item.metrics) : item.metrics;
        const parts: string[] = [];
        
        if (m.blocking > 0) parts.push(`阻塞:${m.blocking}个`);
        if (m.critical > 0) parts.push(`严重:${m.critical}个`);
        if (m.major > 0) parts.push(`主要:${m.major}个`);
        if (m.hint > 0) parts.push(`提示:${m.hint}个`);
        if (m.suggestion > 0) parts.push(`建议:${m.suggestion}个`);

        if (m.high_risk > 0) parts.push(`高风险:${m.high_risk}个`);
        if (m.medium_risk > 0) parts.push(`中风险:${m.medium_risk}个`);
        if (m.low_risk > 0) parts.push(`低风险:${m.low_risk}个`);

        if (parts.length > 0) {
          return parts.join('，');
        }
      } catch (e) {
        // Fallback
      }
    }

    return item.ai_summary || '未发现任何问题，代码质量极佳！';
  };

  return (
    <div>
      {/* Search and Filters */}
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: '1rem', marginBottom: '1.5rem', background: 'var(--card-bg)', padding: '1rem', borderRadius: '8px', border: '1px solid var(--border-color)', alignItems: 'center' }}>
        {/* Search */}
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.25rem', minWidth: '220px', flex: '1 1 auto' }}>
          <label style={{ fontSize: '0.75rem', fontWeight: 600, color: '#64748b' }}>综合检索 (名称 / 摘要)</label>
          <input
            type="text"
            placeholder="搜索代码仓、AI摘要..."
            value={filterSearch}
            onChange={e => handleFilterChange('search', e.target.value)}
            style={{ padding: '0.5rem 0.75rem', borderRadius: '6px', border: '1px solid var(--border-color)', outline: 'none', fontSize: '0.875rem', background: 'var(--bg-color)', color: 'var(--text-color)' }}
          />
        </div>

        {/* Task Type */}
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.25rem', minWidth: '150px' }}>
          <label style={{ fontSize: '0.75rem', fontWeight: 600, color: '#64748b' }}>任务类型</label>
          <select
            value={filterTaskType}
            onChange={e => handleFilterChange('tt', e.target.value)}
            style={{ padding: '0.5rem 0.75rem', borderRadius: '6px', border: '1px solid var(--border-color)', outline: 'none', fontSize: '0.875rem', background: 'var(--bg-color)', color: 'var(--text-color)', cursor: 'pointer' }}
          >
            <option value="">全部任务类型</option>
            {taskTypes.map(t => <option key={t.id} value={t.id}>{t.display_name}</option>)}
          </select>
        </div>

        {/* Team */}
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.25rem', minWidth: '150px' }}>
          <label style={{ fontSize: '0.75rem', fontWeight: 600, color: '#64748b' }}>归属部门</label>
          <select
            value={filterTeam}
            onChange={e => handleFilterChange('team', e.target.value)}
            style={{ padding: '0.5rem 0.75rem', borderRadius: '6px', border: '1px solid var(--border-color)', outline: 'none', fontSize: '0.875rem', background: 'var(--bg-color)', color: 'var(--text-color)', cursor: 'pointer' }}
          >
            <option value="">全部部门</option>
            {teams.map(t => <option key={t.id} value={t.id}>{t.name}</option>)}
          </select>
        </div>

        {/* Status */}
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.25rem', minWidth: '120px' }}>
          <label style={{ fontSize: '0.75rem', fontWeight: 600, color: '#64748b' }}>任务状态</label>
          <select
            value={filterStatus}
            onChange={e => handleFilterChange('status', e.target.value)}
            style={{ padding: '0.5rem 0.75rem', borderRadius: '6px', border: '1px solid var(--border-color)', outline: 'none', fontSize: '0.875rem', background: 'var(--bg-color)', color: 'var(--text-color)', cursor: 'pointer' }}
          >
            <option value="">全部状态</option>
            <option value="success">完成</option>
            <option value="failed">失败</option>
            <option value="running">执行中</option>
            <option value="queued">排队中</option>
            <option value="skipped">已跳过</option>
          </select>
        </div>

        {/* Service Group */}
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.25rem', minWidth: '150px' }}>
          <label style={{ fontSize: '0.75rem', fontWeight: 600, color: '#64748b' }}>服务组</label>
          <input
            type="text"
            placeholder="按服务组过滤..."
            value={filterServiceGroup}
            onChange={e => handleFilterChange('sg', e.target.value)}
            style={{ padding: '0.5rem 0.75rem', borderRadius: '6px', border: '1px solid var(--border-color)', outline: 'none', fontSize: '0.875rem', background: 'var(--bg-color)', color: 'var(--text-color)' }}
          />
        </div>

        {/* Owner */}
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.25rem', minWidth: '150px' }}>
          <label style={{ fontSize: '0.75rem', fontWeight: 600, color: '#64748b' }}>责任人</label>
          <input
            type="text"
            placeholder="按责任人过滤..."
            value={filterOwner}
            onChange={e => handleFilterChange('owner', e.target.value)}
            style={{ padding: '0.5rem 0.75rem', borderRadius: '6px', border: '1px solid var(--border-color)', outline: 'none', fontSize: '0.875rem', background: 'var(--bg-color)', color: 'var(--text-color)' }}
          />
        </div>

        {/* Reset Filters */}
        <div style={{ display: 'flex', alignItems: 'flex-end', height: '38px', marginTop: 'auto' }}>
          {(filterTeam || filterServiceGroup || filterOwner || filterTaskType || filterStatus || filterSearch) && (
            <button
              onClick={() => {
                setSearchParams(new URLSearchParams(), { replace: true });
              }}
              style={{
                background: 'transparent',
                border: '1px solid var(--border-color)',
                padding: '0.5rem 1rem',
                borderRadius: '6px',
                fontSize: '0.875rem',
                color: '#64748b',
                cursor: 'pointer',
                transition: 'all 0.2s',
                display: 'inline-flex',
                alignItems: 'center',
                gap: '0.35rem'
              }}
              onMouseEnter={e => {
                e.currentTarget.style.borderColor = 'var(--primary-color)';
                e.currentTarget.style.color = 'var(--primary-color)';
              }}
              onMouseLeave={e => {
                e.currentTarget.style.borderColor = 'var(--border-color)';
                e.currentTarget.style.color = '#64748b';
              }}
            >
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M21.5 2v6h-6M21.34 15.57a10 10 0 1 1-.57-8.38l5.67-5.67"/></svg>
              重置筛选
            </button>
          )}
        </div>
      </div>

      {/* Reports list */}
      <div className="card" style={{ padding: 0, fontSize: '0.875rem', overflow: 'hidden' }}>
        <table className="table">
          <thead>
            <tr>
              <th style={{ width: '80px' }}>报告 ID</th>
              <th style={{ width: '130px' }}>任务类型</th>
              <th style={{ width: '220px' }}>代码仓</th>
              <th style={{ width: '130px' }}>归属部门</th>
              <th style={{ width: '100px' }}>负责人</th>
              <th style={{ width: '160px' }}>执行时间</th>
              <th style={{ width: '100px' }}>状态</th>
              <th style={{ width: '120px' }}>评分 / 报告</th>
              <th>问题统计与分析摘要</th>
            </tr>
          </thead>
          <tbody>
            {items.length === 0 ? (
              <tr><td colSpan={9} style={{ textAlign: 'center', padding: '3rem', color: '#94a3b8' }}>暂无任务报告数据</td></tr>
            ) : items.map((item, idx) => (
              <tr key={item.id || idx}>
                <td>
                  {item.status === 'success' ? (
                    <span 
                      onClick={() => handleOpenReport(item.id)}
                      style={{ color: 'var(--primary-color)', cursor: 'pointer', fontWeight: 600, fontFamily: 'monospace' }}
                      onMouseEnter={e => e.currentTarget.style.textDecoration = 'underline'}
                      onMouseLeave={e => e.currentTarget.style.textDecoration = 'none'}
                      title="点击查看详细报告"
                    >
                      #{item.id}
                    </span>
                  ) : (
                    <span style={{ color: '#64748b', fontFamily: 'monospace' }}>#{item.id}</span>
                  )}
                </td>
                <td>
                  {item.task_type ? (
                    <span style={{ display: 'inline-block', padding: '0.15rem 0.5rem', borderRadius: '4px', background: 'rgba(37, 99, 235, 0.08)', color: 'var(--primary-color)', fontSize: '0.75rem', fontWeight: 500 }}>
                      {item.task_type.display_name}
                    </span>
                  ) : (
                    <span style={{ color: '#aaa' }}>-</span>
                  )}
                </td>
                <td style={{ fontWeight: 500, width: '220px', maxWidth: '220px' }}>
                  {item.repo ? (() => {
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
                  })() : <span style={{ color: '#aaa' }}>已删除仓库</span>}
                </td>
                <td>{item.repo?.team?.name || teams.find(t => t.id === item.repo?.team_id)?.name || '-'}</td>
                <td>
                  {item.repo?.owner ? (
                    <span>
                      <span>{item.repo.owner.name}</span>
                      <br/>
                      <span style={{ color: '#94a3b8', fontSize: '0.78rem' }}>{item.repo.owner.id}</span>
                    </span>
                  ) : (
                    <span style={{ color: '#94a3b8' }}>{item.repo?.owner_id || '-'}</span>
                  )}
                </td>
                <td style={{ color: '#64748b' }}>
                  {item.created_at ? item.created_at.replace('T', ' ').substring(0, 19) : '-'}
                </td>
                <td>
                  {renderStatusBadge(item.status, item.total_chunks, item.processed_chunks, item.success_chunks)}
                </td>
                <td>
                  {item.status === 'success' ? (
                    <div style={{ display: 'flex', gap: '0.75rem', alignItems: 'center' }}>
                      <span style={{ fontWeight: 700, fontSize: '1rem', color: item.score >= 20 ? '#ef4444' : item.score >= 10 ? '#f59e0b' : '#22c55e' }}>
                        {item.score}
                      </span>
                      <div style={{ display: 'flex', gap: '0.25rem' }}>
                        <button 
                          onClick={() => handleOpenReport(item.id)}
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
                          onClick={() => handleNotify(item.id)}
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
                            onClick={() => handleResume(item.id)}
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
                    </div>
                  ) : item.status === 'failed' && item.total_chunks > 0 && (item.success_chunks ?? 0) !== item.total_chunks ? (
                    <button
                      onClick={() => handleResume(item.id)}
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
                <td style={{ verticalAlign: 'middle' }}>
                  {item.status === 'success' ? (() => {
                    const text = getOverviewText(item);
                    return (
                      <div style={{ color: '#1e293b', fontSize: '0.825rem', fontWeight: 600, overflow: 'hidden', textOverflow: 'ellipsis', display: '-webkit-box', WebkitLineClamp: 2, WebkitBoxOrient: 'vertical', lineHeight: '1.4' }} title={text}>
                        {text}
                      </div>
                    );
                  })() : item.status === 'failed' ? (
                    <div style={{ color: 'var(--danger-color)', fontSize: '0.825rem', fontStyle: 'italic' }}>
                      任务执行失败，AI 审计中断。
                    </div>
                  ) : (item.status === 'running' || item.status === 'cloning' || item.status === 'pre_processing' || item.status === 'analyzing' || item.status === 'post_processing') ? (
                    <div style={{ color: 'var(--primary-color)', fontSize: '0.825rem', display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                      <span className="spinner-mini" />
                      <span>正在分析代码库，AI 深入审计中...</span>
                    </div>
                  ) : item.status === 'queued' ? (
                    <div style={{ color: '#64748b', fontSize: '0.825rem', fontStyle: 'italic' }}>
                      任务已入队，等待可用运行实例...
                    </div>
                  ) : (
                    <span style={{ color: '#aaa', fontSize: '0.825rem' }}>-</span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* Pagination */}
      {totalPages > 0 && (
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginTop: '1rem', padding: '0.5rem', background: 'var(--card-bg)', borderRadius: '6px', border: '1px solid var(--border-color)' }}>
          <span style={{ fontSize: '0.875rem', color: 'var(--text-color)' }}>
            共 {totalItems} 条记录，当前第 {page} / {totalPages} 页
          </span>
          <div style={{ display: 'flex', gap: '0.5rem' }}>
            <button 
              className="btn" 
              disabled={page === 1} 
              onClick={() => setPage(page - 1)} 
              style={{ 
                background: page === 1 ? 'var(--bg-color)' : 'var(--card-bg)', 
                color: page === 1 ? '#94a3b8' : 'var(--text-color)', 
                border: '1px solid var(--border-color)',
                opacity: page === 1 ? 0.5 : 1,
                cursor: page === 1 ? 'not-allowed' : 'pointer'
              }}
            >
              上一页
            </button>
            <button 
              className="btn" 
              disabled={page >= totalPages} 
              onClick={() => setPage(page + 1)} 
              style={{ 
                background: page >= totalPages ? 'var(--bg-color)' : 'var(--card-bg)', 
                color: page >= totalPages ? '#94a3b8' : 'var(--text-color)', 
                border: '1px solid var(--border-color)',
                opacity: page >= totalPages ? 0.5 : 1,
                cursor: page >= totalPages ? 'not-allowed' : 'pointer'
              }}
            >
              下一页
            </button>
          </div>
        </div>
      )}

      {/* Sidebar detail viewer */}
      <ReportSidebar open={sidebarOpen} onClose={() => setSidebarOpen(false)} markdown={currentMarkdown} loading={loadingMarkdown} reportId={currentReportId} />

      <style>{`
        .spinner-mini {
          width: 14px;
          height: 14px;
          border: 2px solid rgba(37, 99, 235, 0.15);
          border-top-color: var(--primary-color);
          border-radius: 50%;
          animation: spin 0.8s linear infinite;
        }
        @keyframes spin {
          to { transform: rotate(360deg); }
        }
      `}</style>
    </div>
  );
}

export default TaskOverviewTab;
