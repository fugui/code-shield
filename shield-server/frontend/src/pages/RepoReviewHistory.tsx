import React, { useEffect, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { useToast } from '../components/Toast';

function RepoReviewHistory() {
  const { repoId } = useParams<{ repoId: string }>();
  const navigate = useNavigate();
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
      fetch('/api/repos')
        .then(res => res.json())
        .then((repos: any[]) => {
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
          onClick={() => navigate('/tasks/overview')}
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
          返回概览
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
              <th>状态</th>
              <th>执行时间</th>
              <th>Base Commit</th>
              <th>Head Commit</th>
              <th>评分</th>
              <th>AI 摘要</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {reviews.length === 0 ? (
              <tr><td colSpan={8} style={{ textAlign: 'center', padding: '3rem', color: '#94a3b8' }}>暂无任务报告数据</td></tr>
            ) : reviews.map((r) => {
              const st = statusLabel(r.status);
              return (
                <tr key={r.id}>
                  <td style={{ color: '#64748b' }}>#{r.id}</td>
                  <td>
                    <span className={`badge ${st.cls}`}>{st.text}</span>
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
                      </div>
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
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginTop: '1rem', padding: '0.5rem', background: 'white', borderRadius: '6px', border: '1px solid var(--border-color)' }}>
          <span style={{ fontSize: '0.875rem', color: 'var(--text-color)' }}>
            共 {totalItems} 条记录，当前第 {page} / {totalPages} 页
          </span>
          <div style={{ display: 'flex', gap: '0.5rem' }}>
            <button
              className="btn"
              disabled={page === 1}
              onClick={() => setPage(page - 1)}
              style={{ background: page === 1 ? '#f1f5f9' : 'white', color: page === 1 ? '#94a3b8' : 'var(--text-color)', border: '1px solid var(--border-color)' }}
            >
              上一页
            </button>
            <button
              className="btn"
              disabled={page >= totalPages}
              onClick={() => setPage(page + 1)}
              style={{ background: page >= totalPages ? '#f1f5f9' : 'white', color: page >= totalPages ? '#94a3b8' : 'var(--text-color)', border: '1px solid var(--border-color)' }}
            >
              下一页
            </button>
          </div>
        </div>
      )}

      {/* Markdown Sidebar Drawer */}
      <div
        style={{
          position: 'fixed', top: 0, right: sidebarOpen ? 0 : '-50vw', width: '50vw', height: '100vh',
          background: 'var(--bg-color)', boxShadow: '-4px 0 15px rgba(0,0,0,0.1)',
          transition: 'right 0.3s ease-in-out', zIndex: 1000, display: 'flex', flexDirection: 'column'
        }}
      >
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
        <div
          onClick={() => setSidebarOpen(false)}
          style={{ position: 'fixed', top: 0, left: 0, right: 0, bottom: 0, background: 'rgba(0,0,0,0.5)', zIndex: 999 }}
        />
      )}

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

export default RepoReviewHistory;
