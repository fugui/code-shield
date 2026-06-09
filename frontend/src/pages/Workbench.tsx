import React, { useEffect, useState } from 'react';
import { useToast } from '../components/Toast';
import MemberSearchSelect from '../components/MemberSearchSelect';

interface WorkbenchFinding {
	id: number;
	type: string;          // "ut", "coredump", "float", "thread", "cjson"
	type_name: string;     // "测试用例有效性", "Coredump 风险", etc.
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
	'阻塞': { color: '#ef4444', bg: 'rgba(239, 68, 68, 0.1)' },
	'严重': { color: '#f97316', bg: 'rgba(249, 115, 22, 0.1)' },
	'主要': { color: '#eab308', bg: 'rgba(234, 179, 8, 0.1)' },
	'提示': { color: '#3b82f6', bg: 'rgba(59, 130, 246, 0.1)' },
	'建议': { color: '#6b7280', bg: 'rgba(107, 114, 128, 0.1)' },
	'合格': { color: '#10b981', bg: 'rgba(16, 185, 129, 0.1)' }
};

const statusMap: Record<string, { label: string; color: string; bg: string }> = {
	'open': { label: '待处理', color: '#ef4444', bg: 'rgba(239, 68, 68, 0.1)' },
	'analyzing': { label: '问题分析', color: '#f59e0b', bg: 'rgba(245, 158, 11, 0.1)' },
	'resolved': { label: '已解决', color: '#10b981', bg: 'rgba(16, 185, 129, 0.1)' },
	'closed': { label: '已关闭', color: '#6b7280', bg: 'rgba(107, 114, 128, 0.1)' },
	'invalid': { label: '忽略/误报', color: '#3b82f6', bg: 'rgba(59, 130, 246, 0.1)' }
};

