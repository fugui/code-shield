import React, { useState, useEffect } from 'react';
import { Pagination, Drawer } from '@code/common';
import { apiUrl } from '../config';
import { useToast } from './Toast';
import MemberSearchSelect from './MemberSearchSelect';
import { sshToHttps } from '../utils/urlUtils';

export interface WorkspaceRepoDetails {
  id: number;
  name: string;
  url: string;
  branch: string;
}

export interface Finding {
  id: number;
  task_type_id?: number;
  repo_id?: number;
  task_report_id?: number;
  title: string;
  file_path: string;
  line_number: string | number;
  category?: string;
  severity: string;
  status: string;
  detail: string;
  code_snippet?: string;
  suggestion?: string;
  assignee_id?: number | null;
  assignee?: {
    id: number;
    name: string;
    email?: string;
  };
  status_log?: string | any[];
  feedback?: string;
  repo?: WorkspaceRepoDetails;
  created_at?: string;
  updated_at?: string;
}

const getRepoSourceUrl = (
  repoUrl: string | undefined,
  branch: string | undefined,
  filePath: string,
  lineNumber?: string | number
): string => {
  if (!repoUrl) return '';

  const webUrl = sshToHttps(repoUrl);
  const targetBranch = branch ? branch.trim() : 'master';

  const encodedFilePath = encodeURIComponent(filePath);
  const encodedBranch = encodeURIComponent(targetBranch);

  let fileUrl = `${webUrl}/files?ref=${encodedBranch}&filePath=${encodedFilePath}&isFile=true`;

  if (lineNumber) {
    const cleanLine = lineNumber.toString().replace(/\s+/g, '');
    const firstLineMatch = cleanLine.match(/^([0-9]+)/);
    if (firstLineMatch) {
      fileUrl += `#L${firstLineMatch[1]}`;
    }
  }

  return fileUrl;
};

interface AuditingWorkspaceProps {
  isOpen: boolean;
  onClose: () => void;
  repoId: number;
  repoName: string;
  apiPrefix: string; // e.g., "/api/analysis/float", "/api/analysis/coredump", "/api/analysis/ut"
  workspaceType: string;
  governanceMode?: 'defect_tracking' | 'entity_assessment';
  onWorkflowSaved?: () => void;
}

