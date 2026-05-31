import React, { useEffect, useState } from 'react';
import { apiUrl } from '../config';
import { useToast } from '../components/Toast';

// Inline Icons as SVGs for portable premium look
const PlayIcon = () => (
  <svg width="20" height="20" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <polygon points="5 3 19 12 5 21 5 3"></polygon>
  </svg>
);

const CheckCircleIcon = () => (
  <svg width="20" height="20" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"></path>
    <polyline points="22 4 12 14.01 9 11.01"></polyline>
  </svg>
);

const ShieldAlertIcon = () => (
  <svg width="20" height="20" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"></path>
    <line x1="12" y1="9" x2="12" y2="13"></line>
    <line x1="12" y1="17" x2="12.01" y2="17"></line>
  </svg>
);

const RefreshIcon = () => (
  <svg width="20" height="20" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <path d="M21.5 2v6h-6M21.34 15.57a10 10 0 1 1-.57-8.38l5.67-5.67"></path>
  </svg>
);

const SearchIcon = () => (
  <svg width="16" height="16" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
    <circle cx="11" cy="11" r="8"></circle>
    <line x1="21" y1="21" x2="16.65" y2="16.65"></line>
  </svg>
);

export default function FloatAnalysis() {
  const { showToast } = useToast();
  
  // Dashboard overall stats & states
  const [loading, setLoading] = useState(true);
  const [repos, setRepos] = useState<any[]>([]);
  const [depts, setDepts] = useState<any[]>([]);
  const [trends, setTrends] = useState<any[]>([]);
  
  const [activeTab, setActiveTab] = useState<'repos' | 'depts' | 'trends'>('repos');
  const [departments, setDepartments] = useState<string[]>([]);
  const [selectedDept, setSelectedDept] = useState('');
  const [repoPage, setRepoPage] = useState(1);
  const repoPageSize = 10;
  const [keyword, setKeyword] = useState('');
  
  // Sorting state
  const [sortField, setSortField] = useState('fix_rate');
  const [sortOrder, setSortOrder] = useState<'asc' | 'desc'>('desc');
  
  // Trend dimension
  const [trendScope, setTrendScope] = useState<'global' | 'repo' | 'dept'>('global');
  const [trendRepoId, setTrendRepoId] = useState('');
  const [trendDeptName, setTrendDeptName] = useState('');
  
  // Audit Workspace Panel
  const [workspaceOpen, setWorkspaceOpen] = useState(false);
  const [selectedRepoId, setSelectedRepoId] = useState<number | null>(null);
  const [selectedRepoName, setSelectedRepoName] = useState('');
  
  // Workspace filter findings
  const [workspaceFindings, setWorkspaceFindings] = useState<any[]>([]);
  const [workspacePage, setWorkspacePage] = useState(1);
  const [workspaceTotalPages, setWorkspaceTotalPages] = useState(1);
  const [wsSeverity, setWsSeverity] = useState('');
  const [wsStatus, setWsStatus] = useState('');
  const [wsCategory, setWsCategory] = useState('');
  const [wsKeyword, setWsKeyword] = useState('');
  
  // Workflows (edit issue dialog)
  const [editingFinding, setEditingFinding] = useState<any | null>(null);
  const [workflowStatus, setWorkflowStatus] = useState('open');
  const [workflowAssignee, setWorkflowAssignee] = useState('');
  const [workflowComment, setWorkflowComment] = useState('');
  const [assigneeSearch, setAssigneeSearch] = useState('');
  const [dropdownOpen, setDropdownOpen] = useState(false);
  
  // System models lists
  const [members, setMembers] = useState<any[]>([]);
  const [floatTaskTypeId, setFloatTaskTypeId] = useState<number | null>(null);

  // Initialize and load core resources
  useEffect(() => {
    fetchTaskTypeId();
    fetchDepartments();
    fetchMembers();
  }, []);

  // Sync content based on tab and filters
  useEffect(() => {
    if (activeTab === 'repos') {
      fetchReposData();
    } else if (activeTab === 'depts') {
      fetchDeptsData();
    } else if (activeTab === 'trends') {
      fetchTrendsData();
    }
  }, [activeTab, selectedDept, sortField, sortOrder, trendScope, trendRepoId, trendDeptName]);

  const fetchDepartments = () => {
    fetch(apiUrl('/api/analysis/float/departments'))
      .then(res => res.json())
      .then(data => {
        if (Array.isArray(data)) {
          const list = data.map(d => d.department).filter(Boolean);
          setDepartments(list);
        }
      })
      .catch(console.error);
  };

  const fetchMembers = () => {
    fetch(apiUrl('/api/members?pageSize=1000'))
      .then(res => res.json())
      .then(data => {
        if (Array.isArray(data)) {
          setMembers(data);
        } else if (data && Array.isArray(data.members)) {
          setMembers(data.members);
        }
      })
      .catch(console.error);
  };

  const fetchTaskTypeId = () => {
    fetch(apiUrl('/api/task-types'))
      .then(res => res.json())
      .then(data => {
        const list = Array.isArray(data) ? data : (data?.taskTypes || []);
        const floatTask = list.find((t: any) => t.name === 'float_comparison');
        if (floatTask) {
          setFloatTaskTypeId(floatTask.id || floatTask.ID);
        }
      })
      .catch(console.error);
  };

  const fetchReposData = () => {
    setLoading(true);
    setRepoPage(1);
    const params = new URLSearchParams({
      sort_by: sortField,
      sort_order: sortOrder,
    });
    if (keyword) params.append('keyword', keyword);
    if (selectedDept) params.append('department', selectedDept);

    fetch(apiUrl(`/api/analysis/float/repos?${params.toString()}`))
      .then(res => res.json())
      .then(data => {
        setRepos(Array.isArray(data) ? data : []);
      })
      .catch(err => {
        console.error(err);
        showToast('获取代码仓统计数据失败', 'error');
      })
      .finally(() => setLoading(false));
  };

  const fetchDeptsData = () => {
    setLoading(true);
    const params = new URLSearchParams({
      sort_by: sortField === 'name' ? 'department' : sortField,
      sort_order: sortOrder,
    });
    fetch(apiUrl(`/api/analysis/float/departments?${params.toString()}`))
      .then(res => res.json())
      .then(data => {
        setDepts(Array.isArray(data) ? data : []);
      })
      .catch(err => {
        console.error(err);
        showToast('获取部门分析数据失败', 'error');
      })
      .finally(() => setLoading(false));
  };

  const fetchTrendsData = () => {
    setLoading(true);
    const params = new URLSearchParams({ days: '30' });
    if (trendScope === 'repo' && trendRepoId) {
      params.append('repo_id', trendRepoId);
    } else if (trendScope === 'dept' && trendDeptName) {
      params.append('department', trendDeptName);
    }

    fetch(apiUrl(`/api/analysis/float/trends?${params.toString()}`))
      .then(res => res.json())
      .then(data => {
        setTrends(Array.isArray(data) ? data : []);
      })
      .catch(err => {
        console.error(err);
        showToast('获取趋势数据失败', 'error');
      })
      .finally(() => setLoading(false));
  };

  const handleSort = (field: string) => {
    if (sortField === field) {
      setSortOrder(prev => (prev === 'asc' ? 'desc' : 'asc'));
    } else {
      setSortField(field);
      setSortOrder('desc');
    }
  };

  // Workspace Actions
  const openWorkspace = (repoId: number, repoName: string) => {
    setSelectedRepoId(repoId);
    setSelectedRepoName(repoName);
    setWorkspacePage(1);
    setWsSeverity('');
    setWsStatus('');
    setWsCategory('');
    setWsKeyword('');
    setWorkspaceOpen(true);
    fetchWorkspaceFindings(repoId, 1, '', '', '', '');
  };

  const fetchWorkspaceFindings = (
    repoId: number,
    page: number,
    severity: string,
    status: string,
    category: string,
    kw: string
  ) => {
    const params = new URLSearchParams({
      repo_id: repoId.toString(),
      page: page.toString(),
      pageSize: '10',
    });
    if (severity) params.append('severity', severity);
    if (status) params.append('status', status);
    if (category) params.append('category', category);
    if (kw) params.append('keyword', kw);

    fetch(apiUrl(`/api/analysis/float/findings?${params.toString()}`))
      .then(res => res.json())
      .then(data => {
        if (data) {
          setWorkspaceFindings(data.findings || []);
          setWorkspaceTotalPages(data.totalPages || 1);
        }
      })
      .catch(err => {
        console.error(err);
        showToast('获取缺陷审计列表失败', 'error');
      });
  };

  const handleWorkspaceFilterChange = (field: 'severity' | 'status' | 'category' | 'keyword', value: string) => {
    let severity = wsSeverity;
    let status = wsStatus;
    let category = wsCategory;
    let kw = wsKeyword;

    if (field === 'severity') { severity = value; setWsSeverity(value); }
    if (field === 'status') { status = value; setWsStatus(value); }
    if (field === 'category') { category = value; setWsCategory(value); }
    if (field === 'keyword') { kw = value; setWsKeyword(value); }

    setWorkspacePage(1);
    if (selectedRepoId) {
      fetchWorkspaceFindings(selectedRepoId, 1, severity, status, category, kw);
    }
  };

  const handleWorkspacePageChange = (newPage: number) => {
    setWorkspacePage(newPage);
    if (selectedRepoId) {
      fetchWorkspaceFindings(selectedRepoId, newPage, wsSeverity, wsStatus, wsCategory, wsKeyword);
    }
  };

  // Open Edit finding / Workflow
  const startWorkflow = (finding: any) => {
    setEditingFinding(finding);
    setWorkflowStatus(finding.status);
    const mId = finding.assignee_id || '';
    setWorkflowAssignee(mId);
    setWorkflowComment('');

    // Set search string based on assignee
    const current = members.find(m => (m.id || m.ID) === mId);
    setAssigneeSearch(current ? `${current.name} (${current.id || current.ID})` : '');
  };

  const submitWorkflow = (e: React.FormEvent) => {
    e.preventDefault();
    if (!editingFinding) return;

    fetch(apiUrl(`/api/analysis/float/findings/${editingFinding.ID || editingFinding.id}`), {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        status: workflowStatus,
        assignee_id: workflowAssignee || null,
        feedback: workflowComment || undefined,
      }),
    })
      .then(res => {
        if (res.ok) {
          showToast('缺陷审计状态已成功更新', 'success');
          setEditingFinding(null);
          // Refresh lists
          if (selectedRepoId) {
            fetchWorkspaceFindings(selectedRepoId, workspacePage, wsSeverity, wsStatus, wsCategory, wsKeyword);
          }
          fetchReposData();
        } else {
          showToast('更新审计信息失败', 'error');
        }
      })
      .catch(err => {
        console.error(err);
        showToast('更新审计过程出现网络错误', 'error');
      });
  };

  // KPIs Calculations
  const totalIssuesCount = repos.reduce((acc, r) => acc + r.total_issues, 0);
  const totalOpenCount = repos.reduce((acc, r) => acc + r.open_issues, 0);
  const totalResolvedCount = repos.reduce((acc, r) => acc + r.resolved_issues, 0);
  const totalScannedRepos = repos.filter(r => r.last_scan_time && r.last_scan_time !== '0001-01-01T00:00:00Z').length;
  
  const overallFixRate = (totalIssuesCount > 0)
    ? (totalResolvedCount / totalIssuesCount) * 100
    : 0;

  const totalBlocking = repos.reduce((acc, r) => acc + r.blocking, 0);
  const totalCritical = repos.reduce((acc, r) => acc + r.critical, 0);

  // Repository list pagination calculations
  const totalRepoPages = Math.ceil(repos.length / repoPageSize) || 1;
  const startIndex = (repoPage - 1) * repoPageSize;
  const paginatedRepos = repos.slice(startIndex, startIndex + repoPageSize);

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '1.5rem', color: 'var(--text-color)', fontFamily: 'Inter, system-ui, sans-serif' }}>
      
      {/* 1. TOP HEADER & KPI CARDS */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
         <div>
           <h2 style={{ margin: 0, fontSize: '1.5rem', fontWeight: 700 }}>Python 浮点数安全专项</h2>
           <p style={{ margin: '0.25rem 0 0 0', fontSize: '0.875rem', color: '#64748b' }}>
             审计与修复 Python 代码中由于 `float` 直接进行边界或等值比较导致的精度丢失与逻辑控制隐患。
           </p>
         </div>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: '1rem' }}>
        <div style={{ background: 'var(--card-bg)', border: '1px solid var(--border-color)', borderRadius: '8px', padding: '1.25rem', display: 'flex', alignItems: 'center', gap: '1rem' }}>
          <div style={{ width: '40px', height: '40px', borderRadius: '8px', background: 'rgba(59, 130, 246, 0.1)', color: '#3b82f6', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
            <PlayIcon />
          </div>
          <div>
            <div style={{ fontSize: '0.8rem', color: '#64748b', fontWeight: 500 }}>受影响代码仓</div>
            <div style={{ fontSize: '1.35rem', fontWeight: 700 }}>{repos.filter(r => r.total_issues > 0).length}</div>
            <div style={{ fontSize: '0.75rem', color: '#94a3b8' }}>已扫描 {totalScannedRepos} 个代码仓</div>
          </div>
        </div>

        <div style={{ background: 'var(--card-bg)', border: '1px solid var(--border-color)', borderRadius: '8px', padding: '1.25rem', display: 'flex', alignItems: 'center', gap: '1rem' }}>
          <div style={{ width: '40px', height: '40px', borderRadius: '8px', background: 'rgba(16, 185, 129, 0.1)', color: '#10b981', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
            <CheckCircleIcon />
          </div>
          <div>
            <div style={{ fontSize: '0.8rem', color: '#64748b', fontWeight: 500 }}>专项整改率</div>
            <div style={{ fontSize: '1.35rem', fontWeight: 700, color: '#10b981' }}>{overallFixRate.toFixed(1)}%</div>
            <div style={{ fontSize: '0.75rem', color: '#94a3b8' }}>已验证关闭 {totalResolvedCount} / {totalIssuesCount} 个缺陷</div>
          </div>
        </div>

        <div style={{ background: 'var(--card-bg)', border: '1px solid var(--border-color)', borderRadius: '8px', padding: '1.25rem', display: 'flex', alignItems: 'center', gap: '1rem' }}>
          <div style={{ width: '40px', height: '40px', borderRadius: '8px', background: 'rgba(239, 68, 68, 0.1)', color: '#ef4444', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
            <ShieldAlertIcon />
          </div>
          <div>
            <div style={{ fontSize: '0.8rem', color: '#64748b', fontWeight: 500 }}>阻塞 / 严重缺陷</div>
            <div style={{ fontSize: '1.35rem', fontWeight: 700, color: '#ef4444' }}>
              {totalBlocking} <span style={{ fontSize: '0.8rem', fontWeight: 500, color: '#94a3b8' }}>/</span> {totalCritical}
            </div>
            <div style={{ fontSize: '0.75rem', color: '#94a3b8' }}>急需修复的高危精度边界隐患</div>
          </div>
        </div>

        <div style={{ background: 'var(--card-bg)', border: '1px solid var(--border-color)', borderRadius: '8px', padding: '1.25rem', display: 'flex', alignItems: 'center', gap: '1rem' }}>
          <div style={{ width: '40px', height: '40px', borderRadius: '8px', background: 'rgba(249, 115, 22, 0.1)', color: '#f97316', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
            <RefreshIcon />
          </div>
          <div>
            <div style={{ fontSize: '0.8rem', color: '#64748b', fontWeight: 500 }}>待审计流转</div>
            <div style={{ fontSize: '1.35rem', fontWeight: 700, color: '#f97316' }}>{totalOpenCount}</div>
            <div style={{ fontSize: '0.75rem', color: '#94a3b8' }}>包含 open 和 analyzing 状态</div>
          </div>
        </div>
      </div>

      {/* 2. SUB TAB NAVIGATION & FILTERS */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '1rem', borderBottom: '1px solid var(--border-color)', paddingBottom: '0.5rem' }}>
        <div style={{ display: 'flex', gap: '1.5rem' }}>
          <button 
            onClick={() => { setActiveTab('repos'); setSortField('fix_rate'); }}
            style={{ padding: '0.75rem 0.25rem', border: 'none', background: 'transparent', borderBottom: activeTab === 'repos' ? '2px solid var(--primary-color)' : '2px solid transparent', color: activeTab === 'repos' ? 'var(--primary-color)' : '#64748b', fontWeight: activeTab === 'repos' ? 600 : 500, cursor: 'pointer', fontSize: '0.9rem' }}
          >
            代码仓看板
          </button>
          <button 
            onClick={() => { setActiveTab('depts'); setSortField('fix_rate'); }}
            style={{ padding: '0.75rem 0.25rem', border: 'none', background: 'transparent', borderBottom: activeTab === 'depts' ? '2px solid var(--primary-color)' : '2px solid transparent', color: activeTab === 'depts' ? 'var(--primary-color)' : '#64748b', fontWeight: activeTab === 'depts' ? 600 : 500, cursor: 'pointer', fontSize: '0.9rem' }}
          >
            部门排行榜
          </button>
          <button 
            onClick={() => { setActiveTab('trends'); }}
            style={{ padding: '0.75rem 0.25rem', border: 'none', background: 'transparent', borderBottom: activeTab === 'trends' ? '2px solid var(--primary-color)' : '2px solid transparent', color: activeTab === 'trends' ? 'var(--primary-color)' : '#64748b', fontWeight: activeTab === 'trends' ? 600 : 500, cursor: 'pointer', fontSize: '0.9rem' }}
          >
            收敛趋势分析
          </button>
        </div>

        {activeTab === 'repos' && (
          <div style={{ display: 'flex', gap: '0.75rem', flexWrap: 'wrap' }}>
            <div style={{ position: 'relative', display: 'flex', alignItems: 'center' }}>
              <input 
                type="text" 
                placeholder="搜索代码仓名称..."
                value={keyword}
                onChange={e => setKeyword(e.target.value)}
                onKeyDown={e => e.key === 'Enter' && fetchReposData()}
                style={{ padding: '0.4rem 0.75rem 0.4rem 2rem', borderRadius: '6px', border: '1px solid var(--border-color)', background: 'var(--card-bg)', fontSize: '0.85rem', outline: 'none', width: '180px' }}
              />
              <span style={{ position: 'absolute', left: '8px', color: '#94a3b8', display: 'flex', alignItems: 'center' }}>
                <SearchIcon />
              </span>
            </div>
            <select
              value={selectedDept}
              onChange={e => {
                setSelectedDept(e.target.value);
                setRepoPage(1);
              }}
              style={{
                padding: '0.4rem 2rem 0.4rem 0.75rem',
                borderRadius: '6px',
                border: '1px solid var(--border-color)',
                background: 'var(--card-bg)',
                color: 'var(--text-color)',
                fontSize: '0.85rem',
                outline: 'none',
                cursor: 'pointer',
                minWidth: '150px',
                appearance: 'none',
                backgroundImage: `url("data:image/svg+xml;charset=UTF-8,%3csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%2364748b' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'%3e%3cpolyline points='6 9 12 15 18 9'%3e%3c/polyline%3e%3c/svg%3e")`,
                backgroundRepeat: 'no-repeat',
                backgroundPosition: 'right 8px center',
                backgroundSize: '16px'
              }}
            >
              <option value="">所有归属部门</option>
              {departments.map(d => (
                <option key={d} value={d}>{d}</option>
              ))}
            </select>
            <button className="btn" onClick={fetchReposData} style={{ padding: '0.4rem 0.8rem', fontSize: '0.85rem' }}>查询</button>
          </div>
        )}
      </div>

      {/* 3. TAB CONTENT */}
      {loading ? (
        <div style={{ display: 'flex', justifyContent: 'center', padding: '6rem' }}>
          <div style={{ textAlign: 'center', color: '#64748b' }}>
            <div style={{ animation: 'spin 1s linear infinite', border: '3px solid rgba(59, 130, 246, 0.1)', borderTop: '3px solid #3b82f6', borderRadius: '50%', width: '32px', height: '32px', margin: '0 auto 1rem' }} />
            数据加载中，请稍候...
          </div>
          <style>{`@keyframes spin { 0% { transform: rotate(0deg); } 100% { transform: rotate(360deg); } }`}</style>
        </div>
      ) : (
        <>
          {/* TAB 1: REPOSITORIES OVERVIEW */}
          {activeTab === 'repos' && (
            <div className="card" style={{ padding: 0, overflowX: 'auto', border: '1px solid var(--border-color)' }}>
              <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                <thead>
                  <tr style={{ background: 'var(--bg-color)', borderBottom: '1px solid var(--border-color)' }}>
                    <th onClick={() => handleSort('name')} style={styles.tableHeader}>
                      代码仓 {sortField === 'name' && (sortOrder === 'asc' ? '↑' : '↓')}
                    </th>
                    <th style={styles.tableHeader}>归属部门</th>
                    <th style={styles.tableHeader}>负责人</th>
                    <th onClick={() => handleSort('total_issues')} style={styles.tableHeader}>
                      跟踪缺陷数 {sortField === 'total_issues' && (sortOrder === 'asc' ? '↑' : '↓')}
                    </th>
                    <th onClick={() => handleSort('blocking')} style={styles.tableHeader}>
                      阻塞 {sortField === 'blocking' && (sortOrder === 'asc' ? '↑' : '↓')}
                    </th>
                    <th onClick={() => handleSort('critical')} style={styles.tableHeader}>
                      严重 {sortField === 'critical' && (sortOrder === 'asc' ? '↑' : '↓')}
                    </th>
                    <th onClick={() => handleSort('fix_rate')} style={styles.tableHeader}>
                      修复进度 {sortField === 'fix_rate' && (sortOrder === 'asc' ? '↑' : '↓')}
                    </th>
                    <th onClick={() => handleSort('last_scan_time')} style={styles.tableHeader}>
                      最近扫描 {sortField === 'last_scan_time' && (sortOrder === 'asc' ? '↑' : '↓')}
                    </th>
                    <th style={{ ...styles.tableHeader, cursor: 'default' }}>操作</th>
                  </tr>
                </thead>
                <tbody>
                  {repos.length === 0 ? (
                    <tr>
                      <td colSpan={9} style={{ textAlign: 'center', padding: '4rem 1rem', color: '#94a3b8' }}>
                        未找到包含扫描数据的代码仓。请先前往“扫描任务”启动“Python 浮点数比较缺陷扫描”任务。
                      </td>
                    </tr>
                  ) : (
                    paginatedRepos.map(r => (
                      <tr key={r.repo_id} style={{ borderBottom: '1px solid var(--border-color)', transition: 'background 0.2s' }} onMouseEnter={e => e.currentTarget.style.background = 'rgba(0,0,0,0.01)'} onMouseLeave={e => e.currentTarget.style.background = 'transparent'}>
                        <td style={{ ...styles.tableCell, fontWeight: 600 }}>{r.repo_name}</td>
                        <td style={styles.tableCell}>{r.department || '-'}</td>
                        <td style={styles.tableCell}>{r.owner_name}</td>
                        <td style={{ ...styles.tableCell, fontWeight: 500 }}>{r.total_issues}</td>
                        <td style={{ ...styles.tableCell, color: r.blocking > 0 ? '#ef4444' : 'inherit', fontWeight: r.blocking > 0 ? 600 : 'normal' }}>{r.blocking}</td>
                        <td style={{ ...styles.tableCell, color: r.critical > 0 ? '#f97316' : 'inherit', fontWeight: r.critical > 0 ? 600 : 'normal' }}>{r.critical}</td>
                        <td style={styles.tableCell}>
                          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', minWidth: '110px' }}>
                            <span style={{ fontSize: '0.85rem', fontWeight: 600, width: '40px' }}>{r.fix_rate.toFixed(0)}%</span>
                            <div style={{ flex: 1, height: '6px', borderRadius: '3px', background: '#e2e8f0', overflow: 'hidden' }}>
                              <div style={{ height: '100%', borderRadius: '3px', width: `${r.fix_rate}%`, background: r.fix_rate >= 85 ? '#10b981' : r.fix_rate >= 50 ? '#f59e0b' : '#ef4444' }} />
                            </div>
                          </div>
                        </td>
                        <td style={styles.tableCell}>
                          {r.last_scan_time && r.last_scan_time !== '0001-01-01T00:00:00Z' ? (
                            <span title={new Date(r.last_scan_time).toLocaleString()}>
                              {r.last_scan_time.substring(0, 10)}
                            </span>
                          ) : (
                            <span style={{ color: '#94a3b8' }}>未扫描</span>
                          )}
                        </td>
                        <td style={{ ...styles.tableCell }}>
                          <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
                            {(() => {
                              const hasNotScanned = !r.last_scan_time || r.last_scan_time === '0001-01-01T00:00:00Z';
                              return (
                                <button 
                                  disabled={hasNotScanned}
                                  onClick={() => openWorkspace(r.repo_id, r.repo_name)}
                                  title={hasNotScanned ? "代码仓未进行首次分析，无法审计" : "缺陷审计"}
                                  style={{
                                    background: 'transparent',
                                    border: 'none',
                                    color: hasNotScanned ? '#cbd5e1' : 'var(--primary-color)',
                                    cursor: hasNotScanned ? 'not-allowed' : 'pointer',
                                    padding: '0.4rem',
                                    borderRadius: '50%',
                                    display: 'inline-flex',
                                    alignItems: 'center',
                                    justifyContent: 'center',
                                    transition: 'background-color 0.2s'
                                  }}
                                  onMouseEnter={e => {
                                    if (!hasNotScanned) {
                                      e.currentTarget.style.backgroundColor = 'rgba(37, 99, 235, 0.08)';
                                    }
                                  }}
                                  onMouseLeave={e => {
                                    if (!hasNotScanned) {
                                      e.currentTarget.style.backgroundColor = 'transparent';
                                    }
                                  }}
                                >
                                  <svg width="20" height="20" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                                    <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path>
                                    <polyline points="14 2 14 8 20 8"></polyline>
                                    <circle cx="10" cy="13" r="2"></circle>
                                    <line x1="21" y1="21" x2="11.5" y2="14.8"></line>
                                  </svg>
                                </button>
                              );
                            })()}
                          </div>
                        </td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>

              {totalRepoPages > 1 && (
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '1rem 1.5rem', borderTop: '1px solid var(--border-color)', background: 'var(--card-bg)' }}>
                  <span style={{ fontSize: '0.8rem', color: '#64748b' }}>
                    显示第 {startIndex + 1} - {Math.min(startIndex + repoPageSize, repos.length)} 条，共 {repos.length} 个代码仓 (第 {repoPage} / {totalRepoPages} 页)
                  </span>
                  <div style={{ display: 'flex', gap: '0.5rem' }}>
                    <button 
                      className="btn" 
                      style={{ padding: '0.3rem 0.75rem', fontSize: '0.8rem', background: repoPage === 1 ? 'rgba(0,0,0,0.03)' : 'var(--primary-color)', color: repoPage === 1 ? '#94a3b8' : 'white', cursor: repoPage === 1 ? 'not-allowed' : 'pointer', border: repoPage === 1 ? '1px solid var(--border-color)' : 'none', borderRadius: '4px' }}
                      disabled={repoPage === 1}
                      onClick={() => setRepoPage(prev => Math.max(prev - 1, 1))}
                    >
                      上一页
                    </button>
                    <button 
                      className="btn" 
                      style={{ padding: '0.3rem 0.75rem', fontSize: '0.8rem', background: repoPage >= totalRepoPages ? 'rgba(0,0,0,0.03)' : 'var(--primary-color)', color: repoPage >= totalRepoPages ? '#94a3b8' : 'white', cursor: repoPage >= totalRepoPages ? 'not-allowed' : 'pointer', border: repoPage >= totalRepoPages ? '1px solid var(--border-color)' : 'none', borderRadius: '4px' }}
                      disabled={repoPage >= totalRepoPages}
                      onClick={() => setRepoPage(prev => Math.min(prev + 1, totalRepoPages))}
                    >
                      下一页
                    </button>
                  </div>
                </div>
              )}
            </div>
          )}

          {/* TAB 2: DEPARTMENTS RANKING */}
          {activeTab === 'depts' && (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '1.5rem' }}>
              {/* Podium */}
              {depts.length > 0 && (
                <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'flex-end', gap: '2rem', padding: '1.5rem 0', background: 'rgba(255,255,255,0.01)', borderRadius: '12px' }}>
                  {/* Silver */}
                  {depts[1] && (
                    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center' }}>
                      <div style={{ fontSize: '0.8rem', color: '#64748b', fontWeight: 600, marginBottom: '0.25rem' }}>🥈 亚军</div>
                      <div style={{ fontSize: '1rem', fontWeight: 700 }}>{depts[1].department}</div>
                      <div style={{ fontSize: '1.25rem', fontWeight: 800, color: '#f59e0b', margin: '0.25rem 0' }}>{depts[1].fix_rate.toFixed(1)}%</div>
                      <div style={{ width: '80px', height: '80px', background: 'linear-gradient(180deg, #e2e8f0 0%, #cbd5e1 100%)', borderTopLeftRadius: '6px', borderTopRightRadius: '6px', display: 'flex', alignItems: 'center', justifyContent: 'center', color: '#475569', fontSize: '1.5rem', fontWeight: 800 }}>2</div>
                    </div>
                  )}

                  {/* Gold */}
                  {depts[0] && (
                    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center' }}>
                      <div style={{ fontSize: '0.85rem', color: '#eab308', fontWeight: 600, marginBottom: '0.25rem' }}>👑 冠军</div>
                      <div style={{ fontSize: '1.15rem', fontWeight: 700 }}>{depts[0].department}</div>
                      <div style={{ fontSize: '1.5rem', fontWeight: 800, color: '#10b981', margin: '0.25rem 0' }}>{depts[0].fix_rate.toFixed(1)}%</div>
                      <div style={{ width: '100px', height: '110px', background: 'linear-gradient(180deg, #fef08a 0%, #eab308 100%)', borderTopLeftRadius: '6px', borderTopRightRadius: '6px', display: 'flex', alignItems: 'center', justifyContent: 'center', color: '#78350f', fontSize: '2rem', fontWeight: 800, boxShadow: '0 8px 16px rgba(234, 179, 8, 0.15)' }}>1</div>
                    </div>
                  )}

                  {/* Bronze */}
                  {depts[2] && (
                    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center' }}>
                      <div style={{ fontSize: '0.8rem', color: '#92400e', fontWeight: 600, marginBottom: '0.25rem' }}>🥉 季军</div>
                      <div style={{ fontSize: '1rem', fontWeight: 700 }}>{depts[2].department}</div>
                      <div style={{ fontSize: '1.25rem', fontWeight: 800, color: '#ea580c', margin: '0.25rem 0' }}>{depts[2].fix_rate.toFixed(1)}%</div>
                      <div style={{ width: '80px', height: '60px', background: 'linear-gradient(180deg, #ffedd5 0%, #fed7aa 100%)', borderTopLeftRadius: '6px', borderTopRightRadius: '6px', display: 'flex', alignItems: 'center', justifyContent: 'center', color: '#9a3412', fontSize: '1.5rem', fontWeight: 800 }}>3</div>
                    </div>
                  )}
                </div>
              )}

              {/* Department Ranking Table */}
              <div className="card" style={{ padding: 0, overflowX: 'auto', border: '1px solid var(--border-color)' }}>
                <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                  <thead>
                    <tr style={{ background: 'var(--bg-color)', borderBottom: '1px solid var(--border-color)' }}>
                      <th style={styles.tableHeaderDept}>名次</th>
                      <th style={styles.tableHeaderDept}>部门名称</th>
                      <th onClick={() => handleSort('total_issues')} style={styles.tableHeaderDept}>缺陷暴露数</th>
                      <th onClick={() => handleSort('fix_rate')} style={styles.tableHeaderDept}>整改就绪率</th>
                    </tr>
                  </thead>
                  <tbody>
                    {depts.map((d, index) => (
                      <tr key={d.department} style={{ borderBottom: '1px solid var(--border-color)' }}>
                        <td style={styles.tableCellDept}><strong>{index + 1}</strong></td>
                        <td style={styles.tableCellDept}>{d.department}</td>
                        <td style={styles.tableCellDept}>{d.total_issues}</td>
                        <td style={styles.tableCellDept}>
                          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                            <span style={{ fontSize: '0.85rem', fontWeight: 600 }}>{d.fix_rate.toFixed(1)}%</span>
                            <div style={{ width: '100px', height: '6px', borderRadius: '3px', background: '#e2e8f0', overflow: 'hidden' }}>
                              <div style={{ height: '100%', borderRadius: '3px', width: `${d.fix_rate}%`, background: d.fix_rate >= 85 ? '#10b981' : d.fix_rate >= 50 ? '#f59e0b' : '#ef4444' }} />
                            </div>
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {/* TAB 3: TRENDS ANALYSIS */}
          {activeTab === 'trends' && (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '1.5rem' }}>
              <div style={{ display: 'flex', gap: '1rem', flexWrap: 'wrap' }}>
                <select 
                  value={trendScope}
                  onChange={e => {
                    setTrendScope(e.target.value as any);
                    setTrendRepoId('');
                    setTrendDeptName('');
                  }}
                  style={styles.selectInput}
                >
                  <option value="global">全局趋势</option>
                  <option value="repo">按代码仓</option>
                  <option value="dept">按部门</option>
                </select>

                {trendScope === 'repo' && (
                  <select 
                    value={trendRepoId}
                    onChange={e => setTrendRepoId(e.target.value)}
                    style={styles.selectInput}
                  >
                    <option value="">-- 选择代码仓 --</option>
                    {repos.map(r => <option key={r.repo_id} value={r.repo_id}>{r.repo_name}</option>)}
                  </select>
                )}

                {trendScope === 'dept' && (
                  <select 
                    value={trendDeptName}
                    onChange={e => setTrendDeptName(e.target.value)}
                    style={styles.selectInput}
                  >
                    <option value="">-- 选择归属部门 --</option>
                    {departments.map(d => <option key={d} value={d}>{d}</option>)}
                  </select>
                )}
              </div>

              {trends.length === 0 ? (
                <div className="card" style={{ textAlign: 'center', padding: '4rem', color: '#94a3b8' }}>暂无趋势收敛数据</div>
              ) : (
                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
                  <div className="card">
                    <h4 style={{ margin: '0 0 1rem 0', fontSize: '0.9rem', fontWeight: 600 }}>浮点比较缺陷总量收敛曲线</h4>
                    {renderTrendChart(trends, 'issues')}
                  </div>
                  <div className="card">
                    <h4 style={{ margin: '0 0 1rem 0', fontSize: '0.9rem', fontWeight: 600 }}>专项缺陷整体整改率走势</h4>
                    {renderTrendChart(trends, 'rate')}
                  </div>
                </div>
              )}
            </div>
          )}
        </>
      )}

      {/* 4. DEFECT WORKSPACE AUDITING DRAWER */}
      {workspaceOpen && (
        <div style={{
          position: 'fixed',
          top: 0,
          right: 0,
          bottom: 0,
          width: '85vw',
          background: 'var(--card-bg)',
          boxShadow: '-10px 0 25px -5px rgba(0,0,0,0.15)',
          zIndex: 100,
          display: 'flex',
          flexDirection: 'column',
          borderLeft: '1px solid var(--border-color)',
          animation: 'slideIn 0.3s ease-out'
        }}>
          <style>{`@keyframes slideIn { from { transform: translateX(100%); } to { transform: translateX(0); } }`}</style>
          
          {/* Workspace Header */}
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '1rem 1.5rem', borderBottom: '1px solid var(--border-color)' }}>
            <div>
              <span style={{ fontSize: '0.75rem', textTransform: 'uppercase', color: 'var(--primary-color)', fontWeight: 700, letterSpacing: '0.05em' }}>代码仓缺陷审计工作区</span>
              <h3 style={{ margin: '0.1rem 0 0 0', fontSize: '1.2rem', fontWeight: 700 }}>📁 {selectedRepoName}</h3>
            </div>
            <button 
              className="btn btn-outline" 
              onClick={() => { setWorkspaceOpen(false); setEditingFinding(null); }}
              style={{ padding: '0.35rem 0.8rem', fontSize: '0.85rem' }}
            >
              关闭工作区
            </button>
          </div>

          {/* Workspace Content */}
          <div style={{ flex: 1, display: 'flex', overflow: 'hidden' }}>
            {/* Left side list */}
            <div style={{ width: '40%', borderRight: '1px solid var(--border-color)', display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
              <div style={{ padding: '1rem', borderBottom: '1px solid var(--border-color)', display: 'flex', flexDirection: 'column', gap: '0.75rem', background: 'rgba(0,0,0,0.01)' }}>
                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.5rem' }}>
                  <select 
                    value={wsSeverity}
                    onChange={e => handleWorkspaceFilterChange('severity', e.target.value)}
                    style={{ padding: '0.35rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--card-bg)', fontSize: '0.8rem', outline: 'none' }}
                  >
                    <option value="">所有影响等级</option>
                    <option value="阻塞">阻塞</option>
                    <option value="严重">严重</option>
                    <option value="主要">主要</option>
                    <option value="提示">提示</option>
                    <option value="建议">建议</option>
                  </select>
                  <select 
                    value={wsStatus}
                    onChange={e => handleWorkspaceFilterChange('status', e.target.value)}
                    style={{ padding: '0.35rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--card-bg)', fontSize: '0.8rem', outline: 'none' }}
                  >
                    <option value="">所有审计状态</option>
                    <option value="open">待处理 (Open)</option>
                    <option value="analyzing">问题分析 (Analyzing)</option>
                    <option value="resolved">已解决 (Resolved)</option>
                    <option value="closed">已关闭 (Closed)</option>
                    <option value="invalid">忽略/误报 (Invalid)</option>
                  </select>
                </div>
                <input 
                  type="text" 
                  placeholder="过滤文件路径/描述/标题..."
                  value={wsKeyword}
                  onChange={e => handleWorkspaceFilterChange('keyword', e.target.value)}
                  style={{ padding: '0.35rem 0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--card-bg)', fontSize: '0.8rem', outline: 'none' }}
                />
              </div>

              {/* Findings scroll area */}
              <div style={{ flex: 1, overflowY: 'auto', padding: '0.5rem' }}>
                {workspaceFindings.length === 0 ? (
                  <div style={{ padding: '4rem 1rem', textAlign: 'center', color: '#94a3b8', fontSize: '0.85rem' }}>未搜索到缺陷结果</div>
                ) : (
                  workspaceFindings.map(f => (
                    <div 
                      key={f.id || f.ID}
                      onClick={() => startWorkflow(f)}
                      style={{
                        padding: '1rem',
                        borderRadius: '6px',
                        border: editingFinding?.ID === f.ID ? '1px solid var(--primary-color)' : '1px solid var(--border-color)',
                        background: editingFinding?.ID === f.ID ? 'rgba(37, 99, 235, 0.03)' : 'var(--card-bg)',
                        cursor: 'pointer',
                        marginBottom: '0.5rem',
                        transition: 'all 0.2s',
                        textAlign: 'left'
                      }}
                      onMouseEnter={e => {
                        if (editingFinding?.ID !== f.ID) e.currentTarget.style.borderColor = '#cbd5e1';
                      }}
                      onMouseLeave={e => {
                        if (editingFinding?.ID !== f.ID) e.currentTarget.style.borderColor = 'var(--border-color)';
                      }}
                    >
                      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: '0.5rem' }}>
                        <span style={{ 
                          fontSize: '0.7rem', 
                          padding: '0.1rem 0.35rem', 
                          borderRadius: '3px', 
                          fontWeight: 600,
                          background: f.severity === '阻塞' || f.severity === '严重' ? '#fee2e2' : '#fef3c7',
                          color: f.severity === '阻塞' || f.severity === '严重' ? '#ef4444' : '#d97706'
                        }}>
                          {f.severity}
                        </span>
                        <span style={{
                          fontSize: '0.7rem',
                          padding: '0.1rem 0.35rem',
                          borderRadius: '3px',
                          fontWeight: 600,
                          background: f.status === 'resolved' || f.status === 'closed' ? '#d1fae5' : '#f3f4f6',
                          color: f.status === 'resolved' || f.status === 'closed' ? '#10b981' : '#6b7280'
                        }}>
                          {f.status === 'open' ? '待处理' : f.status === 'analyzing' ? '分析中' : f.status === 'resolved' ? '已解决' : f.status === 'closed' ? '已关闭' : '忽略'}
                        </span>
                      </div>
                      <h4 style={{ margin: '0.5rem 0 0.25rem 0', fontSize: '0.85rem', fontWeight: 600, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{f.title}</h4>
                      <p style={{ margin: 0, fontSize: '0.75rem', color: '#64748b', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{f.file_path}:{f.line_number}</p>
                      
                      {/* Assignee label */}
                      {f.assignee && (
                        <div style={{ fontSize: '0.75rem', color: '#64748b', marginTop: '0.5rem', display: 'flex', alignItems: 'center', gap: '0.25rem' }}>
                          👤 处理人: <strong>{f.assignee.name}</strong>
                        </div>
                      )}
                    </div>
                  ))
                )}
              </div>

              {/* Workspace Paginated Footer */}
              {workspaceTotalPages > 1 && (
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '1rem', borderTop: '1px solid var(--border-color)', background: 'rgba(0,0,0,0.01)' }}>
                  <span style={{ fontSize: '0.8rem', color: '#64748b' }}>第 {workspacePage} / {workspaceTotalPages} 页</span>
                  <div style={{ display: 'flex', gap: '0.5rem' }}>
                    <button 
                      className="btn btn-outline" 
                      style={{ padding: '0.3rem 0.75rem', fontSize: '0.8rem' }}
                      disabled={workspacePage === 1}
                      onClick={() => handleWorkspacePageChange(workspacePage - 1)}
                    >
                      上一页
                    </button>
                    <button 
                      className="btn btn-outline" 
                      style={{ padding: '0.3rem 0.75rem', fontSize: '0.8rem' }}
                      disabled={workspacePage >= workspaceTotalPages}
                      onClick={() => handleWorkspacePageChange(workspacePage + 1)}
                    >
                      下一页
                    </button>
                  </div>
                </div>
              )}
            </div>

            {/* Right side audit details */}
            <div style={{ width: '60%', display: 'flex', flexDirection: 'column', overflowY: 'auto', padding: '1.5rem', gap: '1.5rem' }}>
              {editingFinding ? (
                <>
                  {/* Defect Context Header */}
                  <div>
                    <h3 style={{ margin: '0 0 0.5rem 0', fontSize: '1.1rem', fontWeight: 700, color: '#ef4444', textAlign: 'left' }}>❌ {editingFinding.title}</h3>
                    <div style={{ display: 'flex', gap: '0.75rem', flexWrap: 'wrap' }}>
                      <div style={{ fontSize: '0.8rem', color: '#64748b', background: 'var(--bg-color)', padding: '0.5rem 0.75rem', borderRadius: '4px' }}>
                        📁 <strong>文件:</strong> {editingFinding.file_path}:{editingFinding.line_number}
                      </div>
                      <div style={{ fontSize: '0.8rem', color: '#64748b', background: 'var(--bg-color)', padding: '0.5rem 0.75rem', borderRadius: '4px' }}>
                        🔖 <strong>归属类别:</strong> {editingFinding.category || '精度逻辑比较'}
                      </div>
                    </div>
                  </div>

                  {/* Detail Description */}
                  <div>
                    <h4 style={{ margin: '0 0 0.5rem 0', fontSize: '0.9rem', fontWeight: 600, textAlign: 'left' }}>缺陷详情</h4>
                    <p style={{ margin: 0, fontSize: '0.85rem', color: 'var(--text-color)', textAlign: 'left', lineHeight: 1.5, background: 'rgba(239, 68, 68, 0.03)', border: '1px solid rgba(239, 68, 68, 0.1)', padding: '1rem', borderRadius: '6px' }}>
                      {editingFinding.detail}
                    </p>
                  </div>

                  {/* Code Snippet */}
                  {editingFinding.code_snippet && (
                    <div>
                      <h4 style={{ margin: '0 0 0.5rem 0', fontSize: '0.9rem', fontWeight: 600, textAlign: 'left' }}>缺陷代码片段</h4>
                      <pre style={{ margin: 0, padding: '1rem', background: '#0f172a', color: '#e2e8f0', borderRadius: '6px', fontSize: '0.8rem', fontFamily: 'Fira Code, Consolas, Monaco, monospace', overflowX: 'auto', border: '1px solid #1e293b', lineHeight: 1.4, textAlign: 'left' }}>
                        <code>{editingFinding.code_snippet}</code>
                      </pre>
                    </div>
                  )}

                  {/* Suggestions */}
                  <div>
                    <h4 style={{ margin: '0 0 0.5rem 0', fontSize: '0.9rem', fontWeight: 600, textAlign: 'left' }}>修复改进建议</h4>
                    <div style={{ margin: 0, padding: '1rem', background: 'rgba(16, 185, 129, 0.03)', border: '1px solid rgba(16, 185, 129, 0.1)', borderRadius: '6px', fontSize: '0.85rem', color: 'var(--text-color)', lineHeight: 1.5, whiteSpace: 'pre-wrap', textAlign: 'left' }}>
                      {editingFinding.suggestion}
                    </div>
                  </div>

                  {/* Audit Form */}
                  <div style={{ borderTop: '1px solid var(--border-color)', paddingTop: '1.5rem' }}>
                    <h4 style={{ margin: '0 0 1rem 0', fontSize: '0.9rem', fontWeight: 600, textAlign: 'left' }}>缺陷流转与认领审计</h4>
                    <form onSubmit={submitWorkflow} style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
                      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
                        <div>
                          <label style={{ display: 'block', fontSize: '0.8rem', color: '#64748b', marginBottom: '0.25rem', textAlign: 'left' }}>处理人/领受人</label>
                          <div style={{ position: 'relative' }}>
                            <input 
                              type="text"
                              placeholder="🔍 搜索责任人 (姓名/工号/部门)..."
                              value={assigneeSearch}
                              onFocus={() => setDropdownOpen(true)}
                              onChange={e => {
                                setAssigneeSearch(e.target.value);
                                setDropdownOpen(true);
                              }}
                              style={{
                                width: '100%',
                                padding: '0.4rem 0.6rem',
                                borderRadius: '4px',
                                border: '1px solid var(--border-color)',
                                background: 'var(--card-bg)',
                                color: 'var(--text-color)',
                                fontSize: '0.85rem',
                                boxSizing: 'border-box',
                                outline: 'none'
                              }}
                            />
                            {dropdownOpen && (
                              <>
                                <div 
                                  onClick={() => {
                                    setDropdownOpen(false);
                                    const current = members.find(m => (m.id || m.ID) === workflowAssignee);
                                    setAssigneeSearch(current ? `${current.name} (${current.id || current.ID})` : '');
                                  }} 
                                  style={{ position: 'fixed', top: 0, left: 0, right: 0, bottom: 0, zIndex: 1000 }} 
                                />
                                <div style={{
                                  position: 'absolute',
                                  top: '100%',
                                  left: 0,
                                  width: '100%',
                                  maxHeight: '200px',
                                  overflowY: 'auto',
                                  background: 'var(--card-bg)',
                                  border: '1px solid var(--border-color)',
                                  borderRadius: '4px',
                                  boxShadow: '0 10px 15px -3px rgba(0,0,0,0.1)',
                                  zIndex: 1001,
                                  marginTop: '4px'
                                }}>
                                  <div 
                                    onClick={() => {
                                      setWorkflowAssignee('');
                                      setAssigneeSearch('');
                                      setDropdownOpen(false);
                                    }}
                                    style={{
                                      padding: '0.5rem 0.75rem',
                                      cursor: 'pointer',
                                      fontSize: '0.825rem',
                                      color: '#64748b',
                                      borderBottom: '1px solid var(--border-color)',
                                      textAlign: 'left'
                                    }}
                                    onMouseEnter={e => e.currentTarget.style.backgroundColor = 'var(--bg-color)'}
                                    onMouseLeave={e => e.currentTarget.style.backgroundColor = 'transparent'}
                                  >
                                    -- 未指定 --
                                  </div>
                                  {members.filter(m => {
                                    const name = m.name || '';
                                    const id = m.id || m.ID || '';
                                    const dept = m.department || '';
                                    const q = assigneeSearch.toLowerCase();
                                    return name.toLowerCase().includes(q) || id.toLowerCase().includes(q) || dept.toLowerCase().includes(q);
                                  }).slice(0, 50).map(m => {
                                    const mId = m.id || m.ID;
                                    return (
                                      <div 
                                        key={mId}
                                        onClick={() => {
                                          setWorkflowAssignee(mId);
                                          setAssigneeSearch(`${m.name} (${mId})`);
                                          setDropdownOpen(false);
                                        }}
                                        style={{
                                          padding: '0.5rem 0.75rem',
                                          cursor: 'pointer',
                                          fontSize: '0.825rem',
                                          borderBottom: '1px solid var(--border-color)',
                                          background: workflowAssignee === mId ? 'rgba(37, 99, 235, 0.05)' : 'transparent',
                                          fontWeight: workflowAssignee === mId ? 600 : 400,
                                          color: 'var(--text-color)',
                                          textAlign: 'left'
                                        }}
                                        onMouseEnter={e => e.currentTarget.style.backgroundColor = 'var(--bg-color)'}
                                        onMouseLeave={e => e.currentTarget.style.backgroundColor = 'transparent'}
                                      >
                                        <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                                          <strong>{m.name}</strong>
                                          <span style={{ color: '#94a3b8', fontSize: '0.75rem' }}>{mId}</span>
                                        </div>
                                        <div style={{ fontSize: '0.7rem', color: '#64748b', marginTop: '0.15rem' }}>{m.department || '未分配部门'}</div>
                                      </div>
                                    );
                                  })}
                                </div>
                              </>
                            )}
                          </div>
                        </div>

                        <div>
                          <label style={{ display: 'block', fontSize: '0.8rem', color: '#64748b', marginBottom: '0.25rem', textAlign: 'left' }}>治理审计状态</label>
                          <select 
                            value={workflowStatus} 
                            onChange={e => setWorkflowStatus(e.target.value)}
                            style={{ width: '100%', padding: '0.4rem 0.6rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--card-bg)', fontSize: '0.85rem', outline: 'none' }}
                          >
                            <option value="open">待处理 (Open)</option>
                            <option value="analyzing">问题分析 (Analyzing)</option>
                            <option value="resolved">已解决 (Resolved)</option>
                            <option value="closed">已关闭 (Closed)</option>
                            <option value="invalid">忽略/误报 (Invalid)</option>
                          </select>
                        </div>
                      </div>

                      <div>
                        <label style={{ display: 'block', fontSize: '0.8rem', color: '#64748b', marginBottom: '0.25rem', textAlign: 'left' }}>审计说明与跟踪意见</label>
                        <textarea 
                          rows={3}
                          placeholder="输入您对缺陷分析的结论或验证关闭意见..."
                          value={workflowComment}
                          onChange={e => setWorkflowComment(e.target.value)}
                          style={{ width: '100%', padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--card-bg)', fontSize: '0.85rem', outline: 'none', resize: 'vertical', fontFamily: 'inherit', boxSizing: 'border-box' }}
                        />
                      </div>

                      <button type="submit" className="btn" style={{ alignSelf: 'flex-end', padding: '0.5rem 1.5rem', fontSize: '0.85rem' }}>
                        保存审计记录
                      </button>
                    </form>
                  </div>

                  {/* Status Change log timeline */}
                  <div style={{ borderTop: '1px solid var(--border-color)', paddingTop: '1.5rem' }}>
                    <h4 style={{ margin: '0 0 1rem 0', fontSize: '0.9rem', fontWeight: 600, textAlign: 'left' }}>状态演进流转历史</h4>
                    <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem', textAlign: 'left' }}>
                      {editingFinding.status_log ? (() => {
                        try {
                          const logs = JSON.parse(editingFinding.status_log);
                          if (!Array.isArray(logs) || logs.length === 0) {
                            return <div style={{ fontSize: '0.8rem', color: '#94a3b8' }}>暂无流转记录</div>;
                          }
                          return logs.map((log: any, idx: number) => (
                            <div key={idx} style={{ position: 'relative', paddingLeft: '1.5rem', borderLeft: '2px solid #e2e8f0', paddingBottom: '0.5rem' }}>
                              <div style={{ position: 'absolute', left: '-6px', top: '2px', width: '10px', height: '10px', borderRadius: '50%', background: '#3b82f6' }} />
                              <div style={{ fontSize: '0.8rem', fontWeight: 600 }}>{log.status === 'open' ? '待处理' : log.status === 'analyzing' ? '分析中' : log.status === 'resolved' ? '已解决' : log.status === 'closed' ? '已关闭' : '忽略/误报'}</div>
                              <div style={{ fontSize: '0.75rem', color: '#64748b', marginTop: '0.1rem' }}>
                                操作人: <strong>{log.user}</strong> &bull; 时间: {log.time}
                              </div>
                              {log.comment && (
                                <div style={{ fontSize: '0.75rem', color: 'var(--text-color)', background: 'rgba(0,0,0,0.02)', padding: '0.4rem 0.6rem', borderRadius: '4px', marginTop: '0.25rem' }}>
                                  {log.comment}
                                </div>
                              )}
                            </div>
                          ));
                        } catch (err) {
                          return <div style={{ fontSize: '0.8rem', color: '#94a3b8' }}>历史日志读取异常</div>;
                        }
                      })() : (
                        <div style={{ fontSize: '0.8rem', color: '#94a3b8' }}>暂无流转记录</div>
                      )}
                    </div>
                  </div>
                </>
              ) : (
                <div style={{ display: 'flex', flexDirection: 'column', justifyContent: 'center', alignItems: 'center', height: '70%', color: '#94a3b8', gap: '0.5rem' }}>
                  <svg width="48" height="48" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="1.5">
                    <path d="M15 15l-2 5L9 9l11 4-5 2zm0 0l5 5M7.188 2.239a9 9 0 0112.573 12.573M5.12 5.12a9 9 0 0012.57 12.57M1.5 1.5l21 21" strokeLinecap="round" strokeLinejoin="round" />
                  </svg>
                  <span>请从左侧列表选择浮点数缺陷开始安全审计</span>
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );

  // SVG Helper
  function renderTrendChart(points: any[], metric: 'issues' | 'rate') {
    const width = 500;
    const height = 220;
    const paddingTop = 20;
    const paddingBottom = 40;
    const paddingLeft = 45;
    const paddingRight = 20;

    const chartWidth = width - paddingLeft - paddingRight;
    const chartHeight = height - paddingTop - paddingBottom;

    if (points.length === 0) return null;

    let maxVal = 10;
    if (metric === 'issues') {
      const vals = points.map(p => p.total_issues);
      maxVal = Math.max(...vals, 10);
    } else {
      maxVal = 100;
    }

    // Grid values
    const gridLines = 4;
    const yGrid = Array.from({ length: gridLines + 1 }, (_, i) => i * (maxVal / gridLines));

    // SVG coordinates builder
    const coords = points.map((p, i) => {
      const x = paddingLeft + (i * chartWidth) / (points.length - 1 || 1);
      const val = metric === 'issues' ? p.total_issues : p.fix_rate;
      const y = paddingTop + chartHeight - (val * chartHeight) / maxVal;
      return { x, y, point: p };
    });

    const linePath = coords.reduce((acc, c, i) => {
      return acc + (i === 0 ? `M ${c.x} ${c.y}` : ` L ${c.x} ${c.y}`);
    }, '');

    const areaPath = coords.reduce((acc, c, i) => {
      if (i === 0) {
        return `M ${c.x} ${paddingTop + chartHeight} L ${c.x} ${c.y}`;
      }
      if (i === coords.length - 1) {
        return acc + ` L ${c.x} ${c.y} L ${c.x} ${paddingTop + chartHeight} Z`;
      }
      return acc + ` L ${c.x} ${c.y}`;
    }, '');

    const strokeColor = metric === 'issues' ? '#ef4444' : '#10b981';
    const gradientId = metric === 'issues' ? 'redGradient' : 'greenGradient';
    const fillGradient = metric === 'issues' ? 'rgba(239, 68, 68, 0.1)' : 'rgba(16, 185, 129, 0.1)';

    return (
      <div style={{ position: 'relative' }}>
        <svg width="100%" height={height} viewBox={`0 0 ${width} ${height}`} style={{ background: 'transparent', overflow: 'visible' }}>
          <defs>
            <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={strokeColor} stopOpacity={0.25} />
              <stop offset="100%" stopColor={strokeColor} stopOpacity={0.0} />
            </linearGradient>
          </defs>

          {/* Grid lines & Y Axis Labels */}
          {yGrid.map((val, idx) => {
            const y = paddingTop + chartHeight - (idx * chartHeight) / gridLines;
            return (
              <g key={idx}>
                <line x1={paddingLeft} y1={y} x2={width - paddingRight} y2={y} stroke="var(--border-color)" strokeWidth="1" strokeDasharray="3,3" />
                <text x={paddingLeft - 8} y={y + 4} textAnchor="end" fontSize="10" fill="#64748b">
                  {metric === 'issues' ? val.toFixed(0) : `${val.toFixed(0)}%`}
                </text>
              </g>
            );
          })}

          {/* Area & Line */}
          {points.length > 1 && (
            <>
              <path d={areaPath} fill={`url(#${gradientId})`} />
              <path d={linePath} fill="none" stroke={strokeColor} strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" />
            </>
          )}

          {/* Interactive Nodes */}
          {coords.map((c, i) => (
            <g key={i}>
              <circle cx={c.x} cy={c.y} r="4" fill={strokeColor} stroke="white" strokeWidth="2" />
              {i % 6 === 0 && (
                <text x={c.x} y={height - 8} textAnchor="middle" fontSize="9" fill="#64748b">
                  {c.point.date.substring(5)}
                </text>
              )}
            </g>
          ))}
        </svg>
      </div>
    );
  }
}

const styles = {
  tableHeader: {
    padding: '0.75rem 1rem',
    fontSize: '0.8rem',
    fontWeight: 600,
    color: '#64748b',
    textAlign: 'left' as const,
    borderBottom: '1px solid var(--border-color)',
    cursor: 'pointer',
    userSelect: 'none' as const
  },
  tableCell: {
    padding: '0.85rem 1rem',
    fontSize: '0.85rem',
    textAlign: 'left' as const
  },
  tableHeaderDept: {
    padding: '0.75rem 1.5rem',
    fontSize: '0.8rem',
    fontWeight: 600,
    color: '#64748b',
    textAlign: 'left' as const,
    borderBottom: '1px solid var(--border-color)'
  },
  tableCellDept: {
    padding: '1rem 1.5rem',
    fontSize: '0.85rem',
    textAlign: 'left' as const
  },
  selectInput: {
    padding: '0.4rem 0.6rem',
    borderRadius: '4px',
    border: '1px solid var(--border-color)',
    background: 'var(--card-bg)',
    color: 'var(--text-color)',
    fontSize: '0.85rem',
    outline: 'none',
    minWidth: '150px'
  }
};
