import React, { useEffect, useState } from 'react';
import { apiUrl } from '../config';
import { useToast } from '../components/Toast';
import AuditingWorkspace from '../components/AuditingWorkspace';

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
  const [sortField, setSortField] = useState('total_issues');
  const [sortOrder, setSortOrder] = useState<'asc' | 'desc'>('desc');
  
  // Trend dimension
  const [trendScope, setTrendScope] = useState<'global' | 'repo' | 'dept'>('global');
  const [trendRepoId, setTrendRepoId] = useState('');
  const [trendDeptName, setTrendDeptName] = useState('');
  
  // Audit Workspace Panel
  const [workspaceOpen, setWorkspaceOpen] = useState(false);
  const [selectedRepoId, setSelectedRepoId] = useState<number | null>(null);
  const [selectedRepoName, setSelectedRepoName] = useState('');
  
  const [floatTaskTypeId, setFloatTaskTypeId] = useState<number | null>(null);

  // Initialize and load core resources
  useEffect(() => {
    fetchTaskTypeId();
    fetchDepartments();
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
    setWorkspaceOpen(true);
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
            onClick={() => { setActiveTab('repos'); setSortField('total_issues'); }}
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
      <AuditingWorkspace 
        isOpen={workspaceOpen}
        onClose={() => setWorkspaceOpen(false)}
        repoId={selectedRepoId || 0}
        repoName={selectedRepoName}
        apiPrefix="/api/analysis/float"
        workspaceType="float"
        onWorkflowSaved={fetchReposData}
      />
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
