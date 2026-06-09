import React, { useEffect, useState, useCallback } from 'react';
import { useToast } from '../components/Toast';
import MemberSearchSelect from '../components/MemberSearchSelect';
import ReportSidebar from '../components/ReportSidebar';
import { sshToHttps } from '../utils/urlUtils';
import { useNavigate } from 'react-router-dom';
import { appNavigatePath } from '../config';

interface WorkbenchFinding {
	id: number;
	type: string;
	type_name: string;
	repo_id: number;
	repo_name: string;
	repo_url: string;
	file_path: string;
	line_number: string;
	title: string;
	detail: string;
	severity: string;
	category: string;
	code_snippet: string;
	suggestion: string;
	status: string;
	status_log: string | null;
	created_at: string;
	updated_at: string;
}

const severityColors: Record<string, { color: string; bg: string }> = {
	'阻塞': { color: '#ef4444', bg: 'rgba(239, 68, 68, 0.12)' },
	'严重': { color: '#f97316', bg: 'rgba(249, 115, 22, 0.12)' },
	'主要': { color: '#eab308', bg: 'rgba(234, 179, 8, 0.12)' },
	'提示': { color: '#3b82f6', bg: 'rgba(59, 130, 246, 0.12)' },
	'建议': { color: '#6b7280', bg: 'rgba(107, 114, 128, 0.12)' },
	'合格': { color: '#10b981', bg: 'rgba(16, 185, 129, 0.12)' }
};

const statusMap: Record<string, { label: string; color: string; bg: string }> = {
	'open': { label: '待处理', color: '#ef4444', bg: 'rgba(239, 68, 68, 0.1)' },
	'analyzing': { label: '问题分析', color: '#f59e0b', bg: 'rgba(245, 158, 11, 0.1)' },
	'resolved': { label: '已解决', color: '#10b981', bg: 'rgba(16, 185, 129, 0.1)' },
	'closed': { label: '已关闭', color: '#6b7280', bg: 'rgba(107, 114, 128, 0.1)' },
	'invalid': { label: '忽略/误报', color: '#3b82f6', bg: 'rgba(59, 130, 246, 0.1)' }
};

// 3 merged columns for the Kanban
const kanbanColumns = [
	{
		key: 'active',
		label: '待处理 / 分析中',
		color: '#ef4444',
		statuses: ['open', 'analyzing'],
		icon: (
			<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
				<circle cx="12" cy="12" r="10" /><line x1="12" y1="8" x2="12" y2="12" /><line x1="12" y1="16" x2="12.01" y2="16" />
			</svg>
		)
	},
	{
		key: 'resolved',
		label: '已解决',
		color: '#10b981',
		statuses: ['resolved'],
		icon: (
			<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
				<polyline points="20 6 9 17 4 12" />
			</svg>
		)
	},
	{
		key: 'done',
		label: '已关闭 / 忽略',
		color: '#6b7280',
		statuses: ['closed', 'invalid'],
		icon: (
			<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
				<circle cx="12" cy="12" r="10" /><line x1="15" y1="9" x2="9" y2="15" /><line x1="9" y1="9" x2="15" y2="15" />
			</svg>
		)
	}
];

