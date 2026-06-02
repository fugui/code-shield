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

interface CampaignAnalysisProps {
  campaign: 'coredump' | 'float' | 'thread' | 'cjson';
  title: string;
  description: string;
  taskTypeName: string;
}

export default function CampaignAnalysis({ campaign, title, description, taskTypeName }: CampaignAnalysisProps) {
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
  
  const [taskTypeId, setTaskTypeId] = useState<number | null>(null);

  // Initialize and load core resources
  useEffect(() => {
    fetchTaskTypeId();
    fetchDepartments();
  }, [campaign, taskTypeName]);

  // Sync content based on tab and filters
  useEffect(() => {
    if (activeTab === 'repos') {
      fetchReposData();
    } else if (activeTab === 'depts') {
      fetchDeptsData();
    } else if (activeTab === 'trends') {
      fetchTrendsData();
    }
  }, [activeTab, selectedDept, sortField, sortOrder, trendScope, trendRepoId, trendDeptName, campaign]);

  const fetchDepartments = () => {
    fetch(apiUrl(`/api/analysis/${campaign}/departments`))
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
        const list = Array.isArray(data) ? data : (data.items || []);
        const targetTask = list.find((t: any) => t.name === taskTypeName);
        if (targetTask) {
          setTaskTypeId(targetTask.id || targetTask.ID);
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

    fetch(apiUrl(`/api/analysis/${campaign}/repos?${params.toString()}`))
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
    fetch(apiUrl(`/api/analysis/${campaign}/departments?${params.toString()}`))
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

    fetch(apiUrl(`/api/analysis/${campaign}/trends?${params.toString()}`))
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

  // Open Workspace for a specific Repository
  const openWorkspace = (repoId: number, repoName: string) => {
    setSelectedRepoId(repoId);
    setSelectedRepoName(repoName);
    setWorkspaceOpen(true);
  };

  // Sorting columns
  const handleSort = (field: string) => {
    if (sortField === field) {
      setSortOrder(sortOrder === 'asc' ? 'desc' : 'asc');
    } else {
      setSortField(field);
      setSortOrder('desc');
    }
  };

  // Inline dynamic styles
  const styles = {
    badge: (severity: string) => {
      let bg = 'rgba(100, 116, 139, 0.1)';
      let color = '#64748b';
      switch (severity) {
        case '合格':
          bg = 'rgba(16, 185, 129, 0.12)';
          color = '#10b981';
          break;
        case '阻塞':
          bg = 'rgba(239, 68, 68, 0.12)';
          color = '#ef4444';
          break;
        case '严重':
          bg = 'rgba(249, 115, 22, 0.12)';
          color = '#f97316';
          break;
        case '主要':
          bg = 'rgba(234, 179, 8, 0.12)';
          color = '#eab308';
          break;
        case '提示':
        case '建议':
          bg = 'rgba(59, 130, 246, 0.12)';
          color = '#3b82f6';
          break;
      }
      return {
        display: 'inline-flex',
        alignItems: 'center',
        padding: '0.25rem 0.5rem',
        borderRadius: '4px',
        fontSize: '0.75rem',
        fontWeight: 600,
        backgroundColor: bg,
        color: color,
      };
    },
    statusBadge: (status: string) => {
      let bg = 'var(--border-color)';
      let label = '待处理';
      let color = 'var(--text-color)';
      if (status === 'open') { bg = '#fee2e2'; label = '待处理'; color = '#991b1b'; }
      else if (status === 'analyzing') { bg = '#fef3c7'; label = '问题分析'; color = '#92400e'; }
      else if (status === 'resolved') { bg = '#d1fae5'; label = '已解决'; color = '#065f46'; }
      else if (status === 'closed') { bg = '#f3f4f6'; label = '已关闭'; color = '#374151'; }
      else if (status === 'invalid') { bg = '#e0f2fe'; label = '忽略/误报'; color = '#075985'; }
      
      return {
        padding: '0.2rem 0.5rem',
        borderRadius: '4px',
        fontSize: '0.75rem',
        fontWeight: 500,
        background: bg,
        color: color,
        display: 'inline-block',
      };
    },
    tableHeader: {
      padding: '0.75rem 1rem',
      fontWeight: 600,
      textAlign: 'left' as const,
      borderBottom: '1px solid var(--border-color)',
      color: '#64748b',
      fontSize: '0.85rem',
      cursor: 'pointer',
    },
    tableCell: {
      padding: '0.875rem 1rem',
      borderBottom: '1px solid var(--border-color)',
      fontSize: '0.875rem',
    },
  };

  // Render SVG charts dynamically based on trends data
  const renderTrendChart = () => {
    if (trends.length === 0) {
      return (
        <div style={{ padding: '4rem 1rem', textAlign: 'center', color: '#94a3b8' }}>
          暂无足够的历史扫描数据来绘制缺陷走势。请在完成扫描任务后查看。
        </div>
      );
    }

    const width = 800;
    const height = 300;
    const paddingLeft = 50;
    const paddingRight = 20;
    const paddingTop = 30;
    const paddingBottom = 40;

    const chartWidth = width - paddingLeft - paddingRight;
    const chartHeight = height - paddingTop - paddingBottom;

    // Resolve max values
    const maxIssues = Math.max(...trends.map(t => t.total_issues), 10);

    // Compute SVG path string for Total Issues Line
    let linePath = '';
    let areaPath = '';
    
    trends.forEach((t, i) => {
      const x = paddingLeft + (i * chartWidth) / (trends.length - 1 || 1);
      const y = paddingTop + chartHeight - (t.total_issues * chartHeight) / maxIssues;
      
      if (i === 0) {
        linePath = `M ${x} ${y}`;
        areaPath = `M ${x} ${paddingTop + chartHeight} L ${x} ${y}`;
      } else {
        linePath += ` L ${x} ${y}`;
        areaPath += ` L ${x} ${y}`;
      }
      
      if (i === trends.length - 1) {
        areaPath += ` L ${x} ${paddingTop + chartHeight} Z`;
      }
    });

    return (
      <div style={{ background: 'var(--card-bg)', padding: '1.5rem', borderRadius: '8px', border: '1px solid var(--border-color)', position: 'relative' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: '1.5rem', alignItems: 'center' }}>
          <h4 style={{ margin: 0, fontSize: '1rem', fontWeight: 600 }}>{title} 存量缺陷收敛趋势 (过去30天)</h4>
          <div style={{ display: 'flex', gap: '1rem', fontSize: '0.8rem' }}>
            <span style={{ display: 'inline-flex', alignItems: 'center', gap: '0.4rem' }}>
              <span style={{ width: '12px', height: '12px', borderRadius: '2px', background: 'rgba(239, 68, 68, 0.2)', border: '2px solid #ef4444', display: 'inline-block' }} />
              未整改缺陷数
            </span>
          </div>
        </div>

        <div style={{ overflowX: 'auto' }}>
          <svg viewBox={`0 0 ${width} ${height}`} width="100%" height="320" style={{ overflow: 'visible' }}>
            <defs>
              <linearGradient id="issueGradient" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor="#ef4444" stopOpacity="0.2"/>
                <stop offset="100%" stopColor="#ef4444" stopOpacity="0.0"/>
              </linearGradient>
            </defs>

            {/* Horizontal Grid lines */}
            {[0, 25, 50, 75, 100].map((percent) => {
              const val = Math.round((percent * maxIssues) / 100);
              const y = paddingTop + chartHeight - (percent * chartHeight) / 100;
              return (
                <g key={percent}>
                  <line x1={paddingLeft} y1={y} x2={width - paddingRight} y2={y} stroke="var(--border-color)" strokeWidth="1" strokeDasharray="4 4" />
                  <text x={paddingLeft - 8} y={y + 4} textAnchor="end" fontSize="10" fill="#94a3b8">{val}</text>
                </g>
              );
            })}

            {/* Area under line */}
            {trends.length > 1 && (
              <path d={areaPath} fill="url(#issueGradient)" />
            )}

            {/* Trend Line */}
            {trends.length > 1 && (
              <path d={linePath} fill="none" stroke="#ef4444" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" />
            )}

            {/* Circular points */}
            {trends.map((t, i) => {
              const x = paddingLeft + (i * chartWidth) / (trends.length - 1 || 1);
              const y = paddingTop + chartHeight - (t.total_issues * chartHeight) / maxIssues;

              return (
                <g key={i}>
                  <circle cx={x} cy={y} r="5" fill="#ef4444" stroke="white" strokeWidth="2" />
                  {/* Date labels */}
                  {i % Math.max(Math.round(trends.length / 6), 1) === 0 && (
                    <text x={x} y={height - 12} textAnchor="middle" fontSize="10" fill="#64748b" transform={`rotate(-15, ${x}, ${height - 12})`}>
                      {t.date.substring(5)}
                    </text>
                  )}
                  {/* Values */}
                  <text x={x} y={y - 12} textAnchor="middle" fontSize="9" fontWeight="600" fill="#b91c1c">
                    {t.total_issues}
                  </text>
                </g>
              );
            })}
          </svg>
        </div>
      </div>
    );
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
          <h2 style={{ margin: 0, fontSize: '1.5rem', fontWeight: 700 }}>{title} 专项治理</h2>
          <p style={{ margin: '0.25rem 0 0 0', fontSize: '0.875rem', color: '#64748b', textAlign: 'left' }}>
            {description}
          </p>
        </div>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: '1rem' }}>
        <div style={{ background: 'var(--card-bg)', border: '1px solid var(--border-color)', borderRadius: '8px', padding: '1.25rem', display: 'flex', alignItems: 'center', gap: '1rem' }}>
          <div style={{ width: '40px', height: '40px', borderRadius: '8px', background: 'rgba(59, 130, 246, 0.1)', color: '#3b82f6', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
            <PlayIcon />
          </div>
          <div>
            <div style={{ fontSize: '0.8rem', color: '#64748b', fontWeight: 500 }}>总追踪缺陷数</div>
            <div style={{ fontSize: '1.35rem', fontWeight: 700 }}>{totalIssuesCount}</div>
            <div style={{ fontSize: '0.75rem', color: '#94a3b8' }}>已扫描 {totalScannedRepos} 个代码仓</div>
          </div>
        </div>

        <div style={{ background: 'var(--card-bg)', border: '1px solid var(--border-color)', borderRadius: '8px', padding: '1.25rem', display: 'flex', alignItems: 'center', gap: '1rem' }}>
          <div style={{ width: '40px', height: '40px', borderRadius: '8px', background: 'rgba(16, 185, 129, 0.1)', color: '#10b981', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
            <CheckCircleIcon />
          </div>
          <div>
            <div style={{ fontSize: '0.8rem', color: '#64748b', fontWeight: 500 }}>缺陷整改率</div>
            <div style={{ fontSize: '1.35rem', fontWeight: 700, color: '#10b981' }}>{overallFixRate.toFixed(1)}%</div>
            <div style={{ fontSize: '0.75rem', color: '#94a3b8' }}>已解决与已验证占比</div>
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
            <div style={{ fontSize: '0.75rem', color: '#94a3b8' }}>高危隐患分布</div>
          </div>
        </div>

        <div style={{ background: 'var(--card-bg)', border: '1px solid var(--border-color)', borderRadius: '8px', padding: '1.25rem', display: 'flex', alignItems: 'center', gap: '1rem' }}>
          <div style={{ width: '40px', height: '40px', borderRadius: '8px', background: 'rgba(249, 115, 22, 0.1)', color: '#f97316', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
            <RefreshIcon />
          </div>
          <div>
            <div style={{ fontSize: '0.8rem', color: '#64748b', fontWeight: 500 }}>待流转缺陷</div>
            <div style={{ fontSize: '1.35rem', fontWeight: 700, color: '#f97316' }}>{totalOpenCount}</div>
            <div style={{ fontSize: '0.75rem', color: '#94a3b8' }}>待研发确认与修复</div>
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
                        未找到包含扫描数据的代码仓。请先在“扫描任务”启动相关任务。
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
                                  title={hasNotScanned ? "代码仓未进行首次分析，无法审计" : "问题审计"}
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
                      <th onClick={() => handleSort('department')} style={styles.tableHeader}>部门 {sortField === 'department' && (sortOrder === 'asc' ? '↑' : '↓')}</th>
                      <th onClick={() => handleSort('scanned_repos')} style={styles.tableHeader}>覆盖代码仓 {sortField === 'scanned_repos' && (sortOrder === 'asc' ? '↑' : '↓')}</th>
                      <th onClick={() => handleSort('total_issues')} style={styles.tableHeader}>总审计缺陷数 {sortField === 'total_issues' && (sortOrder === 'asc' ? '↑' : '↓')}</th>
                      <th onClick={() => handleSort('open_issues')} style={styles.tableHeader}>未整改缺陷 {sortField === 'open_issues' && (sortOrder === 'asc' ? '↑' : '↓')}</th>
                      <th onClick={() => handleSort('fix_rate')} style={styles.tableHeader}>缺陷整改率 {sortField === 'fix_rate' && (sortOrder === 'asc' ? '↑' : '↓')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {depts.map((d, index) => (
                      <tr key={d.department} style={{ borderBottom: '1px solid var(--border-color)' }}>
                        <td style={{ ...styles.tableCell, fontWeight: 600 }}>
                          <span style={{ display: 'inline-block', width: '20px', color: '#94a3b8', fontSize: '0.8rem', fontWeight: 700 }}>{index + 1}</span>
                          {d.department}
                        </td>
                        <td style={styles.tableCell}>{d.scanned_repos}</td>
                        <td style={styles.tableCell}>{d.total_issues}</td>
                        <td style={{ ...styles.tableCell, color: d.open_issues > 0 ? '#ef4444' : 'inherit', fontWeight: d.open_issues > 0 ? 600 : 'normal' }}>{d.open_issues}</td>
                        <td style={styles.tableCell}>
                          <span style={{ fontWeight: 700, color: d.fix_rate >= 85 ? '#10b981' : d.fix_rate >= 50 ? '#f59e0b' : '#ef4444' }}>
                            {d.fix_rate.toFixed(1)}%
                          </span>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {/* TAB 3: TREND ANALYSIS */}
          {activeTab === 'trends' && (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
              {/* Trend Configuration Panel */}
              <div style={{ background: 'var(--card-bg)', padding: '1rem', borderRadius: '8px', border: '1px solid var(--border-color)', display: 'flex', gap: '1.5rem', alignItems: 'center', flexWrap: 'wrap' }}>
                <span style={{ fontSize: '0.875rem', fontWeight: 600 }}>分析维度：</span>
                
                <label style={{ display: 'inline-flex', alignItems: 'center', gap: '0.3rem', fontSize: '0.85rem', cursor: 'pointer' }}>
                  <input type="radio" checked={trendScope === 'global'} onChange={() => setTrendScope('global')} style={{ cursor: 'pointer' }} />
                  全平台汇总
                </label>

                <label style={{ display: 'inline-flex', alignItems: 'center', gap: '0.3rem', fontSize: '0.85rem', cursor: 'pointer' }}>
                  <input type="radio" checked={trendScope === 'repo'} onChange={() => setTrendScope('repo')} style={{ cursor: 'pointer' }} />
                  特定代码仓
                </label>

                {trendScope === 'repo' && (
                  <select 
                    value={trendRepoId} 
                    onChange={e => setTrendRepoId(e.target.value)}
                    style={{ padding: '0.25rem 0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--bg-color)', fontSize: '0.85rem', color: 'var(--text-color)', outline: 'none' }}
                  >
                    <option value="">选择关联代码仓...</option>
                    {repos.filter(r => r.total_issues > 0).map(r => (
                      <option key={r.repo_id} value={r.repo_id}>{r.repo_name}</option>
                    ))}
                  </select>
                )}

                <label style={{ display: 'inline-flex', alignItems: 'center', gap: '0.3rem', fontSize: '0.85rem', cursor: 'pointer' }}>
                  <input type="radio" checked={trendScope === 'dept'} onChange={() => setTrendScope('dept')} style={{ cursor: 'pointer' }} />
                  特定部门
                </label>

                {trendScope === 'dept' && (
                  <select 
                    value={trendDeptName} 
                    onChange={e => setTrendDeptName(e.target.value)}
                    style={{ padding: '0.25rem 0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--bg-color)', fontSize: '0.85rem', color: 'var(--text-color)', outline: 'none' }}
                  >
                    <option value="">选择部门...</option>
                    {departments.map(d => (
                      <option key={d} value={d}>{d}</option>
                    ))}
                  </select>
                )}
              </div>

              {renderTrendChart()}
            </div>
          )}
        </>
      )}

      {/* 4. FLOATING AUDITING WORKSPACE MODAL */}
      <AuditingWorkspace 
        isOpen={workspaceOpen}
        onClose={() => {
          setWorkspaceOpen(false);
          // Re-fetch data on close to update the KPI metrics
          if (activeTab === 'repos') fetchReposData();
          else if (activeTab === 'depts') fetchDeptsData();
          else if (activeTab === 'trends') fetchTrendsData();
        }}
        repoId={selectedRepoId || 0}
        repoName={selectedRepoName}
        apiPrefix={`/api/analysis/${campaign}`}
        workspaceType={campaign}
        onWorkflowSaved={() => {
          // Re-fetch active view metrics dynamically
          if (activeTab === 'repos') fetchReposData();
          else if (activeTab === 'depts') fetchDeptsData();
          else if (activeTab === 'trends') fetchTrendsData();
        }}
      />
    </div>
  );
}
