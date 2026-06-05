import React, { useEffect, useState } from 'react';
import { useToast } from '../components/Toast';
import { apiUrl } from '../config';
import AuditingWorkspace from '../components/AuditingWorkspace';
import { sshToHttps } from '../utils/urlUtils';

// Icons implemented as inline SVGs for maximum control and lightweight portability
const PlayIcon = () => <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor" stroke="none"><polygon points="5 3 19 12 5 21 5 3"/></svg>;
const RefreshIcon = () => <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><polyline points="23 4 23 10 17 10"/><polyline points="1 20 1 14 7 14"/><path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"/></svg>;
const SearchIcon = () => <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>;
const FilterIcon = () => <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><polygon points="22 3 2 3 10 12.46 10 19 14 21 14 12.46 22 3"/></svg>;
const ChevronRightIcon = () => <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><polyline points="9 18 15 12 9 6"/></svg>;
const ArrowUpDownIcon = () => <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><line x1="12" y1="5" x2="12" y2="19"/><polyline points="19 12 12 19 5 12"/><polyline points="5 12 12 5 19 12"/></svg>;
const UserIcon = () => <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg>;
const ShieldAlertIcon = () => <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>;
const CheckCircleIcon = () => <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/><polyline points="22 4 12 14.01 9 11.01"/></svg>;
const XIcon = () => <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>;
const CalendarIcon = () => <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><rect x="3" y="4" width="18" height="18" rx="2" ry="2"/><line x1="16" y1="2" x2="16" y2="6"/><line x1="8" y1="2" x2="8" y2="6"/><line x1="3" y1="10" x2="21" y2="10"/></svg>;

interface StatusLogEntry {
  status: string;
  time: string;
  user: string;
  comment?: string;
}

const getStatusLabel = (status: string) => {
  switch (status) {
    case 'open': return '待处理';
    case 'analyzing': return '问题分析';
    case 'resolved': return '已解决';
    case 'closed': return '已关闭';
    case 'invalid': return '无效问题';
    default: return status || '待处理';
  }
};