export default function AuditingWorkspace({
  isOpen,
  onClose,
  repoId,
  repoName,
  apiPrefix,
  workspaceType,
  governanceMode,
  onWorkflowSaved
}: AuditingWorkspaceProps) {
  const { showToast } = useToast();

  const getLocationText = (filePath: string, lineNumber?: string | number) => {
    if (!filePath) return '';
    const parts = filePath.split(/[/\\]/);
    const fileName = parts[parts.length - 1] || filePath;
    return lineNumber ? `${fileName}:${lineNumber}` : fileName;
  };

  const handleCopyLocation = (filePath: string, lineNumber?: string | number) => {
    if (!filePath) return;
    const copyText = getLocationText(filePath, lineNumber);

    navigator.clipboard.writeText(copyText).then(() => {
      showToast(`已复制: ${copyText}`, 'success');
    }).catch(() => {
      const textArea = document.createElement('textarea');
      textArea.value = copyText;
      document.body.appendChild(textArea);
      textArea.select();
      try {
        document.execCommand('copy');
        showToast(`已复制: ${copyText}`, 'success');
      } catch {
        showToast('复制失败', 'error');
      }
      document.body.removeChild(textArea);
    });
  };

  // Search & Filter States
  const [wsSeverity, setWsSeverity] = useState('');
  const [wsStatus, setWsStatus] = useState('');
  const [wsCategory, setWsCategory] = useState('');
  const [wsKeyword, setWsKeyword] = useState('');
  
  // Data States
  const [workspaceFindings, setWorkspaceFindings] = useState<Finding[]>([]);
  const [workspacePage, setWorkspacePage] = useState(1);
  const [workspaceTotal, setWorkspaceTotal] = useState(0);
  const [workspaceTotalPages, setWorkspaceTotalPages] = useState(1);
  const [severityStats, setSeverityStats] = useState<Record<string, number>>({});
  const [statusStats, setStatusStats] = useState<Record<string, number>>({});
  
  // Active Selected Finding & Workflow States
  const [editingFinding, setEditingFinding] = useState<Finding | null>(null);
  const [workflowStatus, setWorkflowStatus] = useState('open');
  const [workflowAssignee, setWorkflowAssignee] = useState<number | ''>('');
  const [workflowComment, setWorkflowComment] = useState('');
  const [currentUser, setCurrentUser] = useState<any>(null);

  // Resolved Task Report ID for synthesis JSON download
  const [reportId, setReportId] = useState<number | null>(null);

  useEffect(() => {
    if (isOpen) {
      fetch('/api/me')
        .then(res => res.ok ? res.json() : null)
        .then(data => {
          if (data && data.id) {
            setCurrentUser(data);
          }
        })
        .catch(() => {});
    }
  }, [isOpen]);

  useEffect(() => {
    if (currentUser?.id && editingFinding && !workflowAssignee && !editingFinding.assignee_id) {
      setWorkflowAssignee(currentUser.id);
    }
  }, [currentUser, editingFinding, workflowAssignee]);

  useEffect(() => {
    if (isOpen && repoId) {
      const params = new URLSearchParams({
        repo_id: repoId.toString(),
        pageSize: '50'
      });

      fetch(apiUrl(`/api/tasks?${params.toString()}`))
        .then(res => res.json())
        .then(data => {
          const items = data.items || data.reports || [];
          // Find the latest successful task of this campaign type
          let targetTask = items.find((item: any) => 
            (item.task_type?.campaign_path === workspaceType || item.task_type?.name === workspaceType) && 
            (item.status === 'success' || item.status === 'skipped')
          );
          // Fallback to any task of this type
          if (!targetTask) {
            targetTask = items.find((item: any) => 
              item.task_type?.campaign_path === workspaceType || item.task_type?.name === workspaceType
            );
          }
          if (targetTask) {
            setReportId(targetTask.id || targetTask.ID);
          } else {
            setReportId(null);
          }
        })
        .catch(err => {
          console.error('Failed to fetch tasks for report ID resolution:', err);
        });
    } else {
      setReportId(null);
    }
  }, [isOpen, repoId, workspaceType]);

  const dynamicCategories = React.useMemo(() => {
    return Array.from(new Set(workspaceFindings.map(f => f.category).filter(Boolean))) as string[];
  }, [workspaceFindings]);

  const handleDownloadJson = async () => {
    let activeReportId = reportId;
    if (!activeReportId && workspaceFindings.length > 0) {
      const first = workspaceFindings.find(f => f.task_report_id);
      if (first && first.task_report_id) {
        activeReportId = first.task_report_id;
      }
    }

    if (!activeReportId) {
      showToast('未找到该工作区对应的任务报告，无法下载', 'info');
      return;
    }

    try {
      const res = await fetch(apiUrl(`/api/tasks/${activeReportId}/synthesis`));
      if (!res.ok) {
        showToast('无法获取问题记录 JSON 文件，请确认文件是否存在', 'error');
        return;
      }
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `report-${activeReportId}-synthesis.json`;
      a.click();
      URL.revokeObjectURL(url);
      showToast('下载 JSON 文件成功', 'success');
    } catch (err) {
      console.error('Failed to download synthesis JSON:', err);
      showToast('下载 JSON 文件失败', 'error');
    }
  };

  const handleDownloadExcel = async () => {
    // 构造查询过滤参数
    const params = new URLSearchParams({
      repo_id: repoId.toString(),
      severity: wsSeverity,
      status: wsStatus,
      category: wsCategory,
      keyword: wsKeyword
    });

    const downloadUrl = apiUrl(`${apiPrefix}/findings/export?${params.toString()}`);
    const filename = `synthesis_${workspaceType}_${repoName}_${new Date().toISOString().split('T')[0]}.xlsx`;

    try {
      const res = await fetch(downloadUrl);
      if (!res.ok) {
        showToast('无法获取问题记录，请确认服务是否正常', 'error');
        return;
      }
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = filename;
      a.click();
      URL.revokeObjectURL(url);
      showToast('下载 Excel 成功', 'success');
    } catch (err) {
      console.error('Failed to download synthesis Excel:', err);
      showToast('下载 Excel 失败', 'error');
    }
  };

  // Fetch findings
  const fetchWorkspaceFindings = (
    rId: number,
    page: number,
    severity: string,
    status: string,
    category: string,
    keyword: string
  ) => {
    const params = new URLSearchParams({
      repo_id: rId.toString(),
      page: page.toString(),
      pageSize: '10',
      severity,
      status,
      category,
      keyword
    });

    fetch(apiUrl(`${apiPrefix}/findings?${params.toString()}`))
      .then(res => res.json())
      .then(data => {
        if (data) {
          const list = data.findings || data.items || [];
          setWorkspaceFindings(list);
          setWorkspaceTotalPages(data.totalPages || 1);
          setWorkspaceTotal(data.total !== undefined ? data.total : (data.totalPages ? data.totalPages * 10 : list.length));
          if (data.severityStats) setSeverityStats(data.severityStats);
          if (data.statusStats) setStatusStats(data.statusStats);
        }
      })
      .catch(err => {
        console.error('Failed to fetch workspace findings:', err);
        showToast('获取缺陷列表失败', 'error');
      });
  };

  // Trigger search when repoId or filters change
  useEffect(() => {
    if (isOpen && repoId) {
      setWorkspacePage(1);
      setEditingFinding(null);
      fetchWorkspaceFindings(repoId, 1, wsSeverity, wsStatus, wsCategory, wsKeyword);
    }
  }, [isOpen, repoId, wsSeverity, wsStatus, wsCategory, wsKeyword]);

  // Page navigation handler
  const handleWorkspacePageChange = (newPage: number) => {
    if (newPage < 1 || newPage > workspaceTotalPages) return;
    setWorkspacePage(newPage);
    fetchWorkspaceFindings(repoId, newPage, wsSeverity, wsStatus, wsCategory, wsKeyword);
  };

  // Open finding details workflow
  const startWorkflow = (finding: Finding) => {
    setEditingFinding(finding);
    setWorkflowStatus(finding.status || 'open');
    setWorkflowAssignee(finding.assignee_id || currentUser?.id || '');
    setWorkflowComment('');
  };

  // Submit workflow change
  const submitWorkflow = (e: React.FormEvent) => {
    e.preventDefault();
    if (!editingFinding) return;

    const findingId = editingFinding.id;
    if (!findingId) return;

    fetch(apiUrl(`${apiPrefix}/findings/${findingId}`), {
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
          
          // Re-fetch findings list to get updated status and comments
          fetchWorkspaceFindings(repoId, workspacePage, wsSeverity, wsStatus, wsCategory, wsKeyword);
          
          // Clear active panel selection
          setEditingFinding(null);
          
          // Notify parent (e.g. to update dashboard metrics)
          if (onWorkflowSaved) {
            onWorkflowSaved();
          }
        } else {
          showToast('更新审计信息失败', 'error');
        }
      })
      .catch(err => {
        console.error('Error submitting workflow:', err);
        showToast('网络请求错误，更新失败', 'error');
      });
  };

  if (!isOpen) return null;

  // Custom UI labels and logic depending on workspace type
  const severitiesList = workspaceType === 'ut'
    ? ['合格', '建议', '一般', '严重', '致命']
    : ['建议', '一般', '严重', '致命'];

  const getStatusText = (status: string) => {
    switch (status) {
      case 'open': return '待处理';
      case 'analyzing': return '问题分析';
      case 'resolved': return '已解决';
      case 'closed': return '已关闭';
      case 'invalid': return workspaceType === 'ut' ? '无效问题' : '忽略/误报';
      default: return status;
    }
  };

  // Styles helpers
  const getBadgeStyles = (severity: string) => {
    let bg = 'rgba(100, 116, 139, 0.1)';
    let color = '#64748b';
    switch (severity) {
      case '合格':
        bg = 'rgba(16, 185, 129, 0.12)';
        color = '#10b981';
        break;
      case '致命':
      case '阻塞':
        bg = 'rgba(239, 68, 68, 0.12)';
        color = '#ef4444';
        break;
      case '严重':
        bg = 'rgba(249, 115, 22, 0.12)';
        color = '#f97316';
        break;
      case '一般':
      case '主要':
        bg = 'rgba(234, 179, 8, 0.12)';
        color = '#eab308';
        break;
      case '提示':
      case '建议':
        bg = 'rgba(100, 116, 139, 0.12)';
        color = '#64748b';
        break;
    }
    return {
      fontSize: '0.7rem', 
      padding: '0.15rem 0.4rem', 
      borderRadius: '3px', 
      fontWeight: 600,
      background: bg,
      color: color
    };
  };

  const getStatusBadgeStyles = (status: string) => {
    let bg = '#f3f4f6';
    let color = '#6b7280';
    if (status === 'resolved' || status === 'closed') {
      bg = '#d1fae5';
      color = '#10b981';
    } else if (status === 'analyzing') {
      bg = '#fef3c7';
      color = '#d97706';
    } else if (status === 'open') {
      bg = '#fee2e2';
      color = '#ef4444';
    } else if (status === 'invalid') {
      bg = '#e0f2fe';
      color = '#0284c7';
    }
    return {
      fontSize: '0.7rem',
      padding: '0.15rem 0.4rem',
      borderRadius: '3px',
      fontWeight: 600,
      background: bg,
      color: color
    };
  };

  const activeAssignee = editingFinding?.assignee;
  const isEntityMode = governanceMode === 'entity_assessment' || workspaceType === 'ut';

  const workspaceTitle = (
    <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
      <span style={{ fontSize: '0.75rem', textTransform: 'uppercase', color: 'var(--primary-color)', fontWeight: 700, letterSpacing: '0.05em' }}>
        {isEntityMode ? '用例有效性评估工作区' : '专项缺陷审计工作区'}
      </span>
      <span style={{ fontSize: '1.15rem', fontWeight: 700, color: 'var(--text-color)' }}>📁 {repoName}</span>
    </div>
  );

  const workspaceExtra = (
    <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
      <button
        className="btn btn-outline"
        onClick={handleDownloadJson}
        style={{ 
          padding: '0.35rem 0.8rem', 
          fontSize: '0.85rem', 
          borderColor: '#0284c7', 
          color: '#0284c7', 
          display: 'flex', 
          alignItems: 'center', 
          gap: '0.35rem' 
        }}
        title="下载全部问题记录 (JSON)"
      >
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
          <rect x="3" y="3" width="18" height="18" rx="2" ry="2"/><line x1="9" y1="9" x2="15" y2="9"/><line x1="9" y1="17" x2="15" y2="17"/>
        </svg>
        下载 JSON
      </button>
      <button
        className="btn btn-outline"
        onClick={handleDownloadExcel}
        style={{ 
          padding: '0.35rem 0.8rem', 
          fontSize: '0.85rem', 
          borderColor: '#16a34a', 
          color: '#16a34a', 
          display: 'flex', 
          alignItems: 'center', 
          gap: '0.35rem' 
        }}
        title="下载全部问题记录 (Excel)"
      >
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
          <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path>
          <polyline points="14 2 14 8 20 8"></polyline>
          <line x1="16" y1="13" x2="8" y2="13"></line>
          <line x1="16" y1="17" x2="8" y2="17"></line>
          <polyline points="10 9 9 9 8 9"></polyline>
        </svg>
        下载 Excel
      </button>
    </div>
  );

  return (
    <Drawer
      open={isOpen}
      onClose={onClose}
      width="85vw"
      title={workspaceTitle}
      extra={workspaceExtra}
      bodyStyle={{ padding: 0, overflow: 'hidden', height: '100%', display: 'flex', flexDirection: 'column' }}
      destroyOnClose={false}
    >
      <style>{`
        .workspace-location-link {
          display: inline-flex;
          align-items: center;
          gap: 0.35rem;
          font-size: 0.8rem;
          color: #475569;
          font-family: monospace;
          background: var(--bg-color);
          padding: 0.5rem 0.75rem;
          border-radius: 4px;
          border: 1px solid var(--border-color);
          text-decoration: none;
          transition: all 0.2s ease-in-out;
          cursor: pointer;
        }
        .workspace-location-link:hover {
          background: rgba(37, 99, 235, 0.05);
          border-color: rgba(37, 99, 235, 0.25);
          color: #2563eb;
          box-shadow: 0 2px 6px rgba(37, 99, 235, 0.06);
        }
        .workspace-copy-btn {
          display: inline-flex;
          align-items: center;
          justify-content: center;
          width: 28px;
          height: 28px;
          padding: 0;
          font-size: 0.8rem;
          color: #64748b;
          background: var(--bg-color);
          border-radius: 4px;
          border: 1px solid var(--border-color);
          transition: all 0.2s ease-in-out;
          cursor: pointer;
        }
        .workspace-copy-btn:hover {
          background: rgba(37, 99, 235, 0.08);
          border-color: rgba(37, 99, 235, 0.3);
          color: #2563eb;
          box-shadow: 0 2px 6px rgba(37, 99, 235, 0.08);
        }
      `}</style>

      {/* Workspace Content */}
      <div style={{ flex: 1, display: 'flex', overflow: 'hidden' }}>
        {/* Left side list */}
        <div style={{ width: '40%', borderRight: '1px solid var(--border-color)', display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
          <div style={{ padding: '1rem', borderBottom: '1px solid var(--border-color)', display: 'flex', flexDirection: 'column', gap: '0.75rem', background: 'rgba(0,0,0,0.01)' }}>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: '0.5rem' }}>
              <select 
                value={wsSeverity}
                onChange={e => setWsSeverity(e.target.value)}
                style={{ padding: '0.35rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--card-bg)', fontSize: '0.8rem', outline: 'none', color: 'var(--text-color)' }}
              >
                <option value="">所有影响等级</option>
                {severitiesList.map(s => (
                  <option key={s} value={s}>{s} ({severityStats[s] || 0})</option>
                ))}
              </select>
              
              <select 
                value={wsStatus}
                onChange={e => setWsStatus(e.target.value)}
                style={{ padding: '0.35rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--card-bg)', fontSize: '0.8rem', outline: 'none', color: 'var(--text-color)' }}
              >
                <option value="">所有审计状态</option>
                <option value="open">待处理 ({statusStats['open'] || 0})</option>
                <option value="analyzing">问题分析 ({statusStats['analyzing'] || 0})</option>
                <option value="resolved">已解决 ({statusStats['resolved'] || 0})</option>
                <option value="closed">已关闭 ({statusStats['closed'] || 0})</option>
                <option value="invalid">{workspaceType === 'ut' ? '无效问题' : '忽略/误报'} ({statusStats['invalid'] || 0})</option>
              </select>

              <select 
                value={wsCategory}
                onChange={e => setWsCategory(e.target.value)}
                style={{ padding: '0.35rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--card-bg)', fontSize: '0.8rem', outline: 'none', color: 'var(--text-color)', cursor: 'pointer' }}
              >
                <option value="">所有分类</option>
                {dynamicCategories.map(cat => (
                  <option key={cat} value={cat}>{cat}</option>
                ))}
              </select>
            </div>
            
            <input 
              type="text" 
              placeholder={workspaceType === 'ut' ? "搜索用例名称 / 文件路径 / 问题详情..." : "过滤文件路径/描述/标题..."}
              value={wsKeyword}
              onChange={e => setWsKeyword(e.target.value)}
              style={{ padding: '0.35rem 0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--card-bg)', fontSize: '0.8rem', outline: 'none', color: 'var(--text-color)' }}
            />
          </div>

          {/* Findings scroll area */}
          <div style={{ flex: 1, overflowY: 'auto', padding: '0.5rem' }}>
            {workspaceFindings.length === 0 ? (
              <div style={{ padding: '4rem 1rem', textAlign: 'center', color: '#94a3b8', fontSize: '0.85rem' }}>未搜索到缺陷结果</div>
            ) : (
              workspaceFindings.map(f => {
                const itemTitle = f.title || '未命名缺陷';
                const activeId = editingFinding?.id === f.id;
                const itemId = f.id;
                const assigneeName = f.assignee?.name;

                return (
                  <div 
                    key={itemId}
                    onClick={() => startWorkflow(f)}
                    style={{
                      padding: '1rem',
                      borderRadius: '6px',
                      border: activeId ? '1px solid var(--primary-color)' : '1px solid var(--border-color)',
                      background: activeId ? 'rgba(37, 99, 235, 0.03)' : 'var(--card-bg)',
                      cursor: 'pointer',
                      marginBottom: '0.5rem',
                      transition: 'all 0.2s',
                      textAlign: 'left'
                    }}
                    onMouseEnter={e => {
                      if (!activeId) e.currentTarget.style.borderColor = '#cbd5e1';
                    }}
                    onMouseLeave={e => {
                      if (!activeId) e.currentTarget.style.borderColor = 'var(--border-color)';
                    }}
                  >
                    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: '0.5rem' }}>
                      <span style={getBadgeStyles(f.severity)}>
                        {f.severity}
                      </span>
                      <span style={getStatusBadgeStyles(f.status)}>
                        {getStatusText(f.status)}
                      </span>
                    </div>
                    <h4 style={{ margin: '0.5rem 0 0.25rem 0', fontSize: '0.85rem', fontWeight: 600, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', color: 'var(--text-color)' }}>{itemTitle}</h4>
                    <p style={{ margin: 0, fontSize: '0.75rem', color: '#64748b', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{f.file_path}:{f.line_number}</p>
                    
                    {/* Assignee label */}
                    {assigneeName && (
                      <div style={{ fontSize: '0.75rem', color: '#64748b', marginTop: '0.5rem', display: 'flex', alignItems: 'center', gap: '0.25rem' }}>
                        👤 处理人: <strong>{assigneeName}</strong>
                      </div>
                    )}
                  </div>
                );
              })
            )}
          </div>

          {/* Workspace Paginated Footer */}
          {workspaceTotal > 0 && (
            <div style={{ padding: '0 0.5rem 0.5rem 0.5rem', borderTop: '1px solid var(--border-color)' }}>
              <Pagination
                totalItems={workspaceTotal}
                page={workspacePage}
                pageSize={10}
                pageSizeOptions={[10, 20, 50]}
                onPageChange={(p) => {
                  setWorkspacePage(p);
                  fetchWorkspaceFindings(repoId, p, wsSeverity, wsStatus, wsCategory, wsKeyword);
                }}
              />
            </div>
          )}
        </div>

        {/* Right side audit details */}
        <div style={{ width: '60%', display: 'flex', flexDirection: 'column', overflowY: 'auto', padding: '1.5rem', gap: '1.5rem' }}>
          {editingFinding ? (
            <>
              {/* Defect Context Header */}
              <div>
                <h3 style={{ margin: '0 0 0.5rem 0', fontSize: '1.1rem', fontWeight: 700, color: '#ef4444', textAlign: 'left' }}>
                  ❌ {editingFinding.title || '未命名缺陷'}
                </h3>
                <div style={{ display: 'flex', gap: '0.75rem', flexWrap: 'wrap' }}>
                  {editingFinding.repo?.url ? (
                    <a
                      href={getRepoSourceUrl(
                        editingFinding.repo.url,
                        editingFinding.repo.branch,
                        editingFinding.file_path,
                        editingFinding.line_number
                      )}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="workspace-location-link"
                      title="点击跳转到代码仓查看源码"
                    >
                      📁 <strong>文件:</strong> {editingFinding.file_path}:{editingFinding.line_number}
                      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" style={{ marginLeft: '4px', opacity: 0.8 }}>
                        <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"></path>
                        <polyline points="15 3 21 3 21 9"></polyline>
                        <line x1="10" y1="14" x2="21" y2="3"></line>
                      </svg>
                    </a>
                  ) : (
                    <div style={{ fontSize: '0.8rem', color: '#64748b', background: 'var(--bg-color)', padding: '0.5rem 0.75rem', borderRadius: '4px', border: '1px solid var(--border-color)', display: 'inline-block' }}>
                      📁 <strong>文件:</strong> {editingFinding.file_path}:{editingFinding.line_number}
                    </div>
                  )}

                  <button
                    type="button"
                    className="workspace-copy-btn"
                    title={`复制 ${getLocationText(editingFinding.file_path, editingFinding.line_number)} （可用于 VSCode Ctrl+P 快捷定位）`}
                    onClick={() => handleCopyLocation(editingFinding.file_path, editingFinding.line_number)}
                  >
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                      <rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect>
                      <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path>
                    </svg>
                  </button>
                  {editingFinding.category && (
                    <div style={{ fontSize: '0.8rem', color: '#64748b', background: 'var(--bg-color)', padding: '0.5rem 0.75rem', borderRadius: '4px' }}>
                      🔖 <strong>归属类别:</strong> {editingFinding.category}
                    </div>
                  )}
                </div>
              </div>

              {/* Detail Description */}
              <div>
                <h4 style={{ margin: '0 0 0.5rem 0', fontSize: '0.9rem', fontWeight: 600, textAlign: 'left', color: 'var(--text-color)' }}>
                  {workspaceType === 'ut' ? '评估详情描述' : '缺陷详情'}
                </h4>
                <p style={{ margin: 0, fontSize: '0.85rem', color: 'var(--text-color)', textAlign: 'left', lineHeight: 1.5, background: 'rgba(239, 68, 68, 0.03)', border: '1px solid rgba(239, 68, 68, 0.1)', padding: '1rem', borderRadius: '6px' }}>
                  {editingFinding.detail}
                </p>
              </div>

              {/* Code Snippet */}
              {editingFinding.code_snippet && (
                <div>
                  <h4 style={{ margin: '0 0 0.5rem 0', fontSize: '0.9rem', fontWeight: 600, textAlign: 'left', color: 'var(--text-color)' }}>缺陷代码片段</h4>
                  <pre style={{ margin: 0, padding: '1rem', background: '#0f172a', color: '#e2e8f0', borderRadius: '6px', fontSize: '0.8rem', fontFamily: 'Fira Code, Consolas, Monaco, monospace', overflowX: 'auto', border: '1px solid #1e293b', lineHeight: 1.4, textAlign: 'left' }}>
                    <code>{editingFinding.code_snippet}</code>
                  </pre>
                </div>
              )}

              {/* Suggestions */}
              {editingFinding.suggestion && editingFinding.suggestion !== '无' && (
                <div>
                  <h4 style={{ margin: '0 0 0.5rem 0', fontSize: '0.9rem', fontWeight: 600, textAlign: 'left', color: 'var(--text-color)' }}>
                    {workspaceType === 'ut' ? '整改优化建议' : '修复改进建议'}
                  </h4>
                  <div style={{ margin: 0, padding: '1rem', background: 'rgba(16, 185, 129, 0.03)', border: '1px solid rgba(16, 185, 129, 0.1)', borderRadius: '6px', fontSize: '0.85rem', color: 'var(--text-color)', lineHeight: 1.5, whiteSpace: 'pre-wrap', textAlign: 'left' }}>
                    {editingFinding.suggestion}
                  </div>
                </div>
              )}

              {/* Audit Form */}
              <div style={{ borderTop: '1px solid var(--border-color)', paddingTop: '1.5rem' }}>
                <h4 style={{ margin: '0 0 1rem 0', fontSize: '0.9rem', fontWeight: 600, textAlign: 'left', color: 'var(--text-color)' }}>缺陷流转与认领审计</h4>
                <form onSubmit={submitWorkflow} style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
                  <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
                    <div>
                      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.25rem' }}>
                        <label style={{ fontSize: '0.8rem', color: '#64748b', textAlign: 'left' }}>处理人/领受人</label>
                        {currentUser && (
                          <button
                            type="button"
                            onClick={() => setWorkflowAssignee(currentUser.id)}
                            style={{
                              background: 'none',
                              border: 'none',
                              color: workflowAssignee === currentUser.id ? '#10b981' : '#3b82f6',
                              fontSize: '0.75rem',
                              cursor: 'pointer',
                              fontWeight: 500,
                              display: 'flex',
                              alignItems: 'center',
                              gap: '2px',
                              padding: 0
                            }}
                          >
                            {workflowAssignee === currentUser.id ? '✓ 已指派给自己' : '👤 指派给我'}
                          </button>
                        )}
                      </div>
                      <MemberSearchSelect 
                        value={workflowAssignee}
                        onChange={(memberId) => setWorkflowAssignee(memberId)}
                        style={{ width: '100%' }}
                      />
                    </div>

                    <div>
                      <label style={{ display: 'block', fontSize: '0.8rem', color: '#64748b', marginBottom: '0.25rem', textAlign: 'left' }}>治理审计状态</label>
                      <select 
                        value={workflowStatus} 
                        onChange={e => setWorkflowStatus(e.target.value)}
                        style={{ width: '100%', padding: '0.625rem 0.75rem', borderRadius: '6px', border: '1px solid var(--border-color)', background: 'var(--bg-color)', color: 'var(--text-color)', fontSize: '0.85rem', outline: 'none' }}
                      >
                        <option value="open">待处理 (Open)</option>
                        <option value="analyzing">问题分析 (Analyzing)</option>
                        <option value="resolved">已解决 (Resolved)</option>
                        <option value="closed">已关闭 (Closed)</option>
                        <option value="invalid">{workspaceType === 'ut' ? '无效问题 (Invalid)' : '忽略/误报 (Invalid)'}</option>
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
                      style={{ width: '100%', padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--card-bg)', color: 'var(--text-color)', fontSize: '0.85rem', outline: 'none', resize: 'vertical', fontFamily: 'inherit', boxSizing: 'border-box' }}
                    />
                  </div>

                  <button type="submit" className="btn" style={{ alignSelf: 'flex-end', padding: '0.5rem 1.5rem', fontSize: '0.85rem' }}>
                    保存审计记录
                  </button>
                </form>
              </div>

              {/* Status Change log timeline */}
              <div style={{ borderTop: '1px solid var(--border-color)', paddingTop: '1.5rem' }}>
                <h4 style={{ margin: '0 0 1rem 0', fontSize: '0.9rem', fontWeight: 600, textAlign: 'left', color: 'var(--text-color)' }}>状态演进流转历史</h4>
                <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem', textAlign: 'left' }}>
                  {editingFinding.status_log ? (() => {
                    try {
                      let logs: any[];
                      if (Array.isArray(editingFinding.status_log)) {
                        logs = editingFinding.status_log;
                      } else if (typeof editingFinding.status_log === 'string') {
                        logs = JSON.parse(editingFinding.status_log);
                      } else {
                        logs = [];
                      }
                      if (!Array.isArray(logs) || logs.length === 0) {
                        return <div style={{ fontSize: '0.8rem', color: '#94a3b8' }}>暂无流转记录</div>;
                      }
                      return logs.map((log: any, idx: number) => (
                        <div key={idx} style={{ position: 'relative', paddingLeft: '1.5rem', borderLeft: '2px solid #e2e8f0', paddingBottom: '0.5rem' }}>
                          <div style={{ position: 'absolute', left: '-6px', top: '2px', width: '10px', height: '10px', borderRadius: '50%', background: '#3b82f6' }} />
                          <div style={{ fontSize: '0.8rem', fontWeight: 600, color: 'var(--text-color)' }}>{getStatusText(log.status)}</div>
                          <div style={{ fontSize: '0.75rem', color: '#64748b', marginTop: '0.1rem' }}>
                            操作人: <strong>{log.user}</strong> &bull; 时间: {log.time}
                          </div>
                          {log.comment && (
                            <div style={{ fontSize: '0.75rem', color: 'var(--text-color)', background: 'rgba(0,0,0,0.02)', padding: '0.4rem 0.6rem', borderRadius: '4px', marginTop: '0.25rem' }}>
                              {log.comment}
                            </div>
                          )}
                          {log.reason && (
                            <div style={{ fontSize: '0.75rem', color: '#64748b', marginTop: '0.1rem' }}>
                              原因: {log.reason}
                            </div>
                          )}
                        </div>
                      ));
                    } catch (err) {
                      return <div style={{ fontSize: '0.8rem', color: '#94a3b8' }}>日志格式异常，请联系管理员</div>;
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
              <span>请从左侧列表选择缺陷开始安全审计</span>
            </div>
          )}
        </div>
      </div>
    </Drawer>
  );
}
