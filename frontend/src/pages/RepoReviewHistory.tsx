import React, { useEffect, useState } from 'react';
import { useParams, useNavigate, useLocation } from 'react-router-dom';
import { useToast } from '../components/Toast';
import ReportSidebar from '../components/ReportSidebar';
import { appNavigatePath } from '../config';

function RepoReviewHistory() {
  const { repoId } = useParams<{ repoId: string }>();
  const navigate = useNavigate();
  const location = useLocation();
  const returnSearch = (location.state as any)?.returnSearch || '';
  const from = (location.state as any)?.from;
  const { showToast } = useToast();

  const [reviews, setReviews] = useState<any[]>([]);
  const [repoName, setRepoName] = useState<string>('');
  const [page, setPage] = useState(1);
  const [pageSize] = useState(15);
  const [totalItems, setTotalItems] = useState(0);
  const [totalPages, setTotalPages] = useState(0);

  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [currentMarkdown, setCurrentMarkdown] = useState('');
  const [loadingMarkdown, setLoadingMarkdown] = useState(false);
  const [currentReportId, setCurrentReportId] = useState<number | undefined>(undefined);

  useEffect(() => {
    fetchReviews();
  }, [repoId, page]);

  const fetchReviews = async () => {
    try {
      const params = new URLSearchParams({
        repo_id: repoId || '',
        page: page.toString(),
        pageSize: pageSize.toString(),
      });
      const res = await fetch(`/api/tasks?${params.toString()}`);
      if (res.ok) {
        const data = await res.json();
        setReviews(data.items || []);
        setTotalItems(data.total || 0);
        setTotalPages(data.totalPages || 0);
        // Get repo name from first result
        if (data.items && data.items.length > 0 && data.items[0].repo) {
          setRepoName(data.items[0].repo.name || '');
        }
      }
    } catch (err) {
      console.error('Failed to fetch reviews:', err);
    }
  };

  // If we don't have the repo name from reviews (e.g. no reviews yet), fetch repo info
  useEffect(() => {
    if (!repoName && repoId) {
      fetch('/api/repos?pageSize=10000')
        .then(res => res.json())
        .then(data => {
          const repos = data.items || data || [];
          const repo = repos.find((r: any) => r.id === Number(repoId));
          if (repo) setRepoName(repo.name || '');
        })
        .catch(console.error);
    }
  }, [repoId, repoName]);

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

  const handleResume = async (reportId: number) => {
    try {
      const res = await fetch(`/api/tasks/${reportId}/resume`, { method: 'POST' });
      if (res.ok) {
        showToast('恢复任务已入队，等待排队执行', 'success');
        fetchReviews();
      } else {
        const data = await res.json();
        showToast(`恢复失败: ${data.error || '未知错误'}`, 'error');
      }
    } catch {
      showToast('网络异常，恢复失败', 'error');
    }
  };

  const statusLabel = (status: string) => {
    switch (status) {
      case 'success': return { text: '完成', cls: 'success' };
      case 'failed': return { text: '失败', cls: 'danger' };
      case 'running': return { text: '执行中', cls: 'warning' };
      case 'queued': return { text: '排队中', cls: '' };
      default: return { text: status, cls: 'warning' };
    }
  };

  const shortName = repoName?.includes(':') ? repoName.split(':').pop() : repoName;

  return (
    <div>
      {/* Header with back button */}
      <div style={{ display: 'flex', alignItems: 'center', gap: '1rem', marginBottom: '1.5rem' }}>
        <button
          onClick={() => {
            if (from === 'scan-management') {
              navigate(appNavigatePath('/admin/scan/trigger'));
            } else if (from === 'workbench') {
              navigate(appNavigatePath('/workbench'));
            } else {
              navigate(appNavigatePath(`/reports${returnSearch ? '?' + returnSearch : ''}`));
            }
          }}
          style={{
            background: 'transparent', border: '1px solid var(--border-color)', borderRadius: '6px',
            cursor: 'pointer', padding: '0.4rem 0.75rem', display: 'flex', alignItems: 'center', gap: '0.4rem',
            color: 'var(--text-color)', fontSize: '0.875rem',
          }}
          onMouseEnter={e => e.currentTarget.style.borderColor = 'var(--primary-color)'}
          onMouseLeave={e => e.currentTarget.style.borderColor = 'var(--border-color)'}
        >
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <polyline points="15 18 9 12 15 6"></polyline>
          </svg>
          {from === 'scan-management' ? '返回手动触发' : from === 'workbench' ? '返回工作台' : '返回概览'}
        </button>
        <div style={{ display: 'flex', flexDirection: 'column' }}>
          <h2 style={{ margin: 0, fontSize: '1.1rem' }}>
            <span style={{ color: '#64748b', fontWeight: 400 }}>历史任务报告</span>
            <span style={{ margin: '0 0.5rem', color: '#cbd5e1' }}>|</span>
            <span title={repoName}>{shortName || `仓库 #${repoId}`}</span>
          </h2>
        </div>
      </div>

      {/* Reviews table */}
      <div className="card" style={{ padding: 0, overflow: 'hidden', fontSize: '0.875rem' }}>
        <table className="table">
          <thead>
            <tr>
              <th style={{ width: '60px' }}>报告 ID</th>
              <th>任务类型</th>
              <th>状态</th>
              <th>分片进度</th>
              <th>执行时间</th>
              <th>Base Commit</th>
              <th>Head Commit</th>
              <th>风险</th>
              <th>AI 摘要</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {reviews.length === 0 ? (
              <tr><td colSpan={10} style={{ textAlign: 'center', padding: '3rem', color: '#94a3b8' }}>暂无任务报告数据</td></tr>
            ) : reviews.map((r) => {
              const st = statusLabel(r.status);
              return (
                <tr key={r.id}>
                  <td style={{ color: '#64748b' }}>#{r.id}</td>
                  <td>
                    <span style={{ fontSize: '0.8rem', background: '#f1f5f9', padding: '0.2rem 0.5rem', borderRadius: '4px', color: '#475569' }}>
                      {r.task_type ? r.task_type.display_name : `类型 ${r.task_type_id}`}
                    </span>
                  </td>
                  <td>
                    <span className={`badge ${st.cls}`}>{st.text}</span>
                  </td>
                  <td>
                    {r.total_chunks > 0 ? (() => {
                      const success = r.success_chunks ?? 0;
                      const total = r.total_chunks;
                      const allSuccess = success === total;
                      return (
                        <span style={{
                          display: 'inline-block',
                          padding: '0.15rem 0.5rem',
                          borderRadius: '4px',
                          fontSize: '0.8rem',
                          fontWeight: 600,
                          background: allSuccess ? 'rgba(34, 197, 94, 0.12)' : 'rgba(245, 158, 11, 0.12)',
                          color: allSuccess ? '#16a34a' : '#d97706',
                          border: `1px solid ${allSuccess ? 'rgba(34, 197, 94, 0.25)' : 'rgba(245, 158, 11, 0.25)'}`,
                        }}>
                          {success}/{total}
                        </span>
                      );
                    })() : (
                      <span style={{ color: '#aaa', fontSize: '0.8rem' }}>-</span>
                    )}
                  </td>
                  <td style={{ color: '#64748b', whiteSpace: 'nowrap' }}>
                    {r.created_at ? new Date(r.created_at).toLocaleString() : '-'}
                  </td>
                  <td>
                    <code style={{ fontSize: '0.75rem', background: 'rgba(175,184,193,0.15)', padding: '0.15rem 0.35rem', borderRadius: '4px' }}>
                      {r.base_commit ? r.base_commit.substring(0, 8) : '-'}
                    </code>
                  </td>
                  <td>
                    <code style={{ fontSize: '0.75rem', background: 'rgba(175,184,193,0.15)', padding: '0.15rem 0.35rem', borderRadius: '4px' }}>
                      {r.head_commit ? r.head_commit.substring(0, 8) : '-'}
                    </code>
                  </td>
                  <td>
                    {r.status === 'success' ? (
                      <span style={{ fontWeight: 700, fontSize: '1rem', color: r.score >= 20 ? '#ef4444' : r.score >= 10 ? '#f59e0b' : '#22c55e' }}>
                        {r.score ?? 0}
                      </span>
                    ) : (
                      <span style={{ color: '#aaa' }}>-</span>
                    )}
                  </td>
                  <td>
                    {r.ai_summary ? (
                      <span
                        style={{ fontSize: '0.8rem', color: '#475569', display: '-webkit-box', WebkitLineClamp: 2, WebkitBoxOrient: 'vertical', overflow: 'hidden', maxWidth: '200px' }}
                        title={r.ai_summary}
                      >
                        {r.ai_summary}
                      </span>
                    ) : (
                      <span style={{ color: '#aaa', fontSize: '0.8rem' }}>-</span>
                    )}
                  </td>
                  <td>
                    {r.status === 'success' && (
                      <div style={{ display: 'flex', gap: '0.25rem' }}>
                        <button
                          onClick={() => handleOpenReport(r.id)}
                          style={{ background: 'transparent', border: 'none', cursor: 'pointer', display: 'flex', alignItems: 'center', padding: '0.25rem', borderRadius: '4px', color: 'var(--primary-color)' }}
                          title="查看详细报告"
                          onMouseEnter={e => e.currentTarget.style.backgroundColor = 'rgba(37, 99, 235, 0.1)'}
                          onMouseLeave={e => e.currentTarget.style.backgroundColor = 'transparent'}
                        >
                          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                            <circle cx="11" cy="11" r="8"></circle>
                            <line x1="21" y1="21" x2="16.65" y2="16.65"></line>
                          </svg>
                        </button>
                        <button
                          onClick={() => handleNotify(r.id)}
                          style={{ background: 'transparent', border: 'none', cursor: 'pointer', display: 'flex', alignItems: 'center', padding: '0.25rem', borderRadius: '4px', color: '#10b981' }}
                          title="发送报告通知"
                          onMouseEnter={e => e.currentTarget.style.backgroundColor = 'rgba(16, 185, 129, 0.1)'}
                          onMouseLeave={e => e.currentTarget.style.backgroundColor = 'transparent'}
                        >
                          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                            <rect x="2" y="4" width="20" height="16" rx="2"></rect>
                            <polyline points="2,4 12,13 22,4"></polyline>
                          </svg>
                        </button>
                        {r.total_chunks > 0 && (r.success_chunks ?? 0) !== r.total_chunks && (
                          <button
                            onClick={() => handleResume(r.id)}
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
                    {r.status === 'failed' && r.total_chunks > 0 && (r.success_chunks ?? 0) !== r.total_chunks && (
                      <button
                        onClick={() => handleResume(r.id)}
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
                    )}
                  </td>
                </tr>
              );
            })}
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
                color: page === 1 ? 'var(--text-secondary)' : 'var(--text-color)',
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
                color: page >= totalPages ? 'var(--text-secondary)' : 'var(--text-color)',
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

      <ReportSidebar open={sidebarOpen} onClose={() => setSidebarOpen(false)} markdown={currentMarkdown} loading={loadingMarkdown} reportId={currentReportId} />
    </div>
  );
}

export default RepoReviewHistory;