export default function Workbench() {
	const { showToast } = useToast();
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

	useEffect(() => {
		loadFindings();
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
			loadFindings(); // Reload boards
		} catch (err: any) {
			showToast(err.message || '更新失败', 'error');
		} finally {
			setSaving(false);
		}
	};

	// Open Audit Drawer
	const openAudit = (finding: WorkbenchFinding) => {
		setSelectedFinding(finding);
		setEditStatus(finding.status);
		setEditFeedback('');
		// Prepopulate assignee ID
		fetch(`/api/me`)
			.then(res => res.json())
			.then(myInfo => {
				setEditAssignee(myInfo.id || '');
			})
			.catch(() => setEditAssignee(''));
	};

	const getSeverityStyle = (severity: string) => {
		return severityColors[severity] || { color: '#6b7280', bg: 'rgba(107, 114, 128, 0.1)' };
	};

	// Filter findings
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

	// Metric summaries
	const totalCount = filtered.length;
	const unresolvedCount = filtered.filter(f => f.status === 'open' || f.status === 'analyzing').length;
	const resolvedCount = filtered.filter(f => f.status === 'resolved').length;
	const closedCount = filtered.filter(f => f.status === 'closed' || f.status === 'invalid').length;

	// Helper to render logs list
	const renderStatusLogs = (finding: WorkbenchFinding) => {
		if (!finding.status_log) return <div style={{ fontSize: '0.8rem', color: '#94a3b8' }}>暂无流转历史</div>;
		try {
			// Some status log might be already a parsed array or a JSON string
			let logs: any[] = [];
			if (typeof finding.status_log === 'string') {
				logs = JSON.parse(finding.status_log);
			} else if (Array.isArray(finding.status_log)) {
				logs = finding.status_log;
			}
			if (!Array.isArray(logs) || logs.length === 0) {
				return <div style={{ fontSize: '0.8rem', color: '#94a3b8' }}>暂无流转历史</div>;
			}
			return (
				<div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem', textAlign: 'left' }}>
					{logs.map((log: any, idx: number) => (
						<div key={idx} style={{ position: 'relative', paddingLeft: '1.25rem', borderLeft: '2px solid #e2e8f0', paddingBottom: '0.25rem' }}>
							<div style={{ position: 'absolute', left: '-5px', top: '4px', width: '8px', height: '8px', borderRadius: '50%', background: '#3b82f6' }} />
							<div style={{ fontSize: '0.8rem', fontWeight: 600, color: 'var(--text-color)' }}>
								{statusMap[log.status]?.label || log.status}
							</div>
							<div style={{ fontSize: '0.75rem', color: '#64748b', marginTop: '0.1rem' }}>
								操作人: <strong>{log.user || '系统'}</strong> &bull; 时间: {log.time}
							</div>
							{log.comment && (
								<div style={{ fontSize: '0.75rem', color: 'var(--text-color)', background: 'rgba(0,0,0,0.02)', padding: '0.4rem 0.6rem', borderRadius: '4px', marginTop: '0.25rem' }}>
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
		} catch (e) {
			return <div style={{ fontSize: '0.8rem', color: '#94a3b8' }}>日志解析异常</div>;
		}
	};

	return (
		<div style={{ display: 'flex', flexDirection: 'column', gap: '1.5rem', fontFamily: 'system-ui, sans-serif' }}>
			
			{/* Metric summary boxes */}
			<div style={{ display: 'flex', gap: '1rem', flexWrap: 'wrap' }}>
				<div className="card" style={{ flex: '1 1 200px', display: 'flex', flexDirection: 'column', gap: '0.25rem', padding: '1.25rem' }}>
					<span style={{ fontSize: '0.8rem', fontWeight: 600, color: '#64748b', textTransform: 'uppercase' }}>全部分配问题单</span>
					<span style={{ fontSize: '1.8rem', fontWeight: 800, color: 'var(--text-color)' }}>{totalCount} 个</span>
				</div>
				<div className="card" style={{ flex: '1 1 200px', display: 'flex', flexDirection: 'column', gap: '0.25rem', padding: '1.25rem', borderLeft: '4px solid #ef4444' }}>
					<span style={{ fontSize: '0.8rem', fontWeight: 600, color: '#64748b', textTransform: 'uppercase' }}>待处理/分析中</span>
					<span style={{ fontSize: '1.8rem', fontWeight: 800, color: '#ef4444' }}>{unresolvedCount} 个</span>
				</div>
				<div className="card" style={{ flex: '1 1 200px', display: 'flex', flexDirection: 'column', gap: '0.25rem', padding: '1.25rem', borderLeft: '4px solid #10b981' }}>
					<span style={{ fontSize: '0.8rem', fontWeight: 600, color: '#64748b', textTransform: 'uppercase' }}>已解决</span>
					<span style={{ fontSize: '1.8rem', fontWeight: 800, color: '#10b981' }}>{resolvedCount} 个</span>
				</div>
				<div className="card" style={{ flex: '1 1 200px', display: 'flex', flexDirection: 'column', gap: '0.25rem', padding: '1.25rem', borderLeft: '4px solid #6b7280' }}>
					<span style={{ fontSize: '0.8rem', fontWeight: 600, color: '#64748b', textTransform: 'uppercase' }}>已关闭 / 忽略</span>
					<span style={{ fontSize: '1.8rem', fontWeight: 800, color: '#6b7280' }}>{closedCount} 个</span>
				</div>
			</div>

			{/* Filter area */}
			<div className="card" style={{ padding: '1rem 1.5rem', display: 'flex', gap: '1rem', flexWrap: 'wrap', alignItems: 'center', background: 'white' }}>
				<div style={{ flex: 1, minWidth: '220px' }}>
					<input 
						type="text" 
						placeholder="搜索缺陷名称 / 代码仓 / 文件路径..."
						value={search}
						onChange={e => setSearch(e.target.value)}
						style={{ width: '100%', padding: '0.55rem 0.75rem', borderRadius: '6px', border: '1px solid var(--border-color)', outline: 'none', background: 'var(--bg-color)', color: 'var(--text-color)', fontSize: '0.875rem' }}
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
					<div style={{ width: '36px', height: '36px', border: '4px solid #cbd5e1', borderTop: '4px solid var(--primary-color)', borderRadius: '50%', animation: 'spin 1s linear infinite' }}></div>
					<span style={{ marginTop: '1rem', color: '#64748b', fontSize: '0.9rem' }}>正在加载分配给您的治理问题单...</span>
					<style>{`@keyframes spin { 0% { transform: rotate(0deg); } 100% { transform: rotate(360deg); } }`}</style>
				</div>
			) : error ? (
				<div style={{ padding: '3rem', textAlign: 'center', background: '#fee2e2', color: '#ef4444', borderRadius: '8px', border: '1px solid #fecaca' }}>
					<h3>加载数据失败</h3>
					<p>{error}</p>
					<button className="btn" onClick={loadFindings} style={{ marginTop: '0.5rem' }}>重试</button>
				</div>
			) : (
				/* Kanban board container */
				<div style={{ display: 'flex', gap: '1rem', overflowX: 'auto', paddingBottom: '1rem', alignItems: 'flex-start' }}>
					{Object.entries(statusMap).map(([statusKey, statusMeta]) => {
						const columnFindings = filtered.filter(f => f.status === statusKey);
						return (
							<div 
								key={statusKey} 
								style={{ 
									flex: '0 0 280px', 
									background: '#f1f5f9', 
									borderRadius: '12px', 
									padding: '0.75rem', 
									display: 'flex', 
									flexDirection: 'column', 
									gap: '0.75rem', 
									maxHeight: '75vh',
									boxShadow: 'inset 0 1px 2px rgba(0,0,0,0.02)'
								}}
							>
								{/* Column header */}
								<div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '0.25rem 0.5rem' }}>
									<div style={{ display: 'flex', alignItems: 'center', gap: '0.4rem' }}>
										<span style={{ width: '8px', height: '8px', borderRadius: '50%', background: statusMeta.color }} />
										<span style={{ fontSize: '0.9rem', fontWeight: 700, color: '#334155' }}>{statusMeta.label}</span>
									</div>
									<span style={{ background: '#cbd5e1', color: '#475569', fontSize: '0.75rem', fontWeight: 700, padding: '0.1rem 0.4rem', borderRadius: '10px' }}>
										{columnFindings.length}
									</span>
								</div>

								{/* Cards scroll area */}
								<div style={{ display: 'flex', flexDirection: 'column', gap: '0.6rem', overflowY: 'auto', flex: 1, padding: '0.1rem' }}>
									{columnFindings.length === 0 ? (
										<div style={{ padding: '2rem 1rem', textAlign: 'center', color: '#94a3b8', fontSize: '0.75rem', background: '#f8fafc', borderRadius: '8px', border: '1px dashed #cbd5e1' }}>
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
														background: 'white',
														border: '1px solid var(--border-color)',
														borderRadius: '8px',
														padding: '0.85rem',
														cursor: 'pointer',
														boxShadow: '0 1px 3px rgba(0,0,0,0.02)',
														transition: 'transform 0.2s, box-shadow 0.2s',
														display: 'flex',
														flexDirection: 'column',
														gap: '0.5rem',
														textAlign: 'left'
													}}
													onMouseEnter={e => {
														e.currentTarget.style.transform = 'translateY(-2px)';
														e.currentTarget.style.boxShadow = '0 6px 12px rgba(0,0,0,0.05)';
														e.currentTarget.style.borderColor = 'var(--primary-color)';
													}}
													onMouseLeave={e => {
														e.currentTarget.style.transform = 'translateY(0)';
														e.currentTarget.style.boxShadow = '0 1px 3px rgba(0,0,0,0.02)';
														e.currentTarget.style.borderColor = 'var(--border-color)';
													}}
												>
													<div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '0.25rem' }}>
														<span style={{ fontSize: '0.7rem', padding: '0.1rem 0.4rem', background: 'rgba(59, 130, 246, 0.08)', color: 'var(--primary-color)', borderRadius: '4px', fontWeight: 600 }}>
															{f.type_name}
														</span>
														<span style={{ fontSize: '0.65rem', padding: '0.1rem 0.4rem', background: sevStyle.bg, color: sevStyle.color, borderRadius: '4px', fontWeight: 700 }}>
															{f.severity}
														</span>
													</div>
													<h4 style={{ margin: 0, fontSize: '0.825rem', fontWeight: 600, color: 'var(--text-color)', lineHeight: 1.4, overflow: 'hidden', textOverflow: 'ellipsis', display: '-webkit-box', WebkitLineClamp: 2, WebkitBoxOrient: 'vertical' }}>
														{f.title}
													</h4>
													<div style={{ fontSize: '0.725rem', color: '#64748b', display: 'flex', flexDirection: 'column', gap: '0.15rem' }}>
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

			{/* Slide-out detail & audit drawer */}
			{selectedFinding && (
				<div 
					onClick={() => setSelectedFinding(null)}
					style={{
						position: 'fixed', inset: 0, background: 'rgba(15,23,42,0.4)', backdropFilter: 'blur(3px)', zIndex: 999,
						animation: 'fadeIn 0.2s ease-out'
					}}
				>
					<div 
						onClick={e => e.stopPropagation()}
						style={{
							position: 'fixed', top: 0, right: 0, bottom: 0, width: '550px', maxWidth: '100vw',
							background: 'white', borderLeft: '1px solid var(--border-color)', boxShadow: '-10px 0 25px rgba(0,0,0,0.15)',
							zIndex: 1000, display: 'flex', flexDirection: 'column', overflow: 'hidden',
							animation: 'slideLeft 0.25s cubic-bezier(0.22, 1, 0.36, 1)'
						}}
					>
						<style>{`
							@keyframes fadeIn { from { opacity: 0; } to { opacity: 1; } }
							@keyframes slideLeft { from { transform: translateX(100%); } to { transform: translateX(0); } }
						`}</style>

						{/* Drawer Header */}
						<div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '1.25rem 1.5rem', borderBottom: '1px solid var(--border-color)' }}>
							<div>
								<span style={{ fontSize: '0.75rem', background: 'rgba(59, 130, 246, 0.08)', color: 'var(--primary-color)', padding: '0.2rem 0.5rem', borderRadius: '4px', fontWeight: 600, marginRight: '0.5rem' }}>
									{selectedFinding.type_name}
								</span>
								<span style={{ color: '#64748b', fontSize: '0.8rem' }}>问题单ID: #{selectedFinding.id}</span>
							</div>
							<button 
								onClick={() => setSelectedFinding(null)}
								style={{ background: 'transparent', border: 'none', cursor: 'pointer', color: '#94a3b8', padding: '4px', display: 'flex' }}
							>
								<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
									<line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line>
								</svg>
							</button>
						</div>

						{/* Drawer Content Area */}
						<div style={{ flex: 1, overflowY: 'auto', padding: '1.5rem', display: 'flex', flexDirection: 'column', gap: '1.25rem' }}>
							<div>
								<h3 style={{ margin: '0 0 0.5rem 0', fontSize: '1rem', fontWeight: 700, color: 'var(--text-color)', textAlign: 'left', lineHeight: 1.4 }}>
									{selectedFinding.title}
								</h3>
								<div style={{ fontSize: '0.8rem', color: '#64748b', background: 'var(--bg-color)', padding: '0.45rem 0.75rem', borderRadius: '4px', border: '1px solid var(--border-color)', display: 'inline-block', width: '100%', boxSizing: 'border-box', textAlign: 'left' }}>
									📁 <strong>代码仓:</strong> {selectedFinding.repo_name}<br/>
									📄 <strong>文件位置:</strong> {selectedFinding.file_path}:{selectedFinding.line_number}
								</div>
							</div>

							{/* Detail description */}
							<div>
								<h4 style={{ margin: '0 0 0.4rem 0', fontSize: '0.875rem', fontWeight: 600, color: 'var(--text-color)', textAlign: 'left' }}>缺陷详情</h4>
								<div style={{ fontSize: '0.85rem', color: 'var(--text-color)', textAlign: 'left', lineHeight: 1.5, background: 'rgba(239, 68, 68, 0.02)', border: '1px solid rgba(239, 68, 68, 0.08)', padding: '0.75rem 1rem', borderRadius: '6px', whiteSpace: 'pre-wrap' }}>
									{selectedFinding.detail}
								</div>
							</div>

							{/* Code Snippet */}
							{selectedFinding.code_snippet && (
								<div>
									<h4 style={{ margin: '0 0 0.4rem 0', fontSize: '0.875rem', fontWeight: 600, color: 'var(--text-color)', textAlign: 'left' }}>相关代码片段</h4>
									<pre style={{ margin: 0, padding: '0.85rem', background: '#0f172a', color: '#e2e8f0', borderRadius: '6px', fontSize: '0.775rem', fontFamily: 'monospace', overflowX: 'auto', border: '1px solid #1e293b', textAlign: 'left', lineHeight: 1.4 }}>
										<code>{selectedFinding.code_snippet}</code>
									</pre>
								</div>
							)}

							{/* Suggestion */}
							{selectedFinding.suggestion && selectedFinding.suggestion !== '无' && (
								<div>
									<h4 style={{ margin: '0 0 0.4rem 0', fontSize: '0.875rem', fontWeight: 600, color: '#15803d', textAlign: 'left' }}>修复建议</h4>
									<div style={{ margin: 0, padding: '0.75rem 1rem', background: 'rgba(16, 185, 129, 0.02)', border: '1px solid rgba(16, 185, 129, 0.08)', borderRadius: '6px', fontSize: '0.85rem', color: 'var(--text-color)', lineHeight: 1.5, whiteSpace: 'pre-wrap', textAlign: 'left' }}>
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
											<label style={{ display: 'block', fontSize: '0.8rem', color: '#64748b', marginBottom: '0.25rem', textAlign: 'left', fontWeight: 500 }}>指派处理人</label>
											<MemberSearchSelect 
												value={editAssignee}
												onChange={setEditAssignee}
												style={{ width: '100%' }}
											/>
										</div>
										<div>
											<label style={{ display: 'block', fontSize: '0.8rem', color: '#64748b', marginBottom: '0.25rem', textAlign: 'left', fontWeight: 500 }}>审计治理状态</label>
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
										<label style={{ display: 'block', fontSize: '0.8rem', color: '#64748b', marginBottom: '0.25rem', textAlign: 'left', fontWeight: 500 }}>处理反馈与跟踪意见</label>
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
		</div>
	);
}