export default function Workbench() {
	const { showToast } = useToast();
	const navigate = useNavigate();
	const [findings, setFindings] = useState<WorkbenchFinding[]>([]);
	const [loading, setLoading] = useState(true);
	const [error, setError] = useState<string | null>(null);

	// Filters
	const [search, setSearch] = useState('');
	const [filterType, setFilterType] = useState('');
	const [filterSeverity, setFilterSeverity] = useState('');

	// Active Edit Drawer
	const [selectedFinding, setSelectedFinding] = useState<WorkbenchFinding | null>(null);
	const [editStatus, setEditStatus] = useState('open');
	const [editAssignee, setEditAssignee] = useState<number | ''>('');
	const [editFeedback, setEditFeedback] = useState('');
	const [saving, setSaving] = useState(false);

	// Team Repos section
	const [myInfo, setMyInfo] = useState<any>(null);
	const [teamRepos, setTeamRepos] = useState<any[]>([]);
	const [repoTasks, setRepoTasks] = useState<Record<number, any[]>>({});
	const [reposLoading, setReposLoading] = useState(false);

	// Report sidebar
	const [sidebarOpen, setSidebarOpen] = useState(false);
	const [currentMarkdown, setCurrentMarkdown] = useState('');
	const [loadingMarkdown, setLoadingMarkdown] = useState(false);
	const [currentReportId, setCurrentReportId] = useState<number | undefined>(undefined);

	const loadFindings = async () => {
		setLoading(true);
		setError(null);
		try {
			const res = await fetch('/api/me/findings');
			if (!res.ok) throw new Error('获取个人缺陷列表失败');
			const data = await res.json();
			setFindings(data || []);
		} catch (err: any) {
			setError(err.message || '加载工作台失败');
			showToast('加载缺陷数据失败', 'error');
		} finally {
			setLoading(false);
		}
	};

	const loadTeamRepos = useCallback(async (deptId: number) => {
		setReposLoading(true);
		try {
			const res = await fetch(`/api/repos?department_id=${deptId}&pageSize=50`);
			if (!res.ok) return;
			const data = await res.json();
			const repos: any[] = data.items || data || [];
			setTeamRepos(repos);
			// For each repo, fetch recent tasks (limit 5)
			const taskMap: Record<number, any[]> = {};
			await Promise.all(repos.slice(0, 20).map(async (repo: any) => {
				try {
					const r = await fetch(`/api/tasks?repo_id=${repo.id}&pageSize=5&status=success`);
					if (r.ok) {
						const d = await r.json();
						taskMap[repo.id] = d.items || [];
					}
				} catch { /* ignore */ }
			}));
			setRepoTasks(taskMap);
		} catch { /* ignore */ } finally {
			setReposLoading(false);
		}
	}, []);

	useEffect(() => {
		loadFindings();
		// Fetch user info for team repos
		fetch('/api/me')
			.then(r => r.ok ? r.json() : null)
			.then(info => {
				if (info) {
					setMyInfo(info);
					if (info.department_id) {
						loadTeamRepos(info.department_id);
					}
				}
			})
			.catch(console.error);
	}, []);

	// Save Audit
	const handleSaveAudit = async (e: React.FormEvent) => {
		e.preventDefault();
		if (!selectedFinding) return;

		setSaving(true);
		try {
			const typePath = selectedFinding.type;
			const res = await fetch(`/api/analysis/${typePath}/findings/${selectedFinding.id}`, {
				method: 'PATCH',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({
					status: editStatus,
					assignee_id: editAssignee || null,
					feedback: editFeedback || undefined
				})
			});
			if (!res.ok) throw new Error('更新审计状态失败');
			showToast('审计数据已成功更新', 'success');
			setSelectedFinding(null);
			loadFindings();
		} catch (err: any) {
			showToast(err.message || '更新失败', 'error');
		} finally {
			setSaving(false);
		}
	};

	const openAudit = (finding: WorkbenchFinding) => {
		setSelectedFinding(finding);
		setEditStatus(finding.status);
		setEditFeedback('');
		fetch(`/api/me`)
			.then(res => res.json())
			.then(myInfo => { setEditAssignee(myInfo.id || ''); })
			.catch(() => setEditAssignee(''));
	};

	const getSeverityStyle = (severity: string) =>
		severityColors[severity] || { color: '#6b7280', bg: 'rgba(107, 114, 128, 0.1)' };

	const filtered = findings.filter(f => {
		const matchSearch = search.trim() === '' ||
			f.title.toLowerCase().includes(search.toLowerCase()) ||
			f.repo_name.toLowerCase().includes(search.toLowerCase()) ||
			f.file_path.toLowerCase().includes(search.toLowerCase()) ||
			f.detail.toLowerCase().includes(search.toLowerCase());
		const matchType = filterType === '' || f.type === filterType;
		const matchSeverity = filterSeverity === '' || f.severity === filterSeverity;
		return matchSearch && matchType && matchSeverity;
	});

	const totalCount = filtered.length;
	const activeCount = filtered.filter(f => f.status === 'open' || f.status === 'analyzing').length;
	const resolvedCount = filtered.filter(f => f.status === 'resolved').length;
	const doneCount = filtered.filter(f => f.status === 'closed' || f.status === 'invalid').length;

	const renderStatusLogs = (finding: WorkbenchFinding) => {
		if (!finding.status_log) return <div style={{ fontSize: '0.8rem', color: 'var(--text-secondary)' }}>暂无流转历史</div>;
		try {
			let logs: any[] = [];
			if (typeof finding.status_log === 'string') logs = JSON.parse(finding.status_log);
			else if (Array.isArray(finding.status_log)) logs = finding.status_log;
			if (!Array.isArray(logs) || logs.length === 0)
				return <div style={{ fontSize: '0.8rem', color: 'var(--text-secondary)' }}>暂无流转历史</div>;
			return (
				<div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem', textAlign: 'left' }}>
					{logs.map((log: any, idx: number) => (
						<div key={idx} style={{ position: 'relative', paddingLeft: '1.25rem', borderLeft: '2px solid var(--border-color)', paddingBottom: '0.25rem' }}>
							<div style={{ position: 'absolute', left: '-5px', top: '4px', width: '8px', height: '8px', borderRadius: '50%', background: '#3b82f6' }} />
							<div style={{ fontSize: '0.8rem', fontWeight: 600, color: 'var(--text-color)' }}>
								{statusMap[log.status]?.label || log.status}
							</div>
							<div style={{ fontSize: '0.75rem', color: 'var(--text-secondary)', marginTop: '0.1rem' }}>
								操作人: <strong>{log.user || '系统'}</strong> &bull; 时间: {log.time}
							</div>
							{log.comment && (
								<div style={{ fontSize: '0.75rem', color: 'var(--text-color)', background: 'var(--bg-color)', padding: '0.4rem 0.6rem', borderRadius: '4px', marginTop: '0.25rem', border: '1px solid var(--border-color)' }}>
									{log.comment}
								</div>
							)}
							{log.reason && (
								<div style={{ fontSize: '0.75rem', color: 'var(--text-secondary)', marginTop: '0.15rem' }}>
									原因: {log.reason}
								</div>
							)}
						</div>
					))}
				</div>
			);
		} catch {
			return <div style={{ fontSize: '0.8rem', color: 'var(--text-secondary)' }}>日志解析异常</div>;
		}
	};

	const handleOpenReport = async (reportId: number) => {
		setSidebarOpen(true);
		setLoadingMarkdown(true);
		setCurrentMarkdown('');
		setCurrentReportId(reportId);
		try {
			const res = await fetch(`/api/tasks/${reportId}/report`);
			if (res.ok) setCurrentMarkdown(await res.text());
			else {
				const errData = await res.json();
				setCurrentMarkdown(`### 获取报告数据失败\n\n原因: ${errData.error || 'Server error'}`);
			}
		} catch {
			setCurrentMarkdown('### 获取报告数据失败\n\n原因: 网络请求异常。');
		} finally {
			setLoadingMarkdown(false);
		}
	};

	const getScoreColor = (score: number) =>
		score >= 20 ? '#ef4444' : score >= 10 ? '#f59e0b' : '#22c55e';

	const taskStatusBadge = (status: string) => {
		switch (status) {
			case 'success': return { label: '完成', color: '#10b981', bg: 'rgba(16,185,129,0.1)' };
			case 'failed': return { label: '失败', color: '#ef4444', bg: 'rgba(239,68,68,0.1)' };
			case 'running':
			case 'cloning':
			case 'pre_processing':
			case 'analyzing':
			case 'post_processing': return { label: '执行中', color: '#f59e0b', bg: 'rgba(245,158,11,0.1)' };
			case 'queued': return { label: '排队中', color: '#6b7280', bg: 'rgba(107,114,128,0.1)' };
			default: return { label: status, color: '#6b7280', bg: 'rgba(107,114,128,0.1)' };
		}
	};

	return (
		<div style={{ display: 'flex', flexDirection: 'column', gap: '1.5rem', fontFamily: 'system-ui, sans-serif' }}>
			<style>{`
				@keyframes spin { 0% { transform: rotate(0deg); } 100% { transform: rotate(360deg); } }
				@keyframes fadeIn { from { opacity: 0; } to { opacity: 1; } }
				@keyframes slideLeft { from { transform: translateX(100%); } to { transform: translateX(0); } }
				.wb-kanban-col::-webkit-scrollbar { width: 4px; }
				.wb-kanban-col::-webkit-scrollbar-track { background: transparent; }
				.wb-kanban-col::-webkit-scrollbar-thumb { background: var(--border-color); border-radius: 2px; }
			`}</style>

			{/* Metric summary boxes */}
			<div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: '1rem' }}>
				{[
					{ label: '全部分配问题单', count: totalCount, color: 'var(--primary-color)', accent: 'none' },
					{ label: '待处理 / 分析中', count: activeCount, color: '#ef4444', accent: '#ef4444' },
					{ label: '已解决', count: resolvedCount, color: '#10b981', accent: '#10b981' },
					{ label: '已关闭 / 忽略', count: doneCount, color: '#6b7280', accent: '#6b7280' },
				].map(({ label, count, color, accent }) => (
					<div key={label} className="card" style={{
						display: 'flex', flexDirection: 'column', gap: '0.25rem', padding: '1.25rem',
						borderLeft: accent !== 'none' ? `4px solid ${accent}` : undefined
					}}>
						<span style={{ fontSize: '0.78rem', fontWeight: 600, color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.04em' }}>{label}</span>
						<span style={{ fontSize: '1.8rem', fontWeight: 800, color }}>{count} 个</span>
					</div>
				))}
			</div>

			{/* Filter area */}
			<div className="card" style={{ padding: '1rem 1.5rem', display: 'flex', gap: '1rem', flexWrap: 'wrap', alignItems: 'center' }}>
				<div style={{ flex: 1, minWidth: '220px' }}>
					<input
						type="text"
						placeholder="搜索缺陷名称 / 代码仓 / 文件路径..."
						value={search}
						onChange={e => setSearch(e.target.value)}
						style={{ width: '100%', padding: '0.55rem 0.75rem', borderRadius: '6px', border: '1px solid var(--border-color)', outline: 'none', background: 'var(--bg-color)', color: 'var(--text-color)', fontSize: '0.875rem', boxSizing: 'border-box' }}
					/>
				</div>
				<div>
					<select
						value={filterType}
						onChange={e => setFilterType(e.target.value)}
						style={{ padding: '0.55rem 1.5rem 0.55rem 0.75rem', borderRadius: '6px', border: '1px solid var(--border-color)', outline: 'none', background: 'var(--bg-color)', color: 'var(--text-color)', fontSize: '0.875rem', cursor: 'pointer' }}
					>
						<option value="">全部专项类型</option>
						<option value="ut">测试用例有效性</option>
						<option value="coredump">Coredump 风险</option>
						<option value="float">Python 浮点数比较</option>
						<option value="thread">显式创建线程</option>
						<option value="cjson">cJSON 内存泄漏</option>
					</select>
				</div>
				<div>
					<select
						value={filterSeverity}
						onChange={e => setFilterSeverity(e.target.value)}
						style={{ padding: '0.55rem 1.5rem 0.55rem 0.75rem', borderRadius: '6px', border: '1px solid var(--border-color)', outline: 'none', background: 'var(--bg-color)', color: 'var(--text-color)', fontSize: '0.875rem', cursor: 'pointer' }}
					>
						<option value="">所有影响等级</option>
						<option value="阻塞">阻塞 (Blocking)</option>
						<option value="严重">严重 (Critical)</option>
						<option value="主要">主要 (Major)</option>
						<option value="提示">提示 (Hint)</option>
						<option value="建议">建议 (Suggestion)</option>
						<option value="合格">合格 (Pass)</option>
					</select>
				</div>
			</div>

			{/* Loading & Error States */}
			{loading ? (
				<div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', padding: '4rem' }}>
					<div style={{ width: '36px', height: '36px', border: '4px solid var(--border-color)', borderTop: '4px solid var(--primary-color)', borderRadius: '50%', animation: 'spin 1s linear infinite' }} />
					<span style={{ marginTop: '1rem', color: 'var(--text-secondary)', fontSize: '0.9rem' }}>正在加载分配给您的治理问题单...</span>
				</div>
			) : error ? (
				<div style={{ padding: '3rem', textAlign: 'center', background: 'rgba(239, 68, 68, 0.08)', color: '#ef4444', borderRadius: '8px', border: '1px solid rgba(239,68,68,0.2)' }}>
					<h3>加载数据失败</h3>
					<p>{error}</p>
					<button className="btn" onClick={loadFindings} style={{ marginTop: '0.5rem' }}>重试</button>
				</div>
			) : (
				/* Kanban board — 3 merged columns */
				<div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: '1rem', alignItems: 'flex-start' }}>
					{kanbanColumns.map(col => {
						const columnFindings = filtered.filter(f => col.statuses.includes(f.status));
						return (
							<div
								key={col.key}
								style={{
									background: 'var(--bg-color)',
									border: '1px solid var(--border-color)',
									borderTop: `3px solid ${col.color}`,
									borderRadius: '10px',
									padding: '0.75rem',
									display: 'flex',
									flexDirection: 'column',
									gap: '0.6rem',
									maxHeight: '68vh',
								}}
							>
								{/* Column header */}
								<div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '0.15rem 0.25rem' }}>
									<div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', color: col.color }}>
										{col.icon}
										<span style={{ fontSize: '0.875rem', fontWeight: 700, color: 'var(--text-color)' }}>{col.label}</span>
									</div>
									<span style={{ background: col.color + '20', color: col.color, fontSize: '0.72rem', fontWeight: 700, padding: '0.1rem 0.5rem', borderRadius: '10px' }}>
										{columnFindings.length}
									</span>
								</div>

								{/* Cards scroll area */}
								<div className="wb-kanban-col" style={{ display: 'flex', flexDirection: 'column', gap: '0.55rem', overflowY: 'auto', flex: 1 }}>
									{columnFindings.length === 0 ? (
										<div style={{ padding: '2rem 1rem', textAlign: 'center', color: 'var(--text-secondary)', fontSize: '0.75rem', background: 'var(--card-bg)', borderRadius: '8px', border: '1px dashed var(--border-color)' }}>
											无当前状态记录
										</div>
									) : (
										columnFindings.map(f => {
											const sevStyle = getSeverityStyle(f.severity);
											return (
												<div
													key={`${f.type}-${f.id}`}
													onClick={() => openAudit(f)}
													style={{
														background: 'var(--card-bg)',
														border: '1px solid var(--border-color)',
														borderRadius: '8px',
														padding: '0.8rem',
														cursor: 'pointer',
														transition: 'transform 0.15s, box-shadow 0.15s, border-color 0.15s',
														display: 'flex',
														flexDirection: 'column',
														gap: '0.45rem',
														textAlign: 'left',
														flexShrink: 0,
													}}
													onMouseEnter={e => {
														e.currentTarget.style.transform = 'translateY(-2px)';
														e.currentTarget.style.boxShadow = '0 6px 16px rgba(0,0,0,0.1)';
														e.currentTarget.style.borderColor = col.color;
													}}
													onMouseLeave={e => {
														e.currentTarget.style.transform = 'translateY(0)';
														e.currentTarget.style.boxShadow = 'none';
														e.currentTarget.style.borderColor = 'var(--border-color)';
													}}
												>
													<div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '0.25rem' }}>
														<span style={{ fontSize: '0.68rem', padding: '0.1rem 0.4rem', background: 'rgba(59, 130, 246, 0.1)', color: 'var(--primary-color)', borderRadius: '4px', fontWeight: 600, flexShrink: 0 }}>
															{f.type_name}
														</span>
														<span style={{ fontSize: '0.65rem', padding: '0.1rem 0.4rem', background: sevStyle.bg, color: sevStyle.color, borderRadius: '4px', fontWeight: 700, flexShrink: 0 }}>
															{f.severity}
														</span>
													</div>
													<h4 style={{ margin: 0, fontSize: '0.82rem', fontWeight: 600, color: 'var(--text-color)', lineHeight: 1.4, overflow: 'hidden', textOverflow: 'ellipsis', display: '-webkit-box', WebkitLineClamp: 2, WebkitBoxOrient: 'vertical' }}>
														{f.title}
													</h4>
													<div style={{ fontSize: '0.7rem', color: 'var(--text-secondary)', display: 'flex', flexDirection: 'column', gap: '0.1rem' }}>
														<span>📁 {f.repo_name}</span>
														<span style={{ fontFamily: 'monospace', wordBreak: 'break-all' }}>📄 {f.file_path}:{f.line_number}</span>
													</div>
												</div>
											);
										})
									)}
								</div>
							</div>
						);
					})}
				</div>
			)}

			{/* ── Team Repos Section ── */}
			<div>
				<div style={{ display: 'flex', alignItems: 'center', gap: '0.6rem', marginBottom: '0.75rem' }}>
					<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--primary-color)" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
						<path d="M3 9l9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" /><polyline points="9 22 9 12 15 12 15 22" />
					</svg>
					<h3 style={{ margin: 0, fontSize: '1rem', fontWeight: 700, color: 'var(--text-color)' }}>
						团队代码仓报告概况
						{myInfo?.department?.name && (
							<span style={{ fontWeight: 400, color: 'var(--text-secondary)', fontSize: '0.85rem', marginLeft: '0.5rem' }}>
								({myInfo.department.name})
							</span>
						)}
					</h3>
				</div>

				{reposLoading ? (
					<div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', padding: '2rem', color: 'var(--text-secondary)', fontSize: '0.875rem' }}>
						<div style={{ width: '20px', height: '20px', border: '3px solid var(--border-color)', borderTop: '3px solid var(--primary-color)', borderRadius: '50%', animation: 'spin 1s linear infinite', flexShrink: 0 }} />
						正在加载团队代码仓...
					</div>
				) : teamRepos.length === 0 ? (
					<div className="card" style={{ padding: '2rem', textAlign: 'center', color: 'var(--text-secondary)', fontSize: '0.875rem' }}>
						{myInfo?.department_id ? '暂无团队代码仓数据' : '暂未加入任何团队，无法展示代码仓报告。'}
					</div>
				) : (
					<div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
						{teamRepos.map(repo => {
							const tasks = repoTasks[repo.id] || [];
							const shortName = repo.name?.includes(':') ? repo.name.split(':').pop() : repo.name;
							const latestTask = tasks[0];
							return (
								<div key={repo.id} className="card" style={{ padding: '1rem 1.25rem', display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
									{/* Repo header */}
									<div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '0.5rem' }}>
										<div style={{ display: 'flex', alignItems: 'center', gap: '0.6rem', minWidth: 0 }}>
											<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="var(--text-secondary)" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={{ flexShrink: 0 }}>
												<path d="M9 19c-5 1.5-5-2.5-7-3m14 6v-3.87a3.37 3.37 0 0 0-.94-2.61c3.14-.35 6.44-1.54 6.44-7A5.44 5.44 0 0 0 20 4.77 5.07 5.07 0 0 0 19.91 1S18.73.65 16 2.48a13.38 13.38 0 0 0-7 0C6.27.65 5.09 1 5.09 1A5.07 5.07 0 0 0 5 4.77a5.44 5.44 0 0 0-1.5 3.78c0 5.42 3.3 6.61 6.44 7A3.37 3.37 0 0 0 9 18.13V22" />
											</svg>
											<span
												style={{ fontWeight: 700, fontSize: '0.9rem', color: 'var(--text-color)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', cursor: 'pointer' }}
												onClick={() => navigate(appNavigatePath(`/reports/repo/${repo.id}`))}
												title={repo.name}
											>
												{shortName}
											</span>
											{repo.url && (
												<a href={sshToHttps(repo.url)} target="_blank" rel="noreferrer" style={{ color: 'var(--text-secondary)', display: 'flex', flexShrink: 0 }}>
													<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
														<path d="M18 13v6a2 2 0 01-2 2H5a2 2 0 01-2-2V8a2 2 0 012-2h6" /><polyline points="15 3 21 3 21 9" /><line x1="10" y1="14" x2="21" y2="3" />
													</svg>
												</a>
											)}
										</div>
										<div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', flexShrink: 0 }}>
											{latestTask && (
												<>
													{latestTask.score !== undefined && latestTask.status === 'success' && (
														<span style={{ fontWeight: 800, fontSize: '1rem', color: getScoreColor(latestTask.score), minWidth: '2rem', textAlign: 'center' }}>
															{latestTask.score}
														</span>
													)}
													{(() => {
														const b = taskStatusBadge(latestTask.status);
														return (
															<span style={{ fontSize: '0.72rem', padding: '0.15rem 0.5rem', borderRadius: '12px', background: b.bg, color: b.color, fontWeight: 600 }}>
																{b.label}
															</span>
														);
													})()}
												</>
											)}
											<button
												onClick={() => navigate(appNavigatePath(`/reports/repo/${repo.id}`))}
												style={{ background: 'transparent', border: '1px solid var(--border-color)', borderRadius: '5px', padding: '0.2rem 0.6rem', fontSize: '0.75rem', cursor: 'pointer', color: 'var(--text-color)', display: 'flex', alignItems: 'center', gap: '0.3rem' }}
												onMouseEnter={e => { e.currentTarget.style.borderColor = 'var(--primary-color)'; e.currentTarget.style.color = 'var(--primary-color)'; }}
												onMouseLeave={e => { e.currentTarget.style.borderColor = 'var(--border-color)'; e.currentTarget.style.color = 'var(--text-color)'; }}
											>
												<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
													<circle cx="12" cy="12" r="10" /><polyline points="12 6 12 12 16 14" />
												</svg>
												历史报告
											</button>
										</div>
									</div>

									{/* Recent tasks bar */}
									{tasks.length > 0 ? (
										<div style={{ display: 'flex', gap: '0.4rem', flexWrap: 'wrap', alignItems: 'center' }}>
											<span style={{ fontSize: '0.72rem', color: 'var(--text-secondary)', marginRight: '0.25rem' }}>近期报告:</span>
											{tasks.map((task: any) => {
												const b = taskStatusBadge(task.status);
												const date = task.created_at ? task.created_at.substring(0, 10) : '';
												return (
													<button
														key={task.id}
														onClick={() => task.status === 'success' && handleOpenReport(task.id)}
														title={`#${task.id} · ${date}${task.ai_summary ? '\n' + task.ai_summary : ''}`}
														style={{
															display: 'flex', alignItems: 'center', gap: '0.3rem',
															padding: '0.2rem 0.55rem', borderRadius: '12px',
															background: b.bg, color: b.color,
															border: `1px solid ${b.color}25`,
															fontSize: '0.72rem', fontWeight: 600,
															cursor: task.status === 'success' ? 'pointer' : 'default',
															transition: 'opacity 0.15s',
														}}
														onMouseEnter={e => { if (task.status === 'success') e.currentTarget.style.opacity = '0.75'; }}
														onMouseLeave={e => { e.currentTarget.style.opacity = '1'; }}
													>
														{task.status === 'success' && task.score !== undefined && (
															<span style={{ fontWeight: 800, fontSize: '0.75rem' }}>{task.score}</span>
														)}
														<span>{date || `#${task.id}`}</span>
													</button>
												);
											})}
										</div>
									) : (
										<div style={{ fontSize: '0.78rem', color: 'var(--text-secondary)', fontStyle: 'italic' }}>
											暂无最近报告记录
										</div>
									)}
								</div>
							);
						})}
					</div>
				)}
			</div>

			{/* Slide-out detail & audit drawer */}
			{selectedFinding && (
				<div
					onClick={() => setSelectedFinding(null)}
					style={{
						position: 'fixed', inset: 0, background: 'rgba(15,23,42,0.5)', backdropFilter: 'blur(3px)', zIndex: 999,
						animation: 'fadeIn 0.2s ease-out'
					}}
				>
					<div
						onClick={e => e.stopPropagation()}
						style={{
							position: 'fixed', top: 0, right: 0, bottom: 0, width: '550px', maxWidth: '100vw',
							background: 'var(--card-bg)', borderLeft: '1px solid var(--border-color)', boxShadow: '-10px 0 30px rgba(0,0,0,0.2)',
							zIndex: 1000, display: 'flex', flexDirection: 'column', overflow: 'hidden',
							animation: 'slideLeft 0.25s cubic-bezier(0.22, 1, 0.36, 1)'
						}}
					>
						{/* Drawer Header */}
						<div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '1.25rem 1.5rem', borderBottom: '1px solid var(--border-color)' }}>
							<div>
								<span style={{ fontSize: '0.75rem', background: 'rgba(59, 130, 246, 0.1)', color: 'var(--primary-color)', padding: '0.2rem 0.5rem', borderRadius: '4px', fontWeight: 600, marginRight: '0.5rem' }}>
									{selectedFinding.type_name}
								</span>
								<span style={{ color: 'var(--text-secondary)', fontSize: '0.8rem' }}>问题单ID: #{selectedFinding.id}</span>
							</div>
							<button
								onClick={() => setSelectedFinding(null)}
								style={{ background: 'transparent', border: 'none', cursor: 'pointer', color: 'var(--text-secondary)', padding: '4px', display: 'flex', borderRadius: '4px' }}
								onMouseEnter={e => e.currentTarget.style.background = 'var(--bg-color)'}
								onMouseLeave={e => e.currentTarget.style.background = 'transparent'}
							>
								<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
									<line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
								</svg>
							</button>
						</div>

						{/* Drawer Content Area */}
						<div style={{ flex: 1, overflowY: 'auto', padding: '1.5rem', display: 'flex', flexDirection: 'column', gap: '1.25rem' }}>
							<div>
								<h3 style={{ margin: '0 0 0.5rem 0', fontSize: '1rem', fontWeight: 700, color: 'var(--text-color)', textAlign: 'left', lineHeight: 1.4 }}>
									{selectedFinding.title}
								</h3>
								<div style={{ fontSize: '0.8rem', color: 'var(--text-secondary)', background: 'var(--bg-color)', padding: '0.45rem 0.75rem', borderRadius: '4px', border: '1px solid var(--border-color)', display: 'inline-block', width: '100%', boxSizing: 'border-box', textAlign: 'left' }}>
									📁 <strong>代码仓:</strong> {selectedFinding.repo_name}<br />
									📄 <strong>文件位置:</strong> {selectedFinding.file_path}:{selectedFinding.line_number}
								</div>
							</div>

							<div>
								<h4 style={{ margin: '0 0 0.4rem 0', fontSize: '0.875rem', fontWeight: 600, color: 'var(--text-color)', textAlign: 'left' }}>缺陷详情</h4>
								<div style={{ fontSize: '0.85rem', color: 'var(--text-color)', textAlign: 'left', lineHeight: 1.5, background: 'rgba(239, 68, 68, 0.03)', border: '1px solid rgba(239, 68, 68, 0.12)', padding: '0.75rem 1rem', borderRadius: '6px', whiteSpace: 'pre-wrap' }}>
									{selectedFinding.detail}
								</div>
							</div>

							{selectedFinding.code_snippet && (
								<div>
									<h4 style={{ margin: '0 0 0.4rem 0', fontSize: '0.875rem', fontWeight: 600, color: 'var(--text-color)', textAlign: 'left' }}>相关代码片段</h4>
									<pre style={{ margin: 0, padding: '0.85rem', background: '#0f172a', color: '#e2e8f0', borderRadius: '6px', fontSize: '0.775rem', fontFamily: 'monospace', overflowX: 'auto', border: '1px solid #1e293b', textAlign: 'left', lineHeight: 1.4 }}>
										<code>{selectedFinding.code_snippet}</code>
									</pre>
								</div>
							)}

							{selectedFinding.suggestion && selectedFinding.suggestion !== '无' && (
								<div>
									<h4 style={{ margin: '0 0 0.4rem 0', fontSize: '0.875rem', fontWeight: 600, color: '#15803d', textAlign: 'left' }}>修复建议</h4>
									<div style={{ margin: 0, padding: '0.75rem 1rem', background: 'rgba(16, 185, 129, 0.04)', border: '1px solid rgba(16, 185, 129, 0.15)', borderRadius: '6px', fontSize: '0.85rem', color: 'var(--text-color)', lineHeight: 1.5, whiteSpace: 'pre-wrap', textAlign: 'left' }}>
										{selectedFinding.suggestion}
									</div>
								</div>
							)}

							{/* Audit flow form */}
							<div style={{ borderTop: '1px solid var(--border-color)', paddingTop: '1.25rem' }}>
								<h4 style={{ margin: '0 0 1rem 0', fontSize: '0.9rem', fontWeight: 700, textAlign: 'left', color: 'var(--text-color)' }}>治理与审计处理</h4>
								<form onSubmit={handleSaveAudit} style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
									<div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
										<div>
											<label style={{ display: 'block', fontSize: '0.8rem', color: 'var(--text-secondary)', marginBottom: '0.25rem', textAlign: 'left', fontWeight: 500 }}>指派处理人</label>
											<MemberSearchSelect
												value={editAssignee}
												onChange={setEditAssignee}
												style={{ width: '100%' }}
											/>
										</div>
										<div>
											<label style={{ display: 'block', fontSize: '0.8rem', color: 'var(--text-secondary)', marginBottom: '0.25rem', textAlign: 'left', fontWeight: 500 }}>审计治理状态</label>
											<select
												value={editStatus}
												onChange={e => setEditStatus(e.target.value)}
												style={{ width: '100%', padding: '0.6rem 0.75rem', borderRadius: '6px', border: '1px solid var(--border-color)', background: 'var(--bg-color)', color: 'var(--text-color)', fontSize: '0.85rem', outline: 'none', cursor: 'pointer' }}
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
										<label style={{ display: 'block', fontSize: '0.8rem', color: 'var(--text-secondary)', marginBottom: '0.25rem', textAlign: 'left', fontWeight: 500 }}>处理反馈与跟踪意见</label>
										<textarea
											rows={3}
											placeholder="请输入修改说明或验证意见..."
											value={editFeedback}
											onChange={e => setEditFeedback(e.target.value)}
											style={{ width: '100%', padding: '0.5rem 0.75rem', borderRadius: '6px', border: '1px solid var(--border-color)', background: 'var(--card-bg)', color: 'var(--text-color)', fontSize: '0.85rem', outline: 'none', resize: 'vertical', fontFamily: 'inherit', boxSizing: 'border-box' }}
										/>
									</div>

									<div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.75rem', marginTop: '0.5rem' }}>
										<button
											type="button"
											className="btn btn-outline"
											onClick={() => setSelectedFinding(null)}
											style={{ padding: '0.45rem 1.25rem', fontSize: '0.85rem' }}
										>
											取消
										</button>
										<button
											type="submit"
											className="btn"
											disabled={saving}
											style={{ padding: '0.45rem 1.5rem', fontSize: '0.85rem', display: 'flex', alignItems: 'center', gap: '0.35rem' }}
										>
											{saving ? '保存中...' : '提交审计'}
										</button>
									</div>
								</form>
							</div>

							{/* Flow logs timeline */}
							<div style={{ borderTop: '1px solid var(--border-color)', paddingTop: '1.25rem', paddingBottom: '1rem' }}>
								<h4 style={{ margin: '0 0 1rem 0', fontSize: '0.9rem', fontWeight: 700, textAlign: 'left', color: 'var(--text-color)' }}>状态流转演进历史</h4>
								{renderStatusLogs(selectedFinding)}
							</div>
						</div>
					</div>
				</div>
			)}

			<ReportSidebar open={sidebarOpen} onClose={() => setSidebarOpen(false)} markdown={currentMarkdown} loading={loadingMarkdown} reportId={currentReportId} />
		</div>
	);
}
