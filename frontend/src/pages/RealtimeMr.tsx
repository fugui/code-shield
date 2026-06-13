import React, { useEffect, useState, useCallback } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useToast } from '../components/Toast';

// Premium SVG Icons
const RefreshIcon = ({ className = "" }) => (
  <svg className={className} width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
    <path d="M21.5 2v6h-6M21.34 15.57a10 10 0 1 1-.57-8.38l5.67-5.67"/>
  </svg>
);

const ExternalLinkIcon = () => (
  <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
    <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/>
    <polyline points="15 3 21 3 21 9"/>
    <line x1="10" y1="14" x2="21" y2="3"/>
  </svg>
);

const CodeIcon = () => (
  <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <polyline points="16 18 22 12 16 6"/>
    <polyline points="8 6 2 12 8 18"/>
  </svg>
);

const CloseIcon = () => (
  <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5">
    <line x1="18" y1="6" x2="6" y2="18"/>
    <line x1="6" y1="6" x2="18" y2="18"/>
  </svg>
);

interface MrEvent {
  id: number;
  mr_id: number;
  mr_num: number;
  repo_name: string;
  repo_url: string;
  title: string;
  source_branch: string;
  target_branch: string;
  author: string;
  action: string;
  mr_url: string;
  payload: string;
  is_proto_change: boolean;
  interface_files: string;
  created_at: string;
}