function UTAnalysis() {
  const { showToast } = useToast();
  
  // Dashboard view tabs: 'repos' (代码仓概览), 'depts' (部门统计), 'trends' (趋势分析)
  const [activeTab, setActiveTab] = useState<'repos' | 'depts' | 'trends'>('repos');
  const [loading, setLoading] = useState(false);
  
  // Filters & Search
  const [keyword, setKeyword] = useState('');
  const [selectedDept, setSelectedDept] = useState('');
  const [repoPage, setRepoPage] = useState(1);
  const [repoPageSize, setRepoPageSize] = useState(25);
  const [departments, setDepartments] = useState<string[]>([]);
  
  // Back-end data
  const [repos, setRepos] = useState<any[]>([]);
  const [depts, setDepts] = useState<any[]>([]);
  const [trends, setTrends] = useState<any[]>([]);
  const [trendScope, setTrendScope] = useState<'global' | 'repo' | 'dept'>('global');
  const [trendRepoId, setTrendRepoId] = useState<string>('');
  const [trendDeptName, setTrendDeptName] = useState<string>('');
  
  // Sort State
  const [sortField, setSortField] = useState('pass_rate');
  const [sortOrder, setSortOrder] = useState<'asc' | 'desc'>('desc');

  // Workspace / Auditing Drawer states
  const [selectedRepoId, setSelectedRepoId] = useState<number | null>(null);
  const [selectedRepoName, setSelectedRepoName] = useState('');
  const [workspaceOpen, setWorkspaceOpen] = useState(false);
  const [utTaskTypeId, setUtTaskTypeId] = useState<number | null>(null);

  // Nested Department Repos
  const [expandedDepts, setExpandedDepts] = useState<Record<string, boolean>>({});
  const [deptRepos, setDeptRepos] = useState<Record<string, any[]>>({});
  const [deptReposLoading, setDeptReposLoading] = useState<Record<string, boolean>>({});

  const toggleDeptExpand = (deptName: string) => {
    const isExpanding = !expandedDepts[deptName];
    setExpandedDepts(prev => ({ ...prev, [deptName]: isExpanding }));
    
    if (isExpanding && !deptRepos[deptName]) {
      setDeptReposLoading(prev => ({ ...prev, [deptName]: true }));
      fetch(apiUrl(`/api/analysis/ut/repos?department=${encodeURIComponent(deptName)}`))
        .then(res => res.json())
        .then(data => {
          setDeptRepos(prev => ({ ...prev, [deptName]: Array.isArray(data) ? data : [] }));
        })
        .catch(err => {
          console.error(err);
          showToast(`获取部门 ${deptName} 的代码仓失败`, 'error');
        })
        .finally(() => {
          setDeptReposLoading(prev => ({ ...prev, [deptName]: false }));
        });
    }
  };
  
  // Fetch lists
  useEffect(() => {
    fetchDepartments();
    fetchTaskTypeId();
  }, []);

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
    // Collect distinct departments from repos to fill dropdown
    fetch(apiUrl('/api/analysis/ut/departments'))
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
        const utTask = list.find((t: any) => t.name === 'ut_effectiveness');
        if (utTask) {
          setUtTaskTypeId(utTask.id || utTask.ID);
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

    fetch(apiUrl(`/api/analysis/ut/repos?${params.toString()}`))
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
    fetch(apiUrl(`/api/analysis/ut/departments?${params.toString()}`))
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

    fetch(apiUrl(`/api/analysis/ut/trends?${params.toString()}`))
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



  // Trigger scan task manually
  const triggerScan = (repoId: number) => {
    if (!utTaskTypeId) {
      showToast('正在初始化扫描任务，请稍等或刷新重试。', 'info');
      // Fetch fallback just in case
      fetchTaskTypeId();
      return;
    }

    fetch(apiUrl('/api/tasks/trigger'), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ repo_id: repoId, task_type_id: utTaskTypeId }),
    })
      .then(res => {
        if (res.ok) {
          showToast('成功触发测试用例有效性扫描，后台执行中...', 'success');
        } else {
          showToast('启动扫描任务失败，请检查是否已有在排队执行中的相同任务', 'error');
        }
      })
      .catch(err => {
        console.error(err);
        showToast('网络连接失败', 'error');
      });
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
      else if (status === 'invalid') { bg = '#e0f2fe'; label = '无效问题'; color = '#075985'; }
      
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
          暂无足够的历史扫描数据来绘制变化趋势。请在完成更多扫描任务后再来查看。
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

    // Resolve min/max values
    const passRates = trends.map(t => t.pass_rate);
    const maxCases = Math.max(...trends.map(t => t.total_cases), 10);

    // Compute SVG path string for Pass Rate Line
    let passLinePath = '';
    let passAreaPath = '';
    
    trends.forEach((t, i) => {
      const x = paddingLeft + (i * chartWidth) / (trends.length - 1 || 1);
      const y = paddingTop + chartHeight - (t.pass_rate * chartHeight) / 100;
      
      if (i === 0) {
        passLinePath = `M ${x} ${y}`;
        passAreaPath = `M ${x} ${paddingTop + chartHeight} L ${x} ${y}`;
      } else {
        passLinePath += ` L ${x} ${y}`;
        passAreaPath += ` L ${x} ${y}`;
      }
      
      if (i === trends.length - 1) {
        passAreaPath += ` L ${x} ${paddingTop + chartHeight} Z`;
      }
    });

    return (
      <div style={{ background: 'var(--card-bg)', padding: '1.5rem', borderRadius: '8px', border: '1px solid var(--border-color)', position: 'relative' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: '1.5rem', alignItems: 'center' }}>
          <h4 style={{ margin: 0, fontSize: '1rem', fontWeight: 600 }}>单元测试合格率与用例总数趋势（过去30天）</h4>
          <div style={{ display: 'flex', gap: '1rem', fontSize: '0.8rem' }}>
            <span style={{ display: 'inline-flex', alignItems: 'center', gap: '0.4rem' }}>
              <span style={{ width: '12px', height: '12px', borderRadius: '2px', background: 'rgba(16, 185, 129, 0.2)', border: '2px solid #10b981', display: 'inline-block' }} />
              合格率 (%)
            </span>
            <span style={{ display: 'inline-flex', alignItems: 'center', gap: '0.4rem' }}>
              <span style={{ width: '12px', height: '12px', borderRadius: '2px', background: '#3b82f6', display: 'inline-block' }} />
              测试用例总数
            </span>
          </div>
        </div>

        <div style={{ overflowX: 'auto' }}>
          <svg viewBox={`0 0 ${width} ${height}`} width="100%" height="320" style={{ overflow: 'visible' }}>
            <defs>
              <linearGradient id="passGradient" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor="#10b981" stopOpacity="0.25"/>
                <stop offset="100%" stopColor="#10b981" stopOpacity="0.0"/>
              </linearGradient>
            </defs>

            {/* Horizontal Grid lines */}
            {[0, 25, 50, 75, 100].map((val) => {
              const y = paddingTop + chartHeight - (val * chartHeight) / 100;
              return (
                <g key={val}>
                  <line x1={paddingLeft} y1={y} x2={width - paddingRight} y2={y} stroke="var(--border-color)" strokeWidth="1" strokeDasharray="4 4" />
                  <text x={paddingLeft - 8} y={y + 4} textAnchor="end" fontSize="10" fill="#94a3b8">{val}%</text>
                </g>
              );
            })}

            {/* Bars for Total Cases */}
            {trends.map((t, i) => {
              const barWidth = Math.max(chartWidth / (trends.length * 2.5), 10);
              const x = paddingLeft + (i * chartWidth) / (trends.length - 1 || 1) - barWidth / 2;
              const barHeight = (t.total_cases * chartHeight) / maxCases;
              const y = paddingTop + chartHeight - barHeight;

              return (
                <rect
                  key={i}
                  x={x}
                  y={y}
                  width={barWidth}
                  height={barHeight}
                  fill="rgba(59, 130, 246, 0.7)"
                  rx="2"
                  style={{ transition: 'all 0.3s' }}
                />
              );
            })}

            {/* Area under Pass Rate Line */}
            {trends.length > 1 && (
              <path d={passAreaPath} fill="url(#passGradient)" />
            )}

            {/* Pass Rate Line */}
            {trends.length > 1 && (
              <path d={passLinePath} fill="none" stroke="#10b981" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" />
            )}

            {/* Circular points & hover values */}
            {trends.map((t, i) => {
              const x = paddingLeft + (i * chartWidth) / (trends.length - 1 || 1);
              const y = paddingTop + chartHeight - (t.pass_rate * chartHeight) / 100;

              return (
                <g key={i}>
                  <circle cx={x} cy={y} r="5" fill="#10b981" stroke="white" strokeWidth="2" />
                  {/* Date labels on bottom */}
                  {i % Math.max(Math.round(trends.length / 6), 1) === 0 && (
                    <text x={x} y={height - 12} textAnchor="middle" fontSize="10" fill="#64748b" transform={`rotate(-15, ${x}, ${height - 12})`}>
                      {t.date.substring(5)}
                    </text>
                  )}
                  {/* Tooltip-like value hover */}
                  <text x={x} y={y - 12} textAnchor="middle" fontSize="9" fontWeight="600" fill="#047857" style={{ opacity: 0.85 }}>
                    {t.pass_rate.toFixed(0)}%
                  </text>
                </g>
              );
            })}
          </svg>
        </div>
      </div>
    );
  };

  // Metrics overview KPIs
  const totalCasesCount = repos.reduce((acc, r) => acc + r.total_cases, 0);
  const totalScannedRepos = repos.filter(r => r.total_cases > 0).length;
  const avgPassRate = totalCasesCount > 0 
    ? (repos.reduce((acc, r) => acc + r.pass_count, 0) / totalCasesCount) * 100 
    : 0;
  const totalBlocker = repos.reduce((acc, r) => acc + r.blocking, 0);
  const totalCritical = repos.reduce((acc, r) => acc + r.critical, 0);
  const totalOpen = repos.reduce((acc, r) => acc + r.open_issues, 0);

  // Repository list pagination calculations
  const totalRepoPages = Math.ceil(repos.length / repoPageSize) || 1;
  const startIndex = (repoPage - 1) * repoPageSize;
  const paginatedRepos = repos.slice(startIndex, startIndex + repoPageSize);

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '1.5rem', color: 'var(--text-color)', fontFamily: 'Inter, system-ui, sans-serif' }}>
      
      {/* 1. TOP HEADER & KPI CARDS */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <div>
          <h2 style={{ margin: 0, fontSize: '1.5rem', fontWeight: 700 }}>测试用例有效性看板</h2>
          <p style={{ margin: '0.25rem 0 0 0', fontSize: '0.875rem', color: '#64748b' }}>
            展示自动化“测试用例有效性评估”的审计指标，衡量测试断言有效性、空测试和用例修复进展。
          </p>
        </div>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: '1rem' }}>
        <div style={{ background: 'var(--card-bg)', border: '1px solid var(--border-color)', borderRadius: '8px', padding: '1.25rem', display: 'flex', alignItems: 'center', gap: '1rem' }}>
          <div style={{ width: '40px', height: '40px', borderRadius: '8px', background: 'rgba(59, 130, 246, 0.1)', color: '#3b82f6', display: 'flex', alignItems: 'center', justifySelf: 'center', justifyContent: 'center' }}>
            <PlayIcon />
          </div>
          <div>
            <div style={{ fontSize: '0.8rem', color: '#64748b', fontWeight: 500 }}>总测试用例数</div>
            <div style={{ fontSize: '1.35rem', fontWeight: 700 }}>{totalCasesCount}</div>
            <div style={{ fontSize: '0.75rem', color: '#94a3b8' }}>已扫描 {totalScannedRepos} 个代码仓</div>
          </div>
        </div>

        <div style={{ background: 'var(--card-bg)', border: '1px solid var(--border-color)', borderRadius: '8px', padding: '1.25rem', display: 'flex', alignItems: 'center', gap: '1rem' }}>
          <div style={{ width: '40px', height: '40px', borderRadius: '8px', background: 'rgba(16, 185, 129, 0.1)', color: '#10b981', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
            <CheckCircleIcon />
          </div>
          <div>
            <div style={{ fontSize: '0.8rem', color: '#64748b', fontWeight: 500 }}>平均测试合格率</div>
            <div style={{ fontSize: '1.35rem', fontWeight: 700, color: '#10b981' }}>{avgPassRate.toFixed(1)}%</div>
            <div style={{ fontSize: '0.75rem', color: '#94a3b8' }}>有效测试用例比重</div>
          </div>
        </div>

        <div style={{ background: 'var(--card-bg)', border: '1px solid var(--border-color)', borderRadius: '8px', padding: '1.25rem', display: 'flex', alignItems: 'center', gap: '1rem' }}>
          <div style={{ width: '40px', height: '40px', borderRadius: '8px', background: 'rgba(239, 68, 68, 0.1)', color: '#ef4444', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
            <ShieldAlertIcon />
          </div>
          <div>
            <div style={{ fontSize: '0.8rem', color: '#64748b', fontWeight: 500 }}>阻塞 / 严重问题</div>
            <div style={{ fontSize: '1.35rem', fontWeight: 700, color: '#ef4444' }}>
              {totalBlocker} <span style={{ fontSize: '0.8rem', fontWeight: 500, color: '#94a3b8' }}>/</span> {totalCritical}
            </div>
            <div style={{ fontSize: '0.75rem', color: '#94a3b8' }}>空测试与永真断言数量</div>
          </div>
        </div>

        <div style={{ background: 'var(--card-bg)', border: '1px solid var(--border-color)', borderRadius: '8px', padding: '1.25rem', display: 'flex', alignItems: 'center', gap: '1rem' }}>
          <div style={{ width: '40px', height: '40px', borderRadius: '8px', background: 'rgba(249, 115, 22, 0.1)', color: '#f97316', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
            <RefreshIcon />
          </div>
          <div>
            <div style={{ fontSize: '0.8rem', color: '#64748b', fontWeight: 500 }}>待流转缺陷</div>
            <div style={{ fontSize: '1.35rem', fontWeight: 700, color: '#f97316' }}>{totalOpen}</div>
            <div style={{ fontSize: '0.75rem', color: '#94a3b8' }}>待研发整改与确认</div>
          </div>
        </div>
      </div>

      {/* 2. SUB TAB NAVIGATION & FILTERS */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '1rem', borderBottom: '1px solid var(--border-color)', paddingBottom: '0.5rem' }}>
        <div style={{ display: 'flex', gap: '1.5rem' }}>
          <button 
            onClick={() => { setActiveTab('repos'); setSortField('pass_rate'); }}
            style={{ padding: '0.75rem 0.25rem', border: 'none', background: 'transparent', borderBottom: activeTab === 'repos' ? '2px solid var(--primary-color)' : '2px solid transparent', color: activeTab === 'repos' ? 'var(--primary-color)' : '#64748b', fontWeight: activeTab === 'repos' ? 600 : 500, cursor: 'pointer', fontSize: '0.9rem' }}
          >
            代码仓概览
          </button>
          <button 
            onClick={() => { setActiveTab('depts'); setSortField('pass_rate'); }}
            style={{ padding: '0.75rem 0.25rem', border: 'none', background: 'transparent', borderBottom: activeTab === 'depts' ? '2px solid var(--primary-color)' : '2px solid transparent', color: activeTab === 'depts' ? 'var(--primary-color)' : '#64748b', fontWeight: activeTab === 'depts' ? 600 : 500, cursor: 'pointer', fontSize: '0.9rem' }}
          >
            部门排行统计
          </button>
          <button 
            onClick={() => { setActiveTab('trends'); }}
            style={{ padding: '0.75rem 0.25rem', border: 'none', background: 'transparent', borderBottom: activeTab === 'trends' ? '2px solid var(--primary-color)' : '2px solid transparent', color: activeTab === 'trends' ? 'var(--primary-color)' : '#64748b', fontWeight: activeTab === 'trends' ? 600 : 500, cursor: 'pointer', fontSize: '0.9rem' }}
          >
            指标趋势分析
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
                    <th onClick={() => handleSort('total_cases')} style={styles.tableHeader}>
                      测试用例总数 {sortField === 'total_cases' && (sortOrder === 'asc' ? '↑' : '↓')}
                    </th>
                    <th onClick={() => handleSort('blocking')} style={styles.tableHeader}>
                      阻塞 {sortField === 'blocking' && (sortOrder === 'asc' ? '↑' : '↓')}
                    </th>
                    <th onClick={() => handleSort('critical')} style={styles.tableHeader}>
                      严重 {sortField === 'critical' && (sortOrder === 'asc' ? '↑' : '↓')}
                    </th>
                    <th onClick={() => handleSort('pass_rate')} style={styles.tableHeader}>
                      有效合格率 {sortField === 'pass_rate' && (sortOrder === 'asc' ? '↑' : '↓')}
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
                        未找到包含扫描数据的代码仓。请先前往“扫描任务”触发“测试用例有效性评估”任务。
                      </td>
                    </tr>
                  ) : (
                    paginatedRepos.map(r => (
                      <tr key={r.repo_id} style={{ borderBottom: '1px solid var(--border-color)', transition: 'background 0.2s' }} onMouseEnter={e => e.currentTarget.style.background = 'rgba(0,0,0,0.01)'} onMouseLeave={e => e.currentTarget.style.background = 'transparent'}>
                        <td style={{ ...styles.tableCell, fontWeight: 600 }}>
                          {r.repo_url ? (
                            <a
                              href={sshToHttps(r.repo_url)}
                              target="_blank"
                              rel="noreferrer"
                              style={{ color: 'var(--primary-color)', textDecoration: 'none' }}
                              onMouseEnter={e => e.currentTarget.style.textDecoration = 'underline'}
                              onMouseLeave={e => e.currentTarget.style.textDecoration = 'none'}
                            >
                              {r.repo_name}
                            </a>
                          ) : (
                            r.repo_name
                          )}
                        </td>
                        <td style={styles.tableCell}>{r.department || '-'}</td>
                        <td style={styles.tableCell}>{r.owner_name}</td>
                        <td style={{ ...styles.tableCell, fontWeight: 500 }}>{r.total_cases > 0 ? r.total_cases : <span style={{ color: '#94a3b8' }}>未扫描</span>}</td>
                        <td style={{ ...styles.tableCell, color: r.blocking > 0 ? '#ef4444' : 'inherit', fontWeight: r.blocking > 0 ? 600 : 'normal' }}>{r.blocking}</td>
                        <td style={{ ...styles.tableCell, color: r.critical > 0 ? '#f97316' : 'inherit', fontWeight: r.critical > 0 ? 600 : 'normal' }}>{r.critical}</td>
                        <td style={styles.tableCell}>
                          {r.total_cases > 0 ? (
                            <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', minWidth: '110px' }}>
                              <span style={{ fontSize: '0.85rem', fontWeight: 600, width: '40px' }}>{r.pass_rate.toFixed(0)}%</span>
                              <div style={{ flex: 1, height: '6px', borderRadius: '3px', background: '#e2e8f0', overflow: 'hidden' }}>
                                <div style={{ height: '100%', borderRadius: '3px', width: `${r.pass_rate}%`, background: r.pass_rate >= 85 ? '#10b981' : r.pass_rate >= 60 ? '#f59e0b' : '#ef4444' }} />
                              </div>
                            </div>
                          ) : (
                            <span style={{ color: '#94a3b8' }}>-</span>
                          )}
                        </td>
                        <td style={styles.tableCell}>
                          {r.last_scan_time && r.last_scan_time !== '0001-01-01T00:00:00Z' ? (
                            <span title={new Date(r.last_scan_time).toLocaleString()}>
                              {r.last_scan_time.substring(0, 10)}
                            </span>
                          ) : (
                            <span style={{ color: '#94a3b8' }}>-</span>
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
                                  title={hasNotScanned ? "代码仓未进行首次分析，无法审计" : "用例审计"}
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

              {totalRepoPages > 0 && (
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '0.75rem 1.5rem', borderTop: '1px solid var(--border-color)', background: 'var(--card-bg)', fontSize: '0.875rem' }}>
                  <span style={{ fontSize: '0.85rem', color: '#64748b' }}>
                    共 {repos.length} 个代码仓
                  </span>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                    <button 
                      disabled={repoPage === 1}
                      onClick={() => setRepoPage(prev => Math.max(prev - 1, 1))}
                      style={{
                        padding: '0.3rem 0.6rem', border: '1px solid var(--border-color)', background: 'transparent',
                        borderRadius: '4px', cursor: repoPage === 1 ? 'not-allowed' : 'pointer',
                        color: repoPage === 1 ? 'var(--text-secondary)' : 'var(--text-color)', fontSize: '0.825rem',
                        transition: 'all 0.2s'
                      }}
                      onMouseEnter={e => { if (repoPage !== 1) e.currentTarget.style.background = 'rgba(0,0,0,0.02)'; }}
                      onMouseLeave={e => { e.currentTarget.style.background = 'transparent'; }}
                    >
                      上一页
                    </button>
                    
                    {/* Page numbers */}
                    {Array.from({ length: Math.min(5, totalRepoPages) }, (_, i) => {
                      let pageNum = repoPage;
                      if (repoPage <= 3) pageNum = i + 1;
                      else if (repoPage >= totalRepoPages - 2) pageNum = totalRepoPages - 4 + i;
                      else pageNum = repoPage - 2 + i;

                      if (pageNum < 1 || pageNum > totalRepoPages) return null;

                      return (
                        <button
                          key={pageNum}
                          onClick={() => setRepoPage(pageNum)}
                          style={{
                            minWidth: '28px', height: '28px', padding: '0 0.3rem',
                            border: '1px solid',
                            borderColor: repoPage === pageNum ? 'var(--primary-color)' : 'var(--border-color)',
                            background: repoPage === pageNum ? 'var(--primary-color)' : 'transparent',
                            color: repoPage === pageNum ? 'white' : 'var(--text-color)',
                            borderRadius: '4px', cursor: repoPage === pageNum ? 'not-allowed' : 'pointer',
                            fontSize: '0.825rem', fontWeight: repoPage === pageNum ? 600 : 400,
                            transition: 'all 0.2s'
                          }}
                          onMouseEnter={e => { if (repoPage !== pageNum) e.currentTarget.style.background = 'rgba(37,99,235,0.04)'; }}
                          onMouseLeave={e => { if (repoPage !== pageNum) e.currentTarget.style.background = 'transparent'; }}
                        >
                          {pageNum}
                        </button>
                      );
                    })}

                    <button 
                      disabled={repoPage === totalRepoPages}
                      onClick={() => setRepoPage(prev => Math.min(prev + 1, totalRepoPages))}
                      style={{
                        padding: '0.3rem 0.6rem', border: '1px solid var(--border-color)', background: 'transparent',
                        borderRadius: '4px', cursor: repoPage === totalRepoPages ? 'not-allowed' : 'pointer',
                        color: repoPage === totalRepoPages ? 'var(--text-secondary)' : 'var(--text-color)', fontSize: '0.825rem',
                        transition: 'all 0.2s'
                      }}
                      onMouseEnter={e => { if (repoPage !== totalRepoPages) e.currentTarget.style.background = 'rgba(0,0,0,0.02)'; }}
                      onMouseLeave={e => { e.currentTarget.style.background = 'transparent'; }}
                    >
                      下一页
                    </button>

                    <select
                      value={repoPageSize}
                      onChange={e => {
                        setRepoPageSize(Number(e.target.value));
                        setRepoPage(1);
                      }}
                      style={{
                        padding: '0.25rem 0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)',
                        fontSize: '0.825rem', outline: 'none', background: 'transparent', color: 'var(--text-color)', marginLeft: '0.5rem',
                        cursor: 'pointer'
                      }}
                    >
                      <option value="15">15 条/页</option>
                      <option value="25">25 条/页</option>
                      <option value="50">50 条/页</option>
                      <option value="100">100 条/页</option>
                    </select>
                  </div>
                </div>
              )}
            </div>
          )}

          {/* TAB 2: DEPARTMENTS RANKING */}
          {activeTab === 'depts' && (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '1.5rem' }}>
              {/* Podium for Top 3 Departments */}
              {depts.length > 0 && (
                <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'flex-end', gap: '2rem', padding: '1.5rem 0', background: 'rgba(255,255,255,0.01)', borderRadius: '12px' }}>
                  {/* Silver (2nd) */}
                  {depts[1] && (
                    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center' }}>
                      <div style={{ fontSize: '0.8rem', color: '#64748b', fontWeight: 600, marginBottom: '0.25rem' }}>🥈 亚军</div>
                      <div style={{ fontSize: '1rem', fontWeight: 700 }}>{depts[1].department}</div>
                      <div style={{ fontSize: '1.25rem', fontWeight: 800, color: '#f59e0b', margin: '0.25rem 0' }}>{depts[1].pass_rate.toFixed(1)}%</div>
                      <div style={{ width: '80px', height: '80px', background: 'linear-gradient(180deg, #e2e8f0 0%, #cbd5e1 100%)', borderTopLeftRadius: '6px', borderTopRightRadius: '6px', display: 'flex', alignItems: 'center', justifyContent: 'center', color: '#475569', fontSize: '1.5rem', fontWeight: 800 }}>2</div>
                    </div>
                  )}

                  {/* Gold (1st) */}
                  {depts[0] && (
                    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center' }}>
                      <div style={{ fontSize: '0.85rem', color: '#eab308', fontWeight: 600, marginBottom: '0.25rem' }}>👑 冠军</div>
                      <div style={{ fontSize: '1.15rem', fontWeight: 700 }}>{depts[0].department}</div>
                      <div style={{ fontSize: '1.5rem', fontWeight: 800, color: '#10b981', margin: '0.25rem 0' }}>{depts[0].pass_rate.toFixed(1)}%</div>
                      <div style={{ width: '100px', height: '110px', background: 'linear-gradient(180deg, #fef08a 0%, #eab308 100%)', borderTopLeftRadius: '6px', borderTopRightRadius: '6px', display: 'flex', alignItems: 'center', justifyContent: 'center', color: '#78350f', fontSize: '2rem', fontWeight: 800, boxShadow: '0 8px 16px rgba(234, 179, 8, 0.15)' }}>1</div>
                    </div>
                  )}

                  {/* Bronze (3rd) */}
                  {depts[2] && (
                    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center' }}>
                      <div style={{ fontSize: '0.8rem', color: '#92400e', fontWeight: 600, marginBottom: '0.25rem' }}>🥉 季军</div>
                      <div style={{ fontSize: '1rem', fontWeight: 700 }}>{depts[2].department}</div>
                      <div style={{ fontSize: '1.25rem', fontWeight: 800, color: '#ea580c', margin: '0.25rem 0' }}>{depts[2].pass_rate.toFixed(1)}%</div>
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
                      <th onClick={() => handleSort('total_cases')} style={styles.tableHeader}>总测试用例数 {sortField === 'total_cases' && (sortOrder === 'asc' ? '↑' : '↓')}</th>
                      <th style={styles.tableHeader}>问题总数</th>
                      <th onClick={() => handleSort('pass_rate')} style={styles.tableHeader}>合格用例占比 {sortField === 'pass_rate' && (sortOrder === 'asc' ? '↑' : '↓')}</th>
                      <th onClick={() => handleSort('fix_rate')} style={styles.tableHeader}>缺陷整改率 {sortField === 'fix_rate' && (sortOrder === 'asc' ? '↑' : '↓')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {depts.map((d, index) => (
                      <React.Fragment key={d.department}>
                        <tr 
                          onClick={() => toggleDeptExpand(d.department)}
                          style={{ borderBottom: '1px solid var(--border-color)', cursor: 'pointer', transition: 'background 0.2s' }}
                          onMouseEnter={e => e.currentTarget.style.background = 'rgba(0,0,0,0.01)'}
                          onMouseLeave={e => e.currentTarget.style.background = 'transparent'}
                        >
                          <td style={{ ...styles.tableCell, fontWeight: 600 }}>
                            <span style={{ display: 'inline-flex', alignItems: 'center', gap: '0.5rem' }}>
                              <span style={{ display: 'inline-block', width: '20px', color: '#94a3b8', fontSize: '0.8rem', fontWeight: 700 }}>{index + 1}</span>
                              <span style={{ display: 'inline-flex', alignItems: 'center', transition: 'transform 0.2s', transform: expandedDepts[d.department] ? 'rotate(90deg)' : 'rotate(0deg)', color: '#64748b' }}>
                                <ChevronRightIcon />
                              </span>
                              {d.department}
                            </span>
                          </td>
                          <td style={styles.tableCell}>{d.scanned_repos}/{d.total_repos || 0}</td>
                          <td style={styles.tableCell}>{d.total_cases}</td>
                          <td style={{ ...styles.tableCell, color: d.issues_count > 0 ? '#f97316' : 'inherit' }}>{d.issues_count}</td>
                          <td style={styles.tableCell}>
                            <span style={{ fontWeight: 700, color: d.pass_rate >= 85 ? '#10b981' : d.pass_rate >= 60 ? '#f59e0b' : '#ef4444' }}>
                              {d.pass_rate.toFixed(1)}%
                            </span>
                          </td>
                          <td style={styles.tableCell}>
                            <span style={{ fontWeight: 600, color: '#3b82f6' }}>{d.fix_rate.toFixed(1)}%</span>
                          </td>
                        </tr>
                        {expandedDepts[d.department] && (
                          <tr>
                            <td colSpan={6} style={{ padding: '0.75rem 1.5rem', background: 'var(--bg-color)', borderBottom: '1px solid var(--border-color)' }}>
                              {deptReposLoading[d.department] ? (
                                <div style={{ display: 'flex', justifyContent: 'center', padding: '1.5rem' }}>
                                  <div style={{ animation: 'spin 1s linear infinite', border: '2px solid rgba(59, 130, 246, 0.1)', borderTop: '2px solid #3b82f6', borderRadius: '50%', width: '20px', height: '20px', marginRight: '0.5rem' }} />
                                  <span style={{ fontSize: '0.85rem', color: '#64748b' }}>正在加载该部门的代码仓...</span>
                                </div>
                              ) : !deptRepos[d.department] || deptRepos[d.department].length === 0 ? (
                                <div style={{ padding: '1rem', textAlign: 'center', color: '#94a3b8', fontSize: '0.85rem' }}>
                                  暂无代码仓数据
                                </div>
                              ) : (
                                <div style={{ border: '1px solid var(--border-color)', borderRadius: '8px', overflow: 'hidden', background: 'var(--card-bg)', boxShadow: '0 2px 8px rgba(0,0,0,0.04)' }}>
                                  <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '0.85rem' }}>
                                    <thead>
                                      <tr style={{ background: 'var(--bg-color)', borderBottom: '1px solid var(--border-color)' }}>
                                        <th style={{ ...styles.tableHeader, padding: '0.6rem 0.8rem', cursor: 'default', borderBottom: '1px solid var(--border-color)' }}>代码仓</th>
                                        <th style={{ ...styles.tableHeader, padding: '0.6rem 0.8rem', cursor: 'default', borderBottom: '1px solid var(--border-color)' }}>负责人</th>
                                        <th style={{ ...styles.tableHeader, padding: '0.6rem 0.8rem', cursor: 'default', borderBottom: '1px solid var(--border-color)' }}>测试用例总数</th>
                                        <th style={{ ...styles.tableHeader, padding: '0.6rem 0.8rem', cursor: 'default', borderBottom: '1px solid var(--border-color)' }}>阻塞</th>
                                        <th style={{ ...styles.tableHeader, padding: '0.6rem 0.8rem', cursor: 'default', borderBottom: '1px solid var(--border-color)' }}>严重</th>
                                        <th style={{ ...styles.tableHeader, padding: '0.6rem 0.8rem', cursor: 'default', borderBottom: '1px solid var(--border-color)' }}>有效合格率</th>
                                        <th style={{ ...styles.tableHeader, padding: '0.6rem 0.8rem', cursor: 'default', borderBottom: '1px solid var(--border-color)' }}>最近扫描</th>
                                        <th style={{ ...styles.tableHeader, padding: '0.6rem 0.8rem', cursor: 'default', borderBottom: '1px solid var(--border-color)' }}>操作</th>
                                      </tr>
                                    </thead>
                                    <tbody>
                                      {deptRepos[d.department].map((r: any) => (
                                        <tr key={r.repo_id} style={{ borderBottom: '1px solid var(--border-color)', transition: 'background 0.2s' }} onMouseEnter={e => e.currentTarget.style.background = 'rgba(0,0,0,0.01)'} onMouseLeave={e => e.currentTarget.style.background = 'transparent'}>
                                          <td style={{ ...styles.tableCell, padding: '0.6rem 0.8rem', fontWeight: 600 }}>
                                            {r.repo_url ? (
                                              <a
                                                href={sshToHttps(r.repo_url)}
                                                target="_blank"
                                                rel="noreferrer"
                                                style={{ color: 'var(--primary-color)', textDecoration: 'none' }}
                                                onMouseEnter={e => e.currentTarget.style.textDecoration = 'underline'}
                                                onMouseLeave={e => e.currentTarget.style.textDecoration = 'none'}
                                              >
                                                {r.repo_name}
                                              </a>
                                            ) : (
                                              r.repo_name
                                            )}
                                          </td>
                                          <td style={{ ...styles.tableCell, padding: '0.6rem 0.8rem' }}>{r.owner_name}</td>
                                          <td style={{ ...styles.tableCell, padding: '0.6rem 0.8rem' }}>{r.total_cases > 0 ? r.total_cases : <span style={{ color: '#94a3b8' }}>未扫描</span>}</td>
                                          <td style={{ ...styles.tableCell, padding: '0.6rem 0.8rem', color: r.blocking > 0 ? '#ef4444' : 'inherit', fontWeight: r.blocking > 0 ? 600 : 'normal' }}>{r.blocking}</td>
                                          <td style={{ ...styles.tableCell, padding: '0.6rem 0.8rem', color: r.critical > 0 ? '#f97316' : 'inherit', fontWeight: r.critical > 0 ? 600 : 'normal' }}>{r.critical}</td>
                                          <td style={{ ...styles.tableCell, padding: '0.6rem 0.8rem' }}>
                                            {r.total_cases > 0 ? (
                                              <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', minWidth: '95px' }}>
                                                <span style={{ fontSize: '0.8rem', fontWeight: 600, width: '35px' }}>{r.pass_rate.toFixed(0)}%</span>
                                                <div style={{ flex: 1, height: '4px', borderRadius: '2px', background: '#e2e8f0', overflow: 'hidden' }}>
                                                  <div style={{ height: '100%', borderRadius: '2px', width: `${r.pass_rate}%`, background: r.pass_rate >= 85 ? '#10b981' : r.pass_rate >= 60 ? '#f59e0b' : '#ef4444' }} />
                                                </div>
                                              </div>
                                            ) : (
                                              <span style={{ color: '#94a3b8' }}>-</span>
                                            )}
                                          </td>
                                          <td style={{ ...styles.tableCell, padding: '0.6rem 0.8rem' }}>
                                            {r.last_scan_time && r.last_scan_time !== '0001-01-01T00:00:00Z' ? (
                                              <span title={new Date(r.last_scan_time).toLocaleString()}>
                                                {r.last_scan_time.substring(0, 10)}
                                              </span>
                                            ) : (
                                              <span style={{ color: '#94a3b8' }}>-</span>
                                            )}
                                          </td>
                                          <td style={{ ...styles.tableCell, padding: '0.6rem 0.8rem' }}>
                                            {(() => {
                                              const hasNotScanned = !r.last_scan_time || r.last_scan_time === '0001-01-01T00:00:00Z';
                                              return (
                                                <button 
                                                  disabled={hasNotScanned}
                                                  onClick={(e) => {
                                                    e.stopPropagation();
                                                    openWorkspace(r.repo_id, r.repo_name);
                                                  }}
                                                  title={hasNotScanned ? "代码仓未进行首次分析，无法审计" : "用例审计"}
                                                  style={{
                                                    background: 'transparent',
                                                    border: 'none',
                                                    color: hasNotScanned ? '#cbd5e1' : 'var(--primary-color)',
                                                    cursor: hasNotScanned ? 'not-allowed' : 'pointer',
                                                    padding: '0.25rem',
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
                                                  <svg width="16" height="16" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                                                    <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path>
                                                    <polyline points="14 2 14 8 20 8"></polyline>
                                                    <circle cx="10" cy="13" r="2"></circle>
                                                    <line x1="21" y1="21" x2="11.5" y2="14.8"></line>
                                                  </svg>
                                                </button>
                                              );
                                            })()}
                                          </td>
                                        </tr>
                                      ))}
                                    </tbody>
                                  </table>
                                </div>
                              )}
                            </td>
                          </tr>
                        )}
                      </React.Fragment>
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
              <div style={{ background: 'var(--card-bg)', padding: '1rem', borderRadius: '8px', border: '1px solid var(--border-color)', display: 'flex', gap: '1rem', alignItems: 'center', flexWrap: 'wrap' }}>
                <span style={{ fontSize: '0.875rem', fontWeight: 600 }}>分析维度：</span>
                
                <label style={{ display: 'inline-flex', alignItems: 'center', gap: '0.3rem', fontSize: '0.85rem', cursor: 'pointer' }}>
                  <input type="radio" checked={trendScope === 'global'} onChange={() => setTrendScope('global')} style={{ cursor: 'pointer' }} />
                  全平台汇总
                </label>

                <label style={{ display: 'inline-flex', alignItems: 'center', gap: '0.3rem', fontSize: '0.85rem', cursor: 'pointer' }}>
                  <input type="radio" checked={trendScope === 'repo'} onChange={() => setTrendScope('repo')} style={{ cursor: 'pointer' }} />
                  特定代码仓
                </label>

                <label style={{ display: 'inline-flex', alignItems: 'center', gap: '0.3rem', fontSize: '0.85rem', cursor: 'pointer' }}>
                  <input type="radio" checked={trendScope === 'dept'} onChange={() => setTrendScope('dept')} style={{ cursor: 'pointer' }} />
                  特定部门
                </label>

                {trendScope === 'repo' && (
                  <select 
                    value={trendRepoId} 
                    onChange={e => setTrendRepoId(e.target.value)}
                    style={{ padding: '0.3rem 0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--card-bg)', fontSize: '0.85rem' }}
                  >
                    <option value="">-- 选择代码仓 --</option>
                    {repos.map(r => <option key={r.repo_id} value={r.repo_id}>{r.repo_name}</option>)}
                  </select>
                )}

                {trendScope === 'dept' && (
                  <select 
                    value={trendDeptName} 
                    onChange={e => setTrendDeptName(e.target.value)}
                    style={{ padding: '0.3rem 0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--card-bg)', fontSize: '0.85rem' }}
                  >
                    <option value="">-- 选择部门 --</option>
                    {departments.map(dept => <option key={dept} value={dept}>{dept}</option>)}
                  </select>
                )}
              </div>

              {renderTrendChart()}
            </div>
          )}
        </>
      )}

      {/* 4. WORKSPACE MODAL / SLIDE PANELS */}
      <AuditingWorkspace 
        isOpen={workspaceOpen}
        onClose={() => setWorkspaceOpen(false)}
        repoId={selectedRepoId || 0}
        repoName={selectedRepoName}
        apiPrefix="/api/analysis/ut"
        workspaceType="ut"
        onWorkflowSaved={fetchReposData}
      />



    </div>
  );
}

export default UTAnalysis;
