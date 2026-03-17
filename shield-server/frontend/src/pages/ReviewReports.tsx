import React, { useEffect, useState } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';

function ReviewReports() {
  const [reviews, setReviews] = useState<any[]>([]);
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [currentMarkdown, setCurrentMarkdown] = useState<string>('');
  const [loadingMarkdown, setLoadingMarkdown] = useState(false);

  const fetchReviews = () => {
    fetch('/api/reviews')
      .then(res => res.json())
      .then(data => setReviews(data || []))
      .catch(console.error);
  };

  const handleNotify = async (reportId: number) => {
    try {
      const res = await fetch(`/api/reviews/${reportId}/notify`, {
        method: 'POST',
      });
      if (res.ok) {
        alert('已经成功发出通知！');
      } else {
        const data = await res.json();
        alert(`发送通知失败: ${data.error}`);
      }
    } catch (err) {
      console.error('Failed to manually notify:', err);
      alert('网络异常，发送失败');
    }
  };

  const handleOpenReport = async (reportId: number) => {
    setSidebarOpen(true);
    setLoadingMarkdown(true);
    setCurrentMarkdown('');
    try {
      const res = await fetch(`/api/reviews/${reportId}/report`);
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

  useEffect(() => {
    fetchReviews();
    const interval = setInterval(fetchReviews, 3000); // Poll every 3 seconds
    return () => clearInterval(interval);
  }, []);

  return (
    <div>
      <div style={{ display: 'grid', gap: '1rem' }}>
        {reviews.length === 0 ? (
          <div className="card" style={{ textAlign: 'center', color: '#94a3b8', padding: '3rem' }}>暂未生成任何检视报告</div>
        ) : reviews.map(review => (
          <div key={review.id} className="card">
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: '1rem' }}>
              <div>
                <h3 style={{ margin: '0 0 0.25rem 0', color: 'var(--text-color)' }}>
                  {review.repo?.name || '未知代码仓'}
                </h3>
                <div style={{ color: '#64748b', fontSize: '0.8rem', margin: '0 0 0.5rem 0' }}>
                  <a href={review.repo?.url} target="_blank" rel="noreferrer" style={{ color: 'var(--primary-color)', textDecoration: 'none' }}>{review.repo?.url}</a>
                </div>
                <div style={{ color: '#94a3b8', fontSize: '0.875rem' }}>
                  对比区段: <span style={{ fontFamily: 'monospace', background: 'var(--bg-color)', padding: '0.2rem 0.4rem', borderRadius: '4px' }}>{review.base_commit}...{review.head_commit}</span>
                </div>
              </div>
              <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-end', gap: '0.5rem' }}>
                <span className={`badge ${review.status === 'success' ? 'success' : (review.status === 'failed' ? 'danger' : review.status === 'skipped' ? 'info' : review.status === 'queued' ? '' : 'warning')}`}>
                  {review.status === 'success' ? '已完成' : review.status === 'failed' ? '任务失败' : review.status === 'skipped' ? '无代码变更' : review.status === 'queued' ? '排队中...' : review.status === 'running' ? '执行中...' : review.status === 'pending' ? '准备中...' : review.status}
                </span>
                
                {review.status !== 'queued' && review.status !== 'pending' && review.clone_status && (
                  <span style={{ fontSize: '0.75rem', padding: '0.15rem 0.4rem', borderRadius: '4px', background: review.clone_status === 'success' ? '#dcfce7' : review.clone_status === 'failed' ? '#fee2e2' : '#f8fafc', color: review.clone_status === 'success' ? '#166534' : review.clone_status === 'failed' ? '#991b1b' : '#64748b', border: '1px solid #e2e8f0' }}>
                    Git Clone: {review.clone_status}
                  </span>
                )}
              </div>
            </div>
            
            {review.status === 'queued' && (
              <div style={{ color: '#64748b', fontSize: '0.875rem', marginBottom: '1rem' }}>
                <span className="spinner"></span> 任务正在排队等待执行...
              </div>
            )}
            
            {review.status === 'running' && (
              <div style={{ color: '#64748b', fontSize: '0.875rem', marginBottom: '1rem' }}>
                <span className="spinner"></span> AI正在后台检视代码，请稍候...
              </div>
            )}

            {review.status === 'skipped' && review.ai_summary && (
              <div style={{ padding: '1rem 1.5rem', background: '#f0f9ff', borderRadius: '6px', marginBottom: '1rem', border: '1px solid #bae6fd', color: '#0369a1', fontSize: '0.875rem' }}>
                ℹ️ {review.ai_summary}
              </div>
            )}

            {review.status === 'success' && review.ai_summary && (
              <div style={{ padding: '1.5rem', background: '#f8fafc', borderRadius: '6px', marginBottom: '1rem', border: '1px solid #e2e8f0' }}>
                <div className="markdown-body" style={{ background: 'transparent', fontSize: '0.9rem' }}>
                  <ReactMarkdown remarkPlugins={[remarkGfm]}>
                    {review.ai_summary}
                  </ReactMarkdown>
                </div>
              </div>
            )}

            {review.status === 'success' && (
              <div style={{ marginTop: '1rem', display: 'flex', justifyContent: 'flex-end', gap: '0.75rem' }}>
                <button 
                  className="btn" 
                  onClick={() => handleNotify(review.id)}
                  style={{ background: 'transparent', color: 'var(--primary-color)', border: '1px solid var(--primary-color)' }}
                >
                  通知责任人
                </button>
                <button 
                  className="btn" 
                  onClick={() => handleOpenReport(review.id)}
                  style={{ background: 'var(--success-color)', borderColor: 'var(--success-color)' }}
                >
                  查看报告
                </button>
              </div>
            )}

          </div>
        ))}
      </div>
      
      {/* Markdown Sidebar Drawer */}
      <div 
        style={{
          position: 'fixed', top: 0, right: sidebarOpen ? 0 : '-50vw', width: '50vw', height: '100vh',
          background: 'var(--bg-color)', boxShadow: '-4px 0 15px rgba(0,0,0,0.1)',
          transition: 'right 0.3s ease-in-out', zIndex: 1000, display: 'flex', flexDirection: 'column'
        }}
      >
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
        .spinner {
          display: inline-block;
          width: 12px;
          height: 12px;
          border: 2px solid rgba(100, 116, 139, 0.3);
          border-radius: 50%;
          border-top-color: var(--primary-color);
          animation: spin 1s ease-in-out infinite;
          vertical-align: middle;
          margin-right: 5px;
        }
        @keyframes spin {
          to { transform: rotate(360deg); }
        }
        
        /* Basic rendering styles for ReactMarkdown to look appealing without a huge reset framework */
        .markdown-body {
          font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif;
          line-height: 1.6;
          color: #24292f;
        }
        .markdown-body h1, .markdown-body h2, .markdown-body h3, .markdown-body h4 {
          margin-top: 24px;
          margin-bottom: 16px;
          font-weight: 600;
          line-height: 1.25;
        }
        .markdown-body h2 {
          border-bottom: 1px solid #d0d7de;
          padding-bottom: .3em;
        }
        .markdown-body blockquote {
          padding: 0 1em;
          color: #57606a;
          border-left: .25em solid #d0d7de;
          margin: 0 0 16px 0;
        }
        .markdown-body pre {
          padding: 16px;
          overflow: auto;
          font-size: 85%;
          line-height: 1.45;
          background-color: #f6f8fa;
          border-radius: 6px;
        }
        .markdown-body code {
          padding: .2em .4em;
          margin: 0;
          font-size: 85%;
          background-color: rgba(175, 184, 193, 0.2);
          border-radius: 6px;
        }
        .markdown-body pre > code {
          padding: 0;
          margin: 0;
          font-size: 100%;
          background-color: transparent;
          border: 0;
        }
        .markdown-body ul, .markdown-body ol {
          margin-top: 0;
          margin-bottom: 16px;
          padding-left: 2em;
        }
      `}</style>
    </div>
  );
}

export default ReviewReports;