export default function RealtimeMr() {
  const { showToast } = useToast();
  const [searchParams, setSearchParams] = useSearchParams();
  const page = parseInt(searchParams.get('page') || '1', 10) || 1;
  const repoFilter = searchParams.get('repo') || '';
  const authorFilter = searchParams.get('author') || '';

  const [items, setItems] = useState<MrEvent[]>([]);
  const [totalItems, setTotalItems] = useState(0);
  const [totalPages, setTotalPages] = useState(1);
  const [loading, setLoading] = useState(false);

  // Modal detail view state
  const [viewingEvent, setViewingEvent] = useState<MrEvent | null>(null);

  const fetchEvents = useCallback(() => {
    setLoading(true);
    const params = new URLSearchParams({
      page: page.toString(),
      pageSize: '15',
    });
    if (repoFilter) params.append('repo', repoFilter);
    if (authorFilter) params.append('author', authorFilter);

    fetch(`/api/mr?${params.toString()}`)
      .then(res => {
        if (!res.ok) throw new Error('拉取 MR 数据失败');
        return res.json();
      })
      .then(data => {
        setItems(data.items || []);
        setTotalItems(data.total || 0);
        setTotalPages(data.totalPages || 1);
      })
      .catch(err => {
        console.error('Failed to fetch MR events:', err);
        showToast(err.message || '获取看护数据失败', 'error');
      })
      .finally(() => {
        setLoading(false);
      });
  }, [page, repoFilter, authorFilter, showToast]);

  useEffect(() => {
    fetchEvents();
  }, [fetchEvents]);

  const handleFilterChange = (key: string, val: string) => {
    setSearchParams(prev => {
      const next = new URLSearchParams(prev);
      if (val) {
        next.set(key, val);
      } else {
        next.delete(key);
      }
      next.delete('page');
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

  const getActionBadgeStyle = (action: string) => {
    let bg = 'rgba(100, 116, 139, 0.08)';
    let color = '#64748b';
    let border = '1px solid rgba(100, 116, 139, 0.15)';
    const act = action.toLowerCase();
    
    if (act === 'open' || act === 'create') {
      bg = 'rgba(59, 130, 246, 0.08)';
      color = '#3b82f6';
      border = '1px solid rgba(59, 130, 246, 0.2)';
    } else if (act === 'merge' || act === 'merged') {
      bg = 'rgba(16, 185, 129, 0.08)';
      color = '#10b981';
      border = '1px solid rgba(16, 185, 129, 0.2)';
    } else if (act === 'close' || act === 'closed') {
      bg = 'rgba(239, 68, 68, 0.08)';
      color = '#ef4444';
      border = '1px solid rgba(239, 68, 68, 0.2)';
    } else if (act === 'update' || act === 'updated') {
      bg = 'rgba(245, 158, 11, 0.08)';
      color = '#f59e0b';
      border = '1px solid rgba(245, 158, 11, 0.2)';
    }
    return {
      display: 'inline-flex',
      alignItems: 'center',
      padding: '0.2rem 0.55rem',
      borderRadius: '6px',
      fontSize: '0.75rem',
      fontWeight: 600,
      backgroundColor: bg,
      color: color,
      border: border,
    };
  };

  const formatPayload = (payload: string) => {
    try {
      const obj = JSON.parse(payload);
      return JSON.stringify(obj, null, 2);
    } catch (e) {
      return payload;
    }
  };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '1.5rem', width: '100%' }}>
      {/* Dynamic Keyframes Injection */}
      <style>{`
        @keyframes pulse-dot {
          0% { transform: scale(0.95); box-shadow: 0 0 0 0 rgba(16, 185, 129, 0.7); }
          70% { transform: scale(1); box-shadow: 0 0 0 6px rgba(16, 185, 129, 0); }
          100% { transform: scale(0.95); box-shadow: 0 0 0 0 rgba(16, 185, 129, 0); }
        }
        @keyframes spin-custom {
          to { transform: rotate(360deg); }
        }
        .filter-input {
          padding: 0.55rem 0.85rem;
          border-radius: 8px;
          border: 1px solid var(--border-color);
          outline: none;
          font-size: 0.85rem;
          background: var(--bg-color);
          color: var(--text-color);
          transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);
        }
        .filter-input:focus {
          border-color: var(--primary-color);
          box-shadow: 0 0 0 3px rgba(37, 99, 235, 0.15);
          transform: translateY(-1px);
        }
        .table-row-hover {
          transition: background-color 0.2s, transform 0.2s;
        }
        .table-row-hover:hover {
          background-color: rgba(37, 99, 235, 0.02) !important;
        }
        .live-badge {
          display: inline-flex;
          align-items: center;
          gap: 0.45rem;
          background: rgba(16, 185, 129, 0.08);
          color: #10b981;
          padding: 0.3rem 0.75rem;
          border-radius: 9999px;
          font-size: 0.75rem;
          font-weight: 600;
          border: 1px solid rgba(16, 185, 129, 0.2);
        }
        .live-dot {
          width: 8px;
          height: 8px;
          background-color: #10b981;
          border-radius: 50%;
          animation: pulse-dot 2s infinite;
        }
        .spin-anim {
          animation: spin-custom 0.8s linear infinite;
        }
      `}</style>

      {/* Header Info Panel */}
      <div style={{
        background: 'linear-gradient(135deg, rgba(37, 99, 235, 0.03) 0%, rgba(37, 99, 235, 0.07) 100%)',
        border: '1px solid rgba(37, 99, 235, 0.15)',
        padding: '1.5rem',
        borderRadius: '12px',
        display: 'flex',
        justifyContent: 'space-between',
        alignItems: 'center',
        flexWrap: 'wrap',
        gap: '1rem'
      }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.4rem' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
            <h2 style={{ margin: 0, fontSize: '1.25rem', fontWeight: 700, color: 'var(--text-color)' }}>Merge Request 实时事件流</h2>
            <span className="live-badge">
              <span className="live-dot" />
              LIVE 实时监听中
            </span>
          </div>
          <p style={{ margin: 0, fontSize: '0.85rem', color: '#64748b' }}>
            系统已自动对接研发托管平台 Webhook，实现对合并请求的秒级捕获与自动化质量卡口校验。
          </p>
        </div>
      </div>

      {/* Stats & Filters Card */}
      <div style={{
        display: 'flex',
        flexWrap: 'wrap',
        gap: '1rem',
        background: 'var(--card-bg)',
        padding: '1.25rem',
        borderRadius: '12px',
        border: '1px solid var(--border-color)',
        alignItems: 'center',
        justifyContent: 'space-between'
      }}>
        <div style={{ display: 'flex', gap: '1rem', flexWrap: 'wrap', alignItems: 'center' }}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.35rem', minWidth: '180px' }}>
            <label style={{ fontSize: '0.75rem', fontWeight: 600, color: '#64748b' }}>代码仓名称</label>
            <input
              type="text"
              className="filter-input"
              placeholder="搜索代码仓..."
              value={repoFilter}
              onChange={e => handleFilterChange('repo', e.target.value)}
            />
          </div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.35rem', minWidth: '180px' }}>
            <label style={{ fontSize: '0.75rem', fontWeight: 600, color: '#64748b' }}>提交人 / 责任人</label>
            <input
              type="text"
              className="filter-input"
              placeholder="搜索推送作者..."
              value={authorFilter}
              onChange={e => handleFilterChange('author', e.target.value)}
            />
          </div>
          {(repoFilter || authorFilter) && (
            <button
              onClick={() => setSearchParams(new URLSearchParams(), { replace: true })}
              style={{
                background: 'transparent',
                border: '1px solid var(--border-color)',
                padding: '0.5rem 1rem',
                borderRadius: '8px',
                fontSize: '0.85rem',
                color: '#64748b',
                cursor: 'pointer',
                marginTop: '1.2rem',
                display: 'inline-flex',
                alignItems: 'center',
                gap: '0.4rem',
                transition: 'all 0.2s'
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
              <RefreshIcon />
              清除筛选
            </button>
          )}
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: '1rem' }}>
          <span style={{ fontSize: '0.85rem', color: '#64748b' }}>
            已接收 <strong>{totalItems}</strong> 个实时推送事件
          </span>
          <button
            onClick={fetchEvents}
            disabled={loading}
            style={{
              background: 'var(--primary-color)',
              color: 'white',
              border: 'none',
              padding: '0.55rem 1.1rem',
              borderRadius: '8px',
              fontSize: '0.85rem',
              fontWeight: 600,
              cursor: 'pointer',
              display: 'inline-flex',
              alignItems: 'center',
              gap: '0.45rem',
              transition: 'opacity 0.2s',
              boxShadow: '0 4px 6px -1px rgba(37, 99, 235, 0.2)'
            }}
            onMouseEnter={e => e.currentTarget.style.opacity = '0.9'}
            onMouseLeave={e => e.currentTarget.style.opacity = '1'}
          >
            <RefreshIcon className={loading ? "spin-anim" : ""} />
            手动刷新
          </button>
        </div>
      </div>

      {/* Table Card */}
      <div className="card" style={{ padding: 0, overflow: 'hidden', borderRadius: '12px' }}>
        {loading && items.length === 0 ? (
          <div style={{ padding: '6rem', textAlign: 'center', color: '#64748b' }}>
            <div style={{ width: '36px', height: '36px', borderRadius: '50%', border: '3px solid rgba(37,99,246,0.15)', borderTopColor: 'var(--primary-color)', animation: 'spin-custom 0.8s linear infinite', margin: '0 auto 1.25rem' }} />
            数据加载中，请稍候...
          </div>
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th style={{ width: '70px', paddingLeft: '1.5rem' }}>ID</th>
                <th style={{ width: '180px' }}>代码仓</th>
                <th>Merge Request 标题</th>
                <th style={{ width: '120px' }}>变更类型</th>
                <th style={{ width: '140px' }}>源分支</th>
                <th style={{ width: '140px' }}>目标分支</th>
                <th style={{ width: '120px' }}>提交人</th>
                <th style={{ width: '110px' }}>状态/动作</th>
                <th style={{ width: '160px' }}>接收时间</th>
                <th style={{ width: '70px', textAlign: 'center', paddingRight: '1.5rem' }}>Payload</th>
              </tr>
            </thead>
            <tbody>
              {items.length === 0 ? (
                <tr>
                  <td colSpan={10} style={{ textAlign: 'center', padding: '5rem', color: '#94a3b8' }}>
                    暂无 Merge Request 推送事件记录。
                  </td>
                </tr>
              ) : (
                items.map((item, idx) => (
                  <tr key={item.id} className="table-row-hover">
                    <td style={{ paddingLeft: '1.5rem', color: '#94a3b8', fontWeight: 500 }}>
                      #{(page - 1) * 15 + idx + 1}
                    </td>
                    <td style={{ fontWeight: 600 }}>
                      {item.repo_url ? (
                        <a
                          href={item.repo_url}
                          target="_blank"
                          rel="noreferrer"
                          style={{ color: 'var(--primary-color)', textDecoration: 'none', display: 'inline-flex', alignItems: 'center', gap: '0.25rem' }}
                          onMouseEnter={e => e.currentTarget.style.textDecoration = 'underline'}
                          onMouseLeave={e => e.currentTarget.style.textDecoration = 'none'}
                          title={item.repo_url}
                        >
                          <span style={{ maxWidth: '150px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{item.repo_name}</span>
                          <ExternalLinkIcon />
                        </a>
                      ) : (
                        item.repo_name
                      )}
                    </td>
                    <td>
                      {item.mr_url ? (
                        <a
                          href={item.mr_url}
                          target="_blank"
                          rel="noreferrer"
                          style={{ color: 'var(--text-color)', textDecoration: 'none', fontWeight: 600, display: 'inline-flex', alignItems: 'center', gap: '0.3rem' }}
                          onMouseEnter={e => e.currentTarget.style.color = 'var(--primary-color)'}
                          onMouseLeave={e => e.currentTarget.style.color = 'var(--text-color)'}
                          title="在托管平台查看合并请求"
                        >
                          <span style={{ maxWidth: '360px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                            {item.title || `合并请求 #${item.mr_num}`}
                          </span>
                          <ExternalLinkIcon />
                        </a>
                      ) : (
                        <span style={{ fontWeight: 600 }}>{item.title || `合并请求 #${item.mr_num}`}</span>
                      )}
                    </td>
                    <td>
                      <div style={{ display: 'flex', flexDirection: 'column', gap: '0.25rem', alignItems: 'flex-start' }}>
                        {item.is_proto_change ? (
                          <span style={{
                            display: 'inline-flex',
                            alignItems: 'center',
                            padding: '0.15rem 0.45rem',
                            borderRadius: '4px',
                            fontSize: '0.7rem',
                            fontWeight: 700,
                            backgroundColor: 'rgba(16, 185, 129, 0.12)',
                            color: '#10b981',
                            border: '1px solid rgba(16, 185, 129, 0.25)',
                            boxShadow: '0 0 8px rgba(16, 185, 129, 0.15)'
                          }}>
                            ⚡ 接口变更
                          </span>
                        ) : (
                          <span style={{
                            display: 'inline-flex',
                            alignItems: 'center',
                            padding: '0.15rem 0.45rem',
                            borderRadius: '4px',
                            fontSize: '0.7rem',
                            fontWeight: 500,
                            backgroundColor: 'rgba(241, 245, 249, 0.05)',
                            color: 'var(--text-secondary)',
                            border: '1px solid var(--border-color)'
                          }}>
                            普通代码
                          </span>
                        )}
                        {item.is_proto_change && item.interface_files && (() => {
                          try {
                            const files = JSON.parse(item.interface_files);
                            if (files && files.length > 0) {
                              return (
                                <span
                                  style={{
                                    fontSize: '0.65rem',
                                    color: '#10b981',
                                    fontFamily: 'monospace',
                                    maxWidth: '120px',
                                    overflow: 'hidden',
                                    textOverflow: 'ellipsis',
                                    whiteSpace: 'nowrap',
                                    cursor: 'help',
                                    background: 'rgba(16, 185, 129, 0.05)',
                                    padding: '1px 3px',
                                    borderRadius: '3px'
                                  }}
                                  title={`变更文件列表：\n${files.join('\n')}`}
                                >
                                  {files.length === 1 ? files[0] : `${files[0]} 等 ${files.length} 个文件`}
                                </span>
                              );
                            }
                          } catch (e) {}
                          return null;
                        })()}
                      </div>
                    </td>
                    <td>
                      <span style={{ fontFamily: 'monospace', background: 'var(--bg-color)', padding: '0.15rem 0.4rem', borderRadius: '4px', border: '1px solid var(--border-color)', fontSize: '0.75rem' }}>{item.source_branch}</span>
                    </td>
                    <td>
                      <span style={{ fontFamily: 'monospace', background: 'var(--bg-color)', padding: '0.15rem 0.4rem', borderRadius: '4px', border: '1px solid var(--border-color)', fontSize: '0.75rem' }}>{item.target_branch}</span>
                    </td>
                    <td style={{ color: 'var(--text-color)', fontWeight: 500 }}>{item.author}</td>
                    <td>
                      <span style={getActionBadgeStyle(item.action)}>
                        {item.action.toUpperCase()}
                      </span>
                    </td>
                    <td style={{ color: '#64748b', fontSize: '0.8rem' }}>
                      {item.created_at ? item.created_at.replace('T', ' ').substring(0, 19) : '-'}
                    </td>
                    <td style={{ textAlign: 'center', paddingRight: '1.5rem' }}>
                      <button
                        onClick={() => setViewingEvent(item)}
                        style={{
                          background: 'transparent',
                          border: 'none',
                          cursor: 'pointer',
                          padding: '0.4rem',
                          borderRadius: '6px',
                          color: 'var(--primary-color)',
                          display: 'inline-flex',
                          alignItems: 'center',
                          justifyContent: 'center',
                          transition: 'background-color 0.2s'
                        }}
                        title="查看原始 JSON"
                        onMouseEnter={e => e.currentTarget.style.backgroundColor = 'rgba(37, 99, 235, 0.08)'}
                        onMouseLeave={e => e.currentTarget.style.backgroundColor = 'transparent'}
                      >
                        <CodeIcon />
                      </button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        )}
      </div>

      {/* Pagination Footer */}
      {totalPages > 1 && (
        <div style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          marginTop: '0.5rem',
          padding: '0.75rem 1.25rem',
          background: 'var(--card-bg)',
          borderRadius: '12px',
          border: '1px solid var(--border-color)'
        }}>
          <span style={{ fontSize: '0.85rem', color: 'var(--text-color)' }}>
            当前第 <strong>{page}</strong> / {totalPages} 页
          </span>
          <div style={{ display: 'flex', gap: '0.5rem' }}>
            <button
              disabled={page === 1}
              onClick={() => setPage(page - 1)}
              style={{
                padding: '0.4rem 0.9rem',
                borderRadius: '6px',
                border: '1px solid var(--border-color)',
                background: page === 1 ? 'var(--bg-color)' : 'var(--card-bg)',
                color: page === 1 ? '#94a3b8' : 'var(--text-color)',
                cursor: page === 1 ? 'not-allowed' : 'pointer',
                fontSize: '0.825rem',
                transition: 'all 0.2s'
              }}
            >
              上一页
            </button>
            <button
              disabled={page >= totalPages}
              onClick={() => setPage(page + 1)}
              style={{
                padding: '0.4rem 0.9rem',
                borderRadius: '6px',
                border: '1px solid var(--border-color)',
                background: page >= totalPages ? 'var(--bg-color)' : 'var(--card-bg)',
                color: page >= totalPages ? '#94a3b8' : 'var(--text-color)',
                cursor: page >= totalPages ? 'not-allowed' : 'pointer',
                fontSize: '0.825rem',
                transition: 'all 0.2s'
              }}
            >
              下一页
            </button>
          </div>
        </div>
      )}

      {/* Modal Detail Payload View */}
      {viewingEvent !== null && (
        <div style={{
          position: 'fixed',
          top: 0,
          left: 0,
          width: '100vw',
          height: '100vh',
          background: 'rgba(15, 23, 42, 0.4)',
          backdropFilter: 'blur(8px)',
          WebkitBackdropFilter: 'blur(8px)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          zIndex: 9999
        }}>
          <div style={{
            width: '800px',
            maxWidth: '90vw',
            maxHeight: '82vh',
            display: 'flex',
            flexDirection: 'column',
            background: 'var(--card-bg)',
            border: '1px solid var(--border-color)',
            borderRadius: '16px',
            boxShadow: '0 25px 50px -12px rgba(0, 0, 0, 0.25)',
            overflow: 'hidden'
          }}>
            {/* Modal Header */}
            <div style={{
              padding: '1.25rem 1.5rem',
              borderBottom: '1px solid var(--border-color)',
              display: 'flex',
              justifyContent: 'space-between',
              alignItems: 'center',
              background: 'var(--card-bg)'
            }}>
              <div style={{ display: 'flex', flexDirection: 'column', gap: '0.15rem' }}>
                <h3 style={{ margin: 0, fontSize: '1.05rem', fontWeight: 700, color: 'var(--text-color)' }}>
                  Webhook 原始 Payload 数据
                </h3>
                <span style={{ fontSize: '0.72rem', color: '#64748b' }}>
                  事件ID: {viewingEvent.id} | MR序号: #{viewingEvent.mr_num}
                </span>
              </div>
              <button
                onClick={() => setViewingEvent(null)}
                style={{
                  background: 'transparent',
                  border: 'none',
                  cursor: 'pointer',
                  padding: '0.25rem',
                  color: '#64748b',
                  borderRadius: '6px',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  transition: 'background-color 0.2s'
                }}
                onMouseEnter={e => e.currentTarget.style.backgroundColor = 'rgba(0,0,0,0.05)'}
                onMouseLeave={e => e.currentTarget.style.backgroundColor = 'transparent'}
              >
                <CloseIcon />
              </button>
            </div>

            {/* Modal Body */}
            <div style={{ flex: 1, overflow: 'auto', padding: '1.5rem', background: '#0f172a' }}>
              {viewingEvent.is_proto_change && viewingEvent.interface_files && (() => {
                try {
                  const files = JSON.parse(viewingEvent.interface_files);
                  if (files && files.length > 0) {
                    return (
                      <div style={{
                        marginBottom: '1.25rem',
                        padding: '0.85rem 1.25rem',
                        borderRadius: '8px',
                        background: 'rgba(16, 185, 129, 0.08)',
                        border: '1px solid rgba(16, 185, 129, 0.2)',
                        color: '#10b981',
                        fontSize: '0.825rem'
                      }}>
                        <strong style={{ display: 'block', marginBottom: '0.5rem', color: '#10b981', fontSize: '0.85rem' }}>
                          ⚡ 接口修改所涉文件：
                        </strong>
                        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.35rem', fontFamily: 'monospace', fontSize: '0.78rem' }}>
                          {files.map((f: string, idx: number) => (
                            <div key={idx} style={{ paddingLeft: '0.6rem', borderLeft: '2px solid #10b981' }}>{f}</div>
                          ))}
                        </div>
                      </div>
                    );
                  }
                } catch(e) {}
                return null;
              })()}
              <pre style={{ margin: 0, color: '#38bdf8', fontSize: '0.8rem', fontFamily: "monospace", textAlign: 'left', lineHeight: 1.45 }}>
                <code>{formatPayload(viewingEvent.payload)}</code>
              </pre>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
