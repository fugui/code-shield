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

export default function CoredumpAnalysis() {
  const { showToast } = useToast();
  
  // Dashboard overall stats & states
  const [loading, setLoading] = useState(true);
  const [repos, setRepos] = useState<any[]>([]);
  const [depts, setDepts] = useState<any[]>([]);
  const [trends, setTrends] = useState<any[]>([]);
  
  const [activeTab, setActiveTab] = useState<'repos' | 'depts' | 'trends'>('repos');
  const [departments, setDepartments] = useState<string[]>([]);
  const [selectedDept, setSelectedDept] = useState('');
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
  const [workspaceFindings, setWorkspaceFindings] = useState<any[]>([]);
  const [workspacePage, setWorkspacePage] = useState(1);
  const [workspaceTotalPages, setWorkspaceTotalPages] = useState(1);
  
  // Filters inside workspace
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
  const [coredumpTaskTypeId, setCoredumpTaskTypeId] = useState<number | null>(null);

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
    fetch(apiUrl('/api/analysis/coredump/departments'))
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
        const list = Array.isArray(data) ? data : (data.items || []);
        setMembers(list);
      })
      .catch(console.error);
  };

  const fetchTaskTypeId = () => {
    fetch(apiUrl('/api/task-types'))
      .then(res => res.json())
      .then(data => {
        const list = Array.isArray(data) ? data : (data.items || []);
        const cdTask = list.find((t: any) => t.name === 'coredump_risk');
        if (cdTask) {
          setCoredumpTaskTypeId(cdTask.id || cdTask.ID);
        }
      })
      .catch(console.error);
  };

  const fetchReposData = () => {
    setLoading(true);
    const params = new URLSearchParams({
      sort_by: sortField,
      sort_order: sortOrder,
    });
    if (keyword) params.append('keyword', keyword);
    if (selectedDept) params.append('department', selectedDept);

    fetch(apiUrl(`/api/analysis/coredump/repos?${params.toString()}`))
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
    fetch(apiUrl(`/api/analysis/coredump/departments?${params.toString()}`))
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

    fetch(apiUrl(`/api/analysis/coredump/trends?${params.toString()}`))
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
    setWorkspacePage(1);
    setWorkspaceOpen(true);
    setWsSeverity('');
    setWsStatus('');
    setWsCategory('');
    setWsKeyword('');
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

    fetch(apiUrl(`/api/analysis/coredump/findings?${params.toString()}`))
      .then(res => res.json())
      .then(data => {
        setWorkspaceFindings(data.items || []);
        setWorkspaceTotalPages(data.totalPages || 1);
      })
      .catch(err => {
        console.error(err);
        showToast('加载缺陷跟踪列表失败', 'error');
      });
  };

  const handleWorkspaceFilterChange = (field: string, value: string) => {
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

    fetch(apiUrl(`/api/analysis/coredump/findings/${editingFinding.ID || editingFinding.id}`), {
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
          showToast('更新处理状态失败', 'error');
        }
      })
      .catch(err => {
        console.error(err);
        showToast('网络错误，处理失败', 'error');
      });
  };

  // Trigger scan task manually
  const triggerScan = (repoId: number) => {
    if (!coredumpTaskTypeId) {
      showToast('正在初始化扫描任务，请稍等或刷新重试。', 'info');
      fetchTaskTypeId();
      return;
    }

    fetch(apiUrl('/api/tasks/trigger'), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ repo_id: repoId, task_type_id: coredumpTaskTypeId }),
    })
      .then(res => {
        if (res.ok) {
          showToast('成功触发 Coredump 风险扫描，后台深度分析中...', 'success');
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
          <h4 style={{ margin: 0, fontSize: '1rem', fontWeight: 600 }}>Coredump 存量风险缺陷趋势 (过去30天)</h4>
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

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '1.5rem', color: 'var(--text-color)', fontFamily: 'Inter, system-ui, sans-serif' }}>
      
      {/* 1. TOP HEADER & KPI CARDS */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <div>
          <h2 style={{ margin: 0, fontSize: '1.5rem', fontWeight: 700 }}>Coredump 专项风险治理</h2>
          <p style={{ margin: '0.25rem 0 0 0', fontSize: '0.875rem', color: '#64748b' }}>
            跟踪治理 C/C++ 核心代码中可能导致进程异常退出（Coredump）的高危隐患，落实每一个缺陷的认领与修复工作。
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
            <div style={{ fontSize: '0.75rem', color: '#94a3b8' }}>高危段错误/内存溢出隐患</div>
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
                        未找到包含扫描数据的代码仓。请先前往“扫描任务”启动“C/C++ Coredump 风险分析”任务。
                      </td>
                    </tr>
                  ) : (
                    repos.map(r => (
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

                <label style={{ display: 'inline-flex', alignItems: 'center', gap: '0.3rem', fontSize: '0.85rem', cursor: 'pointer' }}>
                  <input type="radio" checked={trendScope === 'dept'} onChange={() => setTrendScope('dept')} style={{ cursor: 'pointer' }} />
                  特定部门
                </label>

                {trendScope === 'repo' && (
                  <select 
                    value={trendRepoId} 
                    onChange={e => setTrendRepoId(e.target.value)}
                    style={{ padding: '0.3rem 0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--card-bg)', fontSize: '0.85rem', outline: 'none' }}
                  >
                    <option value="">-- 选择代码仓 --</option>
                    {repos.map(r => <option key={r.repo_id} value={r.repo_id}>{r.repo_name}</option>)}
                  </select>
                )}

                {trendScope === 'dept' && (
                  <select 
                    value={trendDeptName} 
                    onChange={e => setTrendDeptName(e.target.value)}
                    style={{ padding: '0.3rem 0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--card-bg)', fontSize: '0.85rem', outline: 'none' }}
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

      {/* 4. CAMPAIGN AUDIT WORKSPACE (FULL-WIDTH MODAL DRAWER) */}
      {workspaceOpen && (
        <div style={{ position: 'fixed', top: 0, left: 0, width: '100%', height: '100%', background: 'rgba(15, 23, 42, 0.45)', backdropFilter: 'blur(4px)', display: 'flex', justifyContent: 'flex-end', zIndex: 500 }}>
          <div style={{ width: '80%', minWidth: '800px', height: '100%', background: 'var(--card-bg)', display: 'flex', flexDirection: 'column', boxShadow: '-10px 0 30px rgba(0,0,0,0.15)', borderLeft: '1px solid var(--border-color)' }}>
            
            {/* Header */}
            <div style={{ padding: '1.25rem 2rem', borderBottom: '1px solid var(--border-color)', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <div>
                <h3 style={{ margin: 0, fontSize: '1.2rem', fontWeight: 700 }}>代码仓缺陷审计工作台</h3>
                <span style={{ fontSize: '0.8rem', color: '#64748b' }}>审计对象仓: <strong style={{ color: 'var(--primary-color)' }}>{selectedRepoName}</strong></span>
              </div>
              <button 
                onClick={() => setWorkspaceOpen(false)}
                style={{ background: 'transparent', border: 'none', fontSize: '1.5rem', color: '#64748b', cursor: 'pointer', display: 'flex', alignItems: 'center', justifyContent: 'center', width: '32px', height: '32px', borderRadius: '50%' }}
                onMouseEnter={e => e.currentTarget.style.background = 'var(--bg-color)'}
                onMouseLeave={e => e.currentTarget.style.background = 'transparent'}
              >
                &times;
              </button>
            </div>

            {/* Filter Panel */}
            <div style={{ padding: '1rem 2rem', background: 'var(--bg-color)', borderBottom: '1px solid var(--border-color)', display: 'flex', gap: '0.75rem', flexWrap: 'wrap', alignItems: 'center' }}>
              <select 
                value={wsSeverity} 
                onChange={e => handleWorkspaceFilterChange('severity', e.target.value)}
                style={{ padding: '0.35rem 0.6rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--card-bg)', fontSize: '0.8rem' }}
              >
                <option value="">全部严重度</option>
                <option value="阻塞">阻塞</option>
                <option value="严重">严重</option>
                <option value="主要">主要</option>
                <option value="提示">提示</option>
                <option value="建议">建议</option>
              </select>

              <select 
                value={wsStatus} 
                onChange={e => handleWorkspaceFilterChange('status', e.target.value)}
                style={{ padding: '0.35rem 0.6rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--card-bg)', fontSize: '0.8rem' }}
              >
                <option value="">全部状态</option>
                <option value="open">待处理</option>
                <option value="analyzing">问题分析</option>
                <option value="resolved">已解决</option>
                <option value="closed">已关闭</option>
                <option value="invalid">忽略/误报</option>
              </select>

              <input 
                type="text" 
                placeholder="搜索标题/详情/文件名..."
                value={wsKeyword}
                onChange={e => handleWorkspaceFilterChange('keyword', e.target.value)}
                style={{ padding: '0.35rem 0.6rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--card-bg)', fontSize: '0.8rem', width: '200px', outline: 'none' }}
              />
            </div>

            {/* Workspace Body */}
            <div style={{ flex: 1, display: 'flex', overflow: 'hidden' }}>
              {workspaceFindings.length === 0 ? (
                <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', flexDirection: 'column', color: '#94a3b8', padding: '3rem' }}>
                  <ShieldAlertIcon />
                  <span style={{ marginTop: '1rem', fontSize: '0.9rem' }}>暂无符合当前审计条件的缺陷记录。</span>
                </div>
              ) : (
                <div style={{ flex: 1, display: 'flex', overflow: 'hidden' }}>
                  {/* Left List */}
                  <div style={{ width: '40%', borderRight: '1px solid var(--border-color)', overflowY: 'auto', display: 'flex', flexDirection: 'column' }}>
                    {workspaceFindings.map(f => (
                      <div 
                        key={f.ID || f.id} 
                        onClick={() => startWorkflow(f)}
                        style={{
                          padding: '1.25rem',
                          borderBottom: '1px solid var(--border-color)',
                          cursor: 'pointer',
                          background: editingFinding && (editingFinding.ID === f.ID || editingFinding.id === f.id) ? 'rgba(37, 99, 235, 0.05)' : 'transparent',
                          transition: 'all 0.15s'
                        }}
                      >
                        <div style={{ display: 'flex', justifyContent: 'space-between', gap: '0.5rem', marginBottom: '0.5rem' }}>
                          <span style={styles.badge(f.severity)}>{f.severity}</span>
                          <span style={styles.statusBadge(f.status)}>{f.status}</span>
                        </div>
                        <h4 style={{ margin: '0 0 0.5rem 0', fontSize: '0.875rem', fontWeight: 600, color: 'var(--text-color)', lineHeight: 1.3 }}>{f.title}</h4>
                        <div style={{ fontSize: '0.75rem', color: '#64748b', wordBreak: 'break-all' }}>{f.file_path}:{f.line_number}</div>
                      </div>
                    ))}

                    {/* Pagination */}
                    {workspaceTotalPages > 1 && (
                      <div style={{ padding: '1rem', borderTop: '1px solid var(--border-color)', display: 'flex', justifyContent: 'space-between', alignItems: 'center', background: 'var(--bg-color)' }}>
                        <button 
                          disabled={workspacePage <= 1}
                          onClick={() => handleWorkspacePageChange(workspacePage - 1)}
                          style={{ padding: '0.25rem 0.5rem', fontSize: '0.75rem', cursor: workspacePage <= 1 ? 'not-allowed' : 'pointer' }}
                        >
                          上一页
                        </button>
                        <span style={{ fontSize: '0.75rem', color: '#64748b' }}>页码: {workspacePage} / {workspaceTotalPages}</span>
                        <button 
                          disabled={workspacePage >= workspaceTotalPages}
                          onClick={() => handleWorkspacePageChange(workspacePage + 1)}
                          style={{ padding: '0.25rem 0.5rem', fontSize: '0.75rem', cursor: workspacePage >= workspaceTotalPages ? 'not-allowed' : 'pointer' }}
                        >
                          下一页
                        </button>
                      </div>
                    )}
                  </div>

                  {/* Right Detail Pane */}
                  <div style={{ width: '60%', overflowY: 'auto', padding: '2rem', display: 'flex', flexDirection: 'column', gap: '1.5rem' }}>
                    {editingFinding ? (
                      <>
                        {/* Summary Info */}
                        <div>
                          <div style={{ display: 'flex', gap: '1rem', marginBottom: '0.5rem', alignItems: 'center' }}>
                            <span style={styles.badge(editingFinding.severity)}>{editingFinding.severity}</span>
                            <span style={{ fontSize: '0.8rem', color: '#64748b' }}>{editingFinding.category}</span>
                          </div>
                          <h3 style={{ margin: '0 0 0.75rem 0', fontSize: '1.15rem', fontWeight: 700, color: 'var(--text-color)' }}>{editingFinding.title}</h3>
                          <div style={{ fontSize: '0.8rem', color: '#64748b', background: 'var(--bg-color)', padding: '0.5rem 0.75rem', borderRadius: '4px', wordBreak: 'break-all' }}>
                            📁 <strong>文件:</strong> {editingFinding.file_path}:{editingFinding.line_number}
                          </div>
                        </div>

                        {/* Detail Description */}
                        <div>
                          <h4 style={{ margin: '0 0 0.5rem 0', fontSize: '0.9rem', fontWeight: 600 }}>缺陷详情</h4>
                          <p style={{ margin: 0, fontSize: '0.85rem', color: 'var(--text-color)', lineHeight: 1.5, background: 'rgba(239, 68, 68, 0.03)', border: '1px solid rgba(239, 68, 68, 0.1)', padding: '1rem', borderRadius: '6px' }}>
                            {editingFinding.detail}
                          </p>
                        </div>

                        {/* Code Snippet */}
                        {editingFinding.code_snippet && (
                          <div>
                            <h4 style={{ margin: '0 0 0.5rem 0', fontSize: '0.9rem', fontWeight: 600 }}>缺陷代码片段</h4>
                            <pre style={{ margin: 0, padding: '1rem', background: '#0f172a', color: '#e2e8f0', borderRadius: '6px', fontSize: '0.8rem', fontFamily: 'Fira Code, Consolas, Monaco, monospace', overflowX: 'auto', border: '1px solid #1e293b', lineHeight: 1.4 }}>
                              <code>{editingFinding.code_snippet}</code>
                            </pre>
                          </div>
                        )}

                        {/* Suggestions */}
                        <div>
                          <h4 style={{ margin: '0 0 0.5rem 0', fontSize: '0.9rem', fontWeight: 600 }}>修复改进建议</h4>
                          <div style={{ margin: 0, padding: '1rem', background: 'rgba(16, 185, 129, 0.03)', border: '1px solid rgba(16, 185, 129, 0.1)', borderRadius: '6px', fontSize: '0.85rem', color: 'var(--text-color)', lineHeight: 1.5, whiteSpace: 'pre-wrap' }}>
                            {editingFinding.suggestion}
                          </div>
                        </div>

                        {/* Audit Form */}
                        <div style={{ borderTop: '1px solid var(--border-color)', paddingTop: '1.5rem' }}>
                          <h4 style={{ margin: '0 0 1rem 0', fontSize: '0.9rem', fontWeight: 600 }}>缺陷流转与认领审计</h4>
                          <form onSubmit={submitWorkflow} style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
                            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
                              <div>
                                <label style={{ display: 'block', fontSize: '0.8rem', color: '#64748b', marginBottom: '0.25rem' }}>处理人/领受人</label>
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
                                <label style={{ display: 'block', fontSize: '0.8rem', color: '#64748b', marginBottom: '0.25rem' }}>治理审计状态</label>
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
                              <label style={{ display: 'block', fontSize: '0.8rem', color: '#64748b', marginBottom: '0.25rem' }}>审计说明与跟踪意见</label>
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

                        {/* Workflow Trace Logs */}
                        {editingFinding.status_log && (() => {
                          let logs = [];
                          try {
                            logs = typeof editingFinding.status_log === 'string' 
                              ? JSON.parse(editingFinding.status_log) 
                              : editingFinding.status_log;
                          } catch (e) {}
                          
                          if (!Array.isArray(logs) || logs.length === 0) return null;

                          return (
                            <div style={{ borderTop: '1px solid var(--border-color)', paddingTop: '1.5rem', marginBottom: '2rem' }}>
                              <h4 style={{ margin: '0 0 1rem 0', fontSize: '0.9rem', fontWeight: 600 }}>缺陷流转治理轨迹 (Timeline)</h4>
                              <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem', paddingLeft: '0.5rem' }}>
                                {logs.map((log: any, index: number) => (
                                  <div key={index} style={{ display: 'flex', gap: '1rem', fontSize: '0.8rem', position: 'relative' }}>
                                    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center' }}>
                                      <div style={{ width: '8px', height: '8px', borderRadius: '50%', background: 'var(--primary-color)' }} />
                                      {index < logs.length - 1 && (
                                        <div style={{ width: '1.5px', flex: 1, background: 'var(--border-color)', marginTop: '0.25rem' }} />
                                      )}
                                    </div>
                                    <div style={{ flex: 1, paddingBottom: '0.5rem' }}>
                                      <div style={{ display: 'flex', justifyContent: 'space-between', gap: '1rem', color: '#64748b', marginBottom: '0.25rem' }}>
                                        <span>操作人: <strong>{log.user || 'system'}</strong></span>
                                        <span>时间: {log.time}</span>
                                      </div>
                                      <div>
                                        将状态变更为: <span style={styles.statusBadge(log.status)}>{log.status}</span>
                                      </div>
                                      {log.comment && (
                                        <div style={{ background: 'var(--bg-color)', padding: '0.4rem 0.6rem', borderRadius: '4px', marginTop: '0.25rem', color: '#475569' }}>
                                          💬 {log.comment}
                                        </div>
                                      )}
                                      {log.reason && (
                                        <div style={{ color: '#94a3b8', fontSize: '0.75rem', marginTop: '0.25rem' }}>
                                          ℹ️ 系统日志: {log.reason}
                                        </div>
                                      )}
                                    </div>
                                  </div>
                                ))}
                              </div>
                            </div>
                          );
                        })()}
                      </>
                    ) : (
                      <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', flexDirection: 'column', color: '#94a3b8', height: '100%' }}>
                        <svg width="48" height="48" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="1" strokeLinecap="round" strokeLinejoin="round" style={{ opacity: 0.5 }}>
                          <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path>
                          <polyline points="14 2 14 8 20 8"></polyline>
                          <line x1="16" y1="13" x2="8" y2="13"></line>
                          <line x1="16" y1="17" x2="8" y2="17"></line>
                          <polyline points="10 9 9 9 8 9"></polyline>
                        </svg>
                        <span style={{ marginTop: '1rem', fontSize: '0.9rem' }}>点击左侧缺陷列表项查看审计详情</span>
                      </div>
                    )}
                  </div>
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
