import React, { useEffect, useState, useCallback, useRef } from 'react';
import { createPortal } from 'react-dom';
import { useToast } from '../components/Toast';

interface Finding {
  id: number;
  task_report_id: number;
  task_type_id: number;
  repo_id: number;
  severity: string;
  category: string;
  file_path: string;
  line_number: string;
  code_snippet: string;
  title: string;
  detail: string;
  suggestion: string;
  status: string;
  assignee_id: string;
  feedback: string;
  feedback_at: string | null;
  created_at: string;
}

const statusLabels: Record<string, { label: string; color: string; bg: string }> = {
  open:       { label: '待处理', color: '#f59e0b', bg: 'rgba(245,158,11,0.12)' },
  processing: { label: '处理中', color: '#3b82f6', bg: 'rgba(59,130,246,0.12)' },
  closed:     { label: '已关闭', color: '#10b981', bg: 'rgba(16,185,129,0.12)' },
};

const severityColors: Record<string, { color: string; bg: string }> = {
  '阻塞': { color: '#ef4444', bg: 'rgba(239,68,68,0.12)' },
  '严重': { color: '#f97316', bg: 'rgba(249,115,22,0.12)' },
  '高风险': { color: '#ef4444', bg: 'rgba(239,68,68,0.12)' },
  '主要': { color: '#eab308', bg: 'rgba(234,179,8,0.12)' },
  '中风险': { color: '#f97316', bg: 'rgba(249,115,22,0.12)' },
  '提示': { color: '#3b82f6', bg: 'rgba(59,130,246,0.12)' },
  '低风险': { color: '#3b82f6', bg: 'rgba(59,130,246,0.12)' },
  '建议': { color: '#6b7280', bg: 'rgba(107,114,128,0.12)' },
};

function KeyIssues() {
  const { showToast } = useToast();
  const [findings, setFindings] = useState<Finding[]>([]);
  const [members, setMembers] = useState<any[]>([]);
  const [repos, setRepos] = useState<any[]>([]);
  const [teams, setTeams] = useState<any[]>([]);
  const [taskTypes, setTaskTypes] = useState<any[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [totalPages, setTotalPages] = useState(0);
  const [loading, setLoading] = useState(false);

  // Filters
  const [filterSeverity, setFilterSeverity] = useState('');
  const [filterStatus, setFilterStatus] = useState('');
  const [filterTeam, setFilterTeam] = useState('');
  const [filterRepo, setFilterRepo] = useState('');
  const [filterTaskType, setFilterTaskType] = useState('');
  const [filterKeyword, setFilterKeyword] = useState('');

  // Repo combobox
  const [repoSearch, setRepoSearch] = useState('');
  const [repoDropdownOpen, setRepoDropdownOpen] = useState(false);
  const repoComboRef = useRef<HTMLDivElement>(null);

  // Stats
  const [stats, setStats] = useState<any>(null);

  // Detail modal
  const [detailFinding, setDetailFinding] = useState<Finding | null>(null);
  const [feedbackText, setFeedbackText] = useState('');

  const fetchFindings = useCallback(async () => {
    setLoading(true);
    const params = new URLSearchParams({ page: String(page), pageSize: '20' });
    if (filterSeverity) params.set('severity', filterSeverity);
    if (filterStatus) params.set('status', filterStatus);
    if (filterTeam) params.set('team_id', filterTeam);
    if (filterRepo) params.set('repo_id', filterRepo);
    if (filterTaskType) params.set('task_type_id', filterTaskType);
    if (filterKeyword) params.set('keyword', filterKeyword);
    try {
      const res = await fetch(`/api/findings?${params}`);
      if (res.ok) {
        const data = await res.json();
        setFindings(data.items || []);
        setTotal(data.total);
        setTotalPages(data.totalPages);
      }
    } catch { /* ignore */ }
    setLoading(false);
  }, [page, filterSeverity, filterStatus, filterTeam, filterRepo, filterTaskType, filterKeyword]);

  const fetchStats = async () => {
    try {
      const res = await fetch('/api/findings/stats');
      if (res.ok) setStats(await res.json());
    } catch { /* ignore */ }
  };

  useEffect(() => { fetchFindings(); }, [fetchFindings]);

  useEffect(() => {
    fetchStats();
    fetch('/api/members?pageSize=999').then(r => r.json()).then(d => setMembers(d?.items || d || [])).catch(() => {});
    fetch('/api/repos?pageSize=999').then(r => r.json()).then(d => setRepos(d?.items || d || [])).catch(() => {});
    fetch('/api/teams').then(r => r.json()).then(d => setTeams(d?.items || d || [])).catch(() => {});
    fetch('/api/task-types').then(r => r.json()).then(d => setTaskTypes(Array.isArray(d) ? d : d?.items || [])).catch(() => {});
  }, []);

  // Close repo dropdown on outside click
  useEffect(() => {
    const handleClick = (e: MouseEvent) => {
      if (repoComboRef.current && !repoComboRef.current.contains(e.target as Node)) {
        setRepoDropdownOpen(false);
      }
    };
    document.addEventListener('mousedown', handleClick);
    return () => document.removeEventListener('mousedown', handleClick);
  }, []);

  const updateFinding = async (id: number, updates: Record<string, any>) => {
    try {
      const res = await fetch(`/api/findings/${id}`, {
        method: 'PATCH', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(updates)
      });
      if (res.ok) {
        const updated = await res.json();
        setFindings(prev => prev.map(f => f.id === id ? updated : f));
        if (detailFinding?.id === id) setDetailFinding(updated);
        showToast('更新成功', 'success');
        fetchStats();
      }
    } catch { showToast('更新失败', 'error'); }
  };

  const handleStatusChange = (id: number, status: string) => updateFinding(id, { status });
  const handleAssigneeChange = (id: number, assignee_id: string) => updateFinding(id, { assignee_id });
  const handleFeedbackSubmit = () => {
    if (!detailFinding) return;
    updateFinding(detailFinding.id, { feedback: feedbackText });
  };

  const resetFilters = () => {
    setFilterSeverity(''); setFilterStatus(''); setFilterTeam('');
    setFilterRepo(''); setRepoSearch(''); setFilterTaskType('');
    setFilterKeyword(''); setPage(1);
  };

  // Filtered repos based on selected team + search text
  const filteredRepos = repos.filter((r: any) => {
    if (filterTeam && String(r.team_id) !== filterTeam) return false;
    if (repoSearch && !r.name.toLowerCase().includes(repoSearch.toLowerCase())) return false;
    return true;
  });

  const getSeverityStyle = (severity: string) => severityColors[severity] || { color: '#94a3b8', bg: 'rgba(148,163,184,0.12)' };
  const getStatusStyle = (status: string) => statusLabels[status] || statusLabels['open'];
  const getRepoName = (repoId: number) => {
    const repo = repos.find((r: any) => r.id === repoId);
    return repo?.name || `#${repoId}`;
  };

  const selectStyle: React.CSSProperties = {
    background: 'var(--bg-color)', color: 'var(--text-color)', border: '1px solid var(--border-color)',
    padding: '0.4rem 0.6rem', borderRadius: '6px', outline: 'none', fontSize: '0.8rem', minWidth: '90px'
  };

  const filterSelectStyle: React.CSSProperties = {
    ...selectStyle, fontSize: '0.82rem', padding: '0.45rem 0.7rem'
  };

  return (
    <div>
      {/* Stats Cards */}
      {stats && (
        <div style={{ display: 'flex', gap: '1rem', marginBottom: '1.5rem', flexWrap: 'wrap' }}>
          {(stats.by_status || []).map((s: any) => (
            <div key={s.status} className="card" style={{ padding: '0.8rem 1.2rem', display: 'flex', alignItems: 'center', gap: '0.6rem', cursor: 'pointer', minWidth: '100px' }}
              onClick={() => { setFilterStatus(filterStatus === s.status ? '' : s.status); setPage(1); }}>
              <div style={{
                width: 8, height: 8, borderRadius: '50%',
                background: getStatusStyle(s.status).color,
                boxShadow: filterStatus === s.status ? `0 0 0 3px ${getStatusStyle(s.status).bg}` : 'none'
              }} />
              <span style={{ fontSize: '0.85rem', color: '#94a3b8' }}>{getStatusStyle(s.status).label}</span>
              <span style={{ fontSize: '1.2rem', fontWeight: 700, marginLeft: 'auto' }}>{s.count}</span>
            </div>
          ))}
          <div className="card" style={{ padding: '0.8rem 1.2rem', display: 'flex', alignItems: 'center', gap: '0.6rem', minWidth: '100px' }}>
            <span style={{ fontSize: '0.85rem', color: '#94a3b8' }}>合计</span>
            <span style={{ fontSize: '1.2rem', fontWeight: 700, marginLeft: 'auto' }}>{total}</span>
          </div>
        </div>
      )}

      {/* Filters */}
      <div className="card" style={{ padding: '0.8rem 1rem', marginBottom: '1rem', display: 'flex', gap: '0.8rem', alignItems: 'center', flexWrap: 'wrap' }}>
        <select style={filterSelectStyle} value={filterSeverity} onChange={e => { setFilterSeverity(e.target.value); setPage(1); }}>
          <option value="">全部级别</option>
          <option value="阻塞">阻塞</option>
          <option value="严重">严重</option>
          <option value="主要">主要</option>
          <option value="提示">提示</option>
          <option value="建议">建议</option>
          <option value="高风险">高风险</option>
          <option value="中风险">中风险</option>
          <option value="低风险">低风险</option>
        </select>
        <select style={filterSelectStyle} value={filterStatus} onChange={e => { setFilterStatus(e.target.value); setPage(1); }}>
          <option value="">全部状态</option>
          <option value="open">待处理</option>
          <option value="processing">处理中</option>
          <option value="closed">已关闭</option>
        </select>
        <select style={filterSelectStyle} value={filterTeam} onChange={e => { setFilterTeam(e.target.value); setFilterRepo(''); setRepoSearch(''); setPage(1); }}>
          <option value="">全部部门</option>
          {teams.map((t: any) => <option key={t.id} value={t.id}>{t.name}</option>)}
        </select>
        <div ref={repoComboRef} style={{ position: 'relative', minWidth: '160px' }}>
          <input
            style={{ ...filterSelectStyle, width: '100%', boxSizing: 'border-box' }}
            placeholder="搜索代码仓..."
            value={repoSearch}
            onChange={e => { setRepoSearch(e.target.value); setRepoDropdownOpen(true); if (!e.target.value) { setFilterRepo(''); setPage(1); } }}
            onFocus={() => setRepoDropdownOpen(true)}
          />
          {filterRepo && (
            <span onClick={() => { setFilterRepo(''); setRepoSearch(''); setPage(1); }}
              style={{ position: 'absolute', right: '6px', top: '50%', transform: 'translateY(-50%)', cursor: 'pointer', color: '#94a3b8', fontSize: '0.85rem', lineHeight: 1 }}>✕</span>
          )}
          {repoDropdownOpen && filteredRepos.length > 0 && (
            <div style={{
              position: 'absolute', top: '100%', left: 0, right: 0, zIndex: 50,
              background: 'var(--card-bg)', border: '1px solid var(--border-color)', borderRadius: '6px',
              maxHeight: '220px', overflowY: 'auto', marginTop: '2px',
              boxShadow: '0 4px 12px rgba(0,0,0,0.15)'
            }}>
              <div onClick={() => { setFilterRepo(''); setRepoSearch(''); setRepoDropdownOpen(false); setPage(1); }}
                style={{ padding: '0.4rem 0.7rem', fontSize: '0.82rem', cursor: 'pointer', color: '#94a3b8', borderBottom: '1px solid var(--border-color)' }}>
                全部代码仓
              </div>
              {filteredRepos.map((r: any) => (
                <div key={r.id}
                  onClick={() => { setFilterRepo(String(r.id)); setRepoSearch(r.name); setRepoDropdownOpen(false); setPage(1); }}
                  style={{
                    padding: '0.4rem 0.7rem', fontSize: '0.82rem', cursor: 'pointer',
                    color: 'var(--text-color)',
                    background: String(r.id) === filterRepo ? 'var(--primary-color-light, rgba(99,102,241,0.1))' : 'transparent',
                  }}
                  onMouseEnter={e => (e.currentTarget.style.background = 'var(--primary-color-light, rgba(99,102,241,0.08))')}
                  onMouseLeave={e => (e.currentTarget.style.background = String(r.id) === filterRepo ? 'var(--primary-color-light, rgba(99,102,241,0.1))' : 'transparent')}
                >
                  {r.name}
                </div>
              ))}
            </div>
          )}
        </div>
        <select style={filterSelectStyle} value={filterTaskType} onChange={e => { setFilterTaskType(e.target.value); setPage(1); }}>
          <option value="">全部任务类型</option>
          {taskTypes.map((t: any) => <option key={t.id} value={t.id}>{t.display_name}</option>)}
        </select>
        <input
          style={{ ...filterSelectStyle, minWidth: '180px', flex: 1 }}
          placeholder="搜索标题、描述或文件路径..."
          value={filterKeyword}
          onChange={e => setFilterKeyword(e.target.value)}
          onKeyDown={e => { if (e.key === 'Enter') { setPage(1); fetchFindings(); } }}
        />
        <button onClick={resetFilters} style={{ padding: '0.45rem 0.8rem', borderRadius: '6px', border: '1px solid var(--border-color)', background: 'transparent', color: '#94a3b8', cursor: 'pointer', fontSize: '0.82rem' }}>
          重置
        </button>
      </div>

      {/* Table */}
      <div className="card" style={{ padding: 0, overflow: 'hidden' }}>
        <table style={{ width: '100%', borderCollapse: 'collapse' }}>
          <thead>
            <tr style={{ borderBottom: '1px solid var(--border-color)', color: '#64748b', fontSize: '0.82rem', textAlign: 'left', background: 'var(--bg-color)' }}>
              <th style={{ padding: '0.75rem 1rem', width: '35%' }}>问题</th>
              <th style={{ padding: '0.75rem 0.5rem' }}>代码仓</th>
              <th style={{ padding: '0.75rem 0.5rem' }}>级别</th>
              <th style={{ padding: '0.75rem 0.5rem' }}>分类</th>
              <th style={{ padding: '0.75rem 0.5rem' }}>状态</th>
              <th style={{ padding: '0.75rem 0.5rem' }}>处理人</th>
              <th style={{ padding: '0.75rem 0.5rem' }}>发现时间</th>
              <th style={{ padding: '0.75rem 0.5rem', textAlign: 'right' }}>操作</th>
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr><td colSpan={8} style={{ textAlign: 'center', padding: '3rem', color: '#94a3b8' }}>加载中...</td></tr>
            ) : findings.length === 0 ? (
              <tr><td colSpan={8} style={{ textAlign: 'center', padding: '3rem', color: '#94a3b8' }}>暂无问题记录</td></tr>
            ) : findings.map(f => {
              const sevStyle = getSeverityStyle(f.severity);
              const stStyle = getStatusStyle(f.status);
              return (
                <tr key={f.id} style={{ borderBottom: '1px solid var(--border-color)' }}>
                  <td style={{ padding: '0.75rem 1rem' }}>
                    <div style={{ fontWeight: 500, fontSize: '0.88rem', marginBottom: '0.25rem', lineHeight: 1.4, cursor: 'pointer', color: 'var(--text-color)' }}
                      onClick={() => { setDetailFinding(f); setFeedbackText(f.feedback || ''); }}>
                      {f.title}
                    </div>
                    {f.file_path && (
                      <div style={{ fontSize: '0.75rem', color: '#64748b', fontFamily: 'monospace' }}>
                        {f.file_path}{f.line_number ? `:${f.line_number}` : ''}
                      </div>
                    )}
                  </td>
                  <td style={{ padding: '0.75rem 0.5rem', fontSize: '0.82rem', color: '#94a3b8' }}>{getRepoName(f.repo_id)}</td>
                  <td style={{ padding: '0.75rem 0.5rem' }}>
                    <span style={{ fontSize: '0.75rem', padding: '0.15rem 0.5rem', borderRadius: '4px', background: sevStyle.bg, color: sevStyle.color, fontWeight: 600 }}>
                      {f.severity}
                    </span>
                  </td>
                  <td style={{ padding: '0.75rem 0.5rem', fontSize: '0.78rem', color: '#64748b' }}>{f.category || '-'}</td>
                  <td style={{ padding: '0.75rem 0.5rem' }}>
                    <select value={f.status} onChange={e => handleStatusChange(f.id, e.target.value)} style={selectStyle}>
                      <option value="open">待处理</option>
                      <option value="processing">处理中</option>
                      <option value="closed">已关闭</option>
                    </select>
                  </td>
                  <td style={{ padding: '0.75rem 0.5rem' }}>
                    <select value={f.assignee_id || ''} onChange={e => handleAssigneeChange(f.id, e.target.value)} style={selectStyle}>
                      <option value="">未分配</option>
                      {members.map(m => <option key={m.id} value={m.id}>{m.name}</option>)}
                    </select>
                  </td>
                  <td style={{ padding: '0.75rem 0.5rem', fontSize: '0.78rem', color: '#64748b', whiteSpace: 'nowrap' }}>
                    {f.created_at ? new Date(f.created_at).toLocaleDateString('zh-CN') : '-'}
                  </td>
                  <td style={{ padding: '0.75rem 0.5rem', textAlign: 'right' }}>
                    <button onClick={() => { setDetailFinding(f); setFeedbackText(f.feedback || ''); }}
                      style={{ padding: '0.25rem 0.6rem', borderRadius: '4px', border: '1px solid var(--primary-color)', background: 'transparent', color: 'var(--primary-color)', cursor: 'pointer', fontSize: '0.78rem' }}>
                      详情
                    </button>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      {/* Pagination */}
      {totalPages > 1 && (
        <div style={{ display: 'flex', justifyContent: 'center', gap: '0.5rem', marginTop: '1rem', alignItems: 'center' }}>
          <button disabled={page <= 1} onClick={() => setPage(p => p - 1)}
            style={{ padding: '0.4rem 0.8rem', borderRadius: '6px', border: '1px solid var(--border-color)', background: 'transparent', color: 'var(--text-color)', cursor: page <= 1 ? 'default' : 'pointer', opacity: page <= 1 ? 0.4 : 1 }}>
            上一页
          </button>
          <span style={{ fontSize: '0.85rem', color: '#94a3b8' }}>{page} / {totalPages}（共 {total} 条）</span>
          <button disabled={page >= totalPages} onClick={() => setPage(p => p + 1)}
            style={{ padding: '0.4rem 0.8rem', borderRadius: '6px', border: '1px solid var(--border-color)', background: 'transparent', color: 'var(--text-color)', cursor: page >= totalPages ? 'default' : 'pointer', opacity: page >= totalPages ? 0.4 : 1 }}>
            下一页
          </button>
        </div>
      )}

      {/* Detail Modal */}
      {detailFinding && createPortal(
        <div style={{ position: 'fixed', top: 0, left: 0, right: 0, bottom: 0, background: 'rgba(0,0,0,0.6)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 }}
          onClick={e => { if (e.target === e.currentTarget) setDetailFinding(null); }}>
          <div className="card" style={{ width: '720px', maxWidth: '95vw', maxHeight: '85vh', overflowY: 'auto', padding: '1.5rem' }}>
            {/* Header */}
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: '1.2rem' }}>
              <div style={{ flex: 1 }}>
                <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center', marginBottom: '0.5rem' }}>
                  <span style={{ fontSize: '0.78rem', padding: '0.15rem 0.5rem', borderRadius: '4px', background: getSeverityStyle(detailFinding.severity).bg, color: getSeverityStyle(detailFinding.severity).color, fontWeight: 600 }}>
                    {detailFinding.severity}
                  </span>
                  {detailFinding.category && (
                    <span style={{ fontSize: '0.75rem', padding: '0.15rem 0.5rem', borderRadius: '4px', background: 'rgba(100,116,139,0.12)', color: '#94a3b8' }}>
                      {detailFinding.category}
                    </span>
                  )}
                  <span style={{ fontSize: '0.75rem', padding: '0.15rem 0.5rem', borderRadius: '4px', background: getStatusStyle(detailFinding.status).bg, color: getStatusStyle(detailFinding.status).color, fontWeight: 500 }}>
                    {getStatusStyle(detailFinding.status).label}
                  </span>
                </div>
                <h3 style={{ margin: 0, fontSize: '1.05rem', fontWeight: 600, lineHeight: 1.4 }}>{detailFinding.title}</h3>
              </div>
              <button onClick={() => setDetailFinding(null)} style={{ background: 'transparent', border: 'none', cursor: 'pointer', padding: '0.25rem', color: '#64748b', fontSize: '1.3rem', lineHeight: 1, flexShrink: 0, marginLeft: '1rem' }}>✕</button>
            </div>

            {/* File info */}
            {detailFinding.file_path && (
              <div style={{ background: 'var(--bg-color)', border: '1px solid var(--border-color)', borderRadius: '6px', padding: '0.5rem 0.8rem', marginBottom: '1rem', fontFamily: 'monospace', fontSize: '0.82rem', color: '#94a3b8' }}>
                📄 {detailFinding.file_path}{detailFinding.line_number ? `:${detailFinding.line_number}` : ''}
                <span style={{ marginLeft: '1rem', color: '#64748b' }}>代码仓: {getRepoName(detailFinding.repo_id)}</span>
              </div>
            )}

            {/* Detail */}
            {detailFinding.detail && (
              <div style={{ marginBottom: '1rem' }}>
                <div style={{ fontSize: '0.8rem', fontWeight: 600, color: '#64748b', marginBottom: '0.4rem' }}>问题详情</div>
                <div style={{ fontSize: '0.88rem', lineHeight: 1.7, color: 'var(--text-color)', whiteSpace: 'pre-wrap' }}>{detailFinding.detail}</div>
              </div>
            )}

            {/* Code Snippet */}
            {detailFinding.code_snippet && (
              <div style={{ marginBottom: '1rem' }}>
                <div style={{ fontSize: '0.8rem', fontWeight: 600, color: '#64748b', marginBottom: '0.4rem' }}>相关代码</div>
                <pre style={{ background: '#1e293b', color: '#e2e8f0', padding: '1rem', borderRadius: '8px', fontSize: '0.82rem', lineHeight: 1.6, overflowX: 'auto', margin: 0, fontFamily: "'JetBrains Mono', 'Fira Code', monospace" }}>
                  {detailFinding.code_snippet}
                </pre>
              </div>
            )}

            {/* Suggestion */}
            {detailFinding.suggestion && (
              <div style={{ marginBottom: '1rem' }}>
                <div style={{ fontSize: '0.8rem', fontWeight: 600, color: '#64748b', marginBottom: '0.4rem' }}>修复建议</div>
                <div style={{ background: '#f0fdf4', border: '1px solid #bbf7d0', borderRadius: '6px', padding: '0.8rem', fontSize: '0.85rem', lineHeight: 1.6, color: '#15803d', whiteSpace: 'pre-wrap' }}>
                  💡 {detailFinding.suggestion}
                </div>
              </div>
            )}

            {/* Actions */}
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem', marginBottom: '1rem', padding: '1rem', background: 'var(--bg-color)', borderRadius: '8px', border: '1px solid var(--border-color)' }}>
              <div>
                <div style={{ fontSize: '0.78rem', fontWeight: 600, color: '#64748b', marginBottom: '0.3rem' }}>处理状态</div>
                <select value={detailFinding.status} onChange={e => handleStatusChange(detailFinding.id, e.target.value)} style={{ ...selectStyle, width: '100%' }}>
                  <option value="open">待处理</option>
                  <option value="processing">处理中</option>
                  <option value="closed">已关闭</option>
                </select>
              </div>
              <div>
                <div style={{ fontSize: '0.78rem', fontWeight: 600, color: '#64748b', marginBottom: '0.3rem' }}>指派处理人</div>
                <select value={detailFinding.assignee_id || ''} onChange={e => handleAssigneeChange(detailFinding.id, e.target.value)} style={{ ...selectStyle, width: '100%' }}>
                  <option value="">未分配</option>
                  {members.map(m => <option key={m.id} value={m.id}>{m.name}</option>)}
                </select>
              </div>
            </div>

            {/* Feedback */}
            <div style={{ marginBottom: '1rem' }}>
              <div style={{ fontSize: '0.8rem', fontWeight: 600, color: '#64748b', marginBottom: '0.4rem' }}>
                处理反馈
                {detailFinding.feedback_at && (
                  <span style={{ fontWeight: 400, marginLeft: '0.5rem', color: '#94a3b8' }}>
                    上次反馈: {new Date(detailFinding.feedback_at).toLocaleString('zh-CN')}
                  </span>
                )}
              </div>
              <textarea
                value={feedbackText}
                onChange={e => setFeedbackText(e.target.value)}
                placeholder="填写处理进展或反馈..."
                style={{ width: '100%', padding: '0.7rem', borderRadius: '6px', border: '1px solid var(--border-color)', outline: 'none', resize: 'vertical', minHeight: '80px', fontSize: '0.85rem', boxSizing: 'border-box', background: 'var(--card-bg)', color: 'var(--text-color)' }}
              />
              <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: '0.5rem' }}>
                <button className="btn" onClick={handleFeedbackSubmit}
                  disabled={feedbackText === (detailFinding.feedback || '')}
                  style={{ padding: '0.4rem 1rem', fontSize: '0.85rem', opacity: feedbackText === (detailFinding.feedback || '') ? 0.5 : 1 }}>
                  保存反馈
                </button>
              </div>
            </div>

            {/* Meta */}
            <div style={{ fontSize: '0.75rem', color: '#64748b', borderTop: '1px solid var(--border-color)', paddingTop: '0.8rem', display: 'flex', gap: '1.5rem' }}>
              <span>ID: {detailFinding.id}</span>
              <span>报告: #{detailFinding.task_report_id}</span>
              <span>创建: {new Date(detailFinding.created_at).toLocaleString('zh-CN')}</span>
            </div>
          </div>
        </div>, document.body
      )}
    </div>
  );
}

export default KeyIssues;
