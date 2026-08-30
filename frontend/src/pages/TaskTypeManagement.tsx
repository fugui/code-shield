import React, { useEffect, useState } from 'react';
import { useConfirm, Modal, Drawer, EmptyState } from '@code/common';
import { useToast } from '../components/Toast';
import { Code2, Settings, Trash2 } from 'lucide-react';


type FileTab = 'analysis_prompt' | 'synthesis_prompt' | 'precondition';

function TaskTypeManagement() {
  const { showToast } = useToast();
  const [taskTypes, setTaskTypes] = useState<any[]>([]);
  const [editingId, setEditingId] = useState<number | null>(null);
  const [form, setForm] = useState({
    name: '', display_name: '', description: '', engine_mode: 'single', engine_config: '',
    ai_backend: '', target_scope: 'business',
    notify_template: '', notify_threshold: 0, notify_cc: [] as string[], timeout: 60, is_active: true,
    is_campaign: false, campaign_path: '', governance_mode: 'defect_tracking', campaign_icon: '', campaign_config: ''
  });
  const [ccInput, setCcInput] = useState('');
  const [showForm, setShowForm] = useState(false);

  // File editor state
  const [showFileEditor, setShowFileEditor] = useState(false);
  const [fileEditorTaskId, setFileEditorTaskId] = useState<number | null>(null);
  const [fileEditorTaskName, setFileEditorTaskName] = useState('');
  const [activeFileTab, setActiveFileTab] = useState<FileTab>('analysis_prompt');
  const [fileContents, setFileContents] = useState({ analysis_prompt: '', synthesis_prompt: '', precondition: '' });
  const [fileDirty, setFileDirty] = useState({ analysis_prompt: false, synthesis_prompt: false, precondition: false });
  const [fileSaving, setFileSaving] = useState(false);

  const fetchTaskTypes = async () => {
    const res = await fetch('/api/task-types');
    if (res.ok) setTaskTypes(await res.json());
  };

  useEffect(() => { fetchTaskTypes(); }, []);

  const resetForm = () => {
    setForm({
      name: '', display_name: '', description: '', engine_mode: 'single', engine_config: '',
      ai_backend: '', target_scope: 'business', notify_template: '', notify_threshold: 0,
      notify_cc: [], timeout: 60, is_active: true,
      is_campaign: false, campaign_path: '', governance_mode: 'defect_tracking', campaign_icon: '', campaign_config: ''
    });
    setEditingId(null);
    setCcInput('');
  };

  const handleEdit = (tt: any) => {
    let ccList: string[] = [];
    if (tt.notify_cc) {
      try { ccList = typeof tt.notify_cc === 'string' ? JSON.parse(tt.notify_cc) : tt.notify_cc; } catch { ccList = []; }
    }
    let configStr = '';
    if (tt.engine_config) {
      configStr = typeof tt.engine_config === 'string' ? tt.engine_config : JSON.stringify(tt.engine_config, null, 2);
    }
    let campConfigStr = '';
    if (tt.campaign_config) {
      campConfigStr = typeof tt.campaign_config === 'string' ? tt.campaign_config : JSON.stringify(tt.campaign_config, null, 2);
    }
    setForm({
      name: tt.name, display_name: tt.display_name, description: tt.description || '',
      engine_mode: tt.engine_mode || 'single', engine_config: configStr,
      ai_backend: tt.ai_backend || '', target_scope: tt.target_scope || 'business',
      notify_template: tt.notify_template || '',
      notify_threshold: tt.notify_threshold || 0, notify_cc: ccList, timeout: tt.timeout || 60, is_active: tt.is_active,
      is_campaign: !!tt.is_campaign,
      campaign_path: tt.campaign_path || '',
      governance_mode: tt.governance_mode || 'defect_tracking',
      campaign_icon: tt.campaign_icon || '',
      campaign_config: campConfigStr
    });
    setCcInput('');
    setEditingId(tt.id);
    setShowForm(true);
  };

  const handleAddCc = () => {
    const email = ccInput.trim();
    if (!email) return;
    if (!/\S+@\S+\.\S+/.test(email)) { showToast('请输入有效的邮箱地址', 'error'); return; }
    if (form.notify_cc.includes(email)) { showToast('该邮箱已添加', 'error'); return; }
    setForm({ ...form, notify_cc: [...form.notify_cc, email] });
    setCcInput('');
  };

  const handleRemoveCc = (email: string) => {
    setForm({ ...form, notify_cc: form.notify_cc.filter(e => e !== email) });
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const url = editingId ? `/api/task-types/${editingId}` : '/api/task-types';
    const method = editingId ? 'PATCH' : 'POST';
    
    const payload = { ...form };
    if (payload.engine_config) {
      try {
        (payload as any).engine_config = JSON.parse(payload.engine_config);
      } catch (e) {
        showToast('引擎配置必须是有效的 JSON', 'error');
        return;
      }
    } else {
      (payload as any).engine_config = null;
    }

    if (payload.campaign_config) {
      try {
        (payload as any).campaign_config = JSON.parse(payload.campaign_config);
      } catch (e) {
        showToast('专项高级配置必须是有效的 JSON', 'error');
        return;
      }
    } else {
      (payload as any).campaign_config = null;
    }

    const res = await fetch(url, {
      method, headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
    if (res.ok) {
      showToast(editingId ? '任务类型已更新' : '任务类型已创建', 'success');
      setShowForm(false);
      resetForm();
      fetchTaskTypes();
      window.dispatchEvent(new CustomEvent('shield-task-types-changed'));
    } else {
      const d = await res.json();
      showToast(d.error || '操作失败', 'error');
    }
  };

  const handleDelete = async (id: number) => {
    if (!window.confirm('确认删除此任务类型？')) return;
    const res = await fetch(`/api/task-types/${id}`, { method: 'DELETE' });
    if (res.ok) {
      showToast('已删除', 'success');
      fetchTaskTypes();
      window.dispatchEvent(new CustomEvent('shield-task-types-changed'));
    } else {
      const d = await res.json();
      showToast(d.error || '删除失败', 'error');
    }
  };

  const handleToggleActive = async (tt: any) => {
    const res = await fetch(`/api/task-types/${tt.id}`, {
      method: 'PATCH', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ is_active: !tt.is_active })
    });
    if (res.ok) {
      fetchTaskTypes();
      window.dispatchEvent(new CustomEvent('shield-task-types-changed'));
    }
  };

  // File editor functions
  const openFileEditor = async (tt: any) => {
    setFileEditorTaskId(tt.id);
    setFileEditorTaskName(tt.display_name);
    setActiveFileTab('analysis_prompt');
    setFileDirty({ analysis_prompt: false, synthesis_prompt: false, precondition: false });
    try {
      const res = await fetch(`/api/task-types/${tt.id}/files`);
      if (res.ok) {
        const data = await res.json();
        setFileContents({ analysis_prompt: data.analysis_prompt || '', synthesis_prompt: data.synthesis_prompt || '', precondition: data.precondition || '' });
      }
    } catch { /* ignore */ }
    setShowFileEditor(true);
  };

  const handleFileSave = async (fileType: FileTab) => {
    if (!fileEditorTaskId) return;
    setFileSaving(true);
    try {
      const res = await fetch(`/api/task-types/${fileEditorTaskId}/files/${fileType}`, {
        method: 'PUT', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content: fileContents[fileType] })
      });
      if (res.ok) {
        showToast('文件已保存', 'success');
        setFileDirty({ ...fileDirty, [fileType]: false });
      } else {
        const d = await res.json();
        showToast(d.error || '保存失败', 'error');
      }
    } catch { showToast('网络错误', 'error'); }
    setFileSaving(false);
  };

  const updateFileContent = (tab: FileTab, content: string) => {
    setFileContents({ ...fileContents, [tab]: content });
    setFileDirty({ ...fileDirty, [tab]: true });
  };

  const fieldStyle: React.CSSProperties = { width: '100%', padding: '0.6rem', borderRadius: '6px', border: '1px solid var(--border-color)', outline: 'none', boxSizing: 'border-box', fontSize: '0.875rem' };
  const labelStyle: React.CSSProperties = { display: 'block', marginBottom: '0.4rem', fontSize: '0.8rem', color: '#64748b', fontWeight: 600 };

  const fileTabLabels: Record<FileTab, string> = { analysis_prompt: '分析提示词', synthesis_prompt: '综合报告提示词', precondition: '前置检查脚本' };

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.5rem' }}>
        <p style={{ color: '#64748b', margin: 0, fontSize: '0.875rem' }}>管理系统支持的任务类型，配置 Prompt 文件和前置脚本</p>
        <button className="btn" onClick={() => { resetForm(); setShowForm(true); }}>+ 新建任务类型</button>
      </div>

      <div style={{ padding: 0, overflow: 'hidden' }}>
        <table style={{ width: '100%', borderCollapse: 'collapse' }}>
          <thead>
            <tr style={{ borderBottom: '1px solid var(--border-color)', color: '#64748b', fontSize: '0.875rem', textAlign: 'left', background: 'var(--bg-color)' }}>
              <th style={{ padding: '1rem' }}>名称</th>
              <th style={{ padding: '1rem' }}>标识</th>
              <th style={{ padding: '1rem' }}>执行引擎</th>
              <th style={{ padding: '1rem' }}>AI 后端</th>
              <th style={{ padding: '1rem' }}>超时(分钟)</th>
              <th style={{ padding: '1rem' }}>通知阈值</th>
              <th style={{ padding: '1rem' }}>状态</th>
              <th style={{ padding: '1rem', textAlign: 'right' }}>操作</th>
            </tr>
          </thead>
          <tbody>
            {taskTypes.length === 0 ? (
              <EmptyState
                inTable
                colSpan={8}
                type="data"
                title="暂无任务类型"
                description="任务类型定义了扫描与分析规则的执行引擎、AI 后端与处理流程。"
                action={
                  <button className="btn" onClick={() => { resetForm(); setShowForm(true); }} style={{ padding: '0.45rem 1rem', fontSize: '0.85rem' }}>
                    新建任务类型
                  </button>
                }
              />
            ) : taskTypes.map(tt => (

              <tr key={tt.id} style={{ borderBottom: '1px solid var(--border-color)' }}>
                <td style={{ padding: '1rem', fontWeight: 500 }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', flexWrap: 'wrap' }}>
                    {tt.display_name}
                    {tt.is_builtin && <span style={{ fontSize: '0.7rem', background: '#dbeafe', color: '#2563eb', padding: '0.1rem 0.4rem', borderRadius: '4px' }}>内置</span>}
                    {tt.is_campaign && (
                      <span style={{
                        fontSize: '0.7rem',
                        background: tt.governance_mode === 'entity_assessment' ? 'rgba(16, 185, 129, 0.1)' : 'rgba(99, 102, 241, 0.1)',
                        color: tt.governance_mode === 'entity_assessment' ? '#059669' : '#4f46e5',
                        border: `1px solid ${tt.governance_mode === 'entity_assessment' ? 'rgba(16, 185, 129, 0.3)' : 'rgba(99, 102, 241, 0.3)'}`,
                        padding: '0.1rem 0.4rem',
                        borderRadius: '4px',
                        fontWeight: 600
                      }}>
                        {tt.governance_mode === 'entity_assessment' ? '专项 · 实体评估' : '专项 · 缺陷攻关'}
                      </span>
                    )}
                  </div>
                </td>
                <td style={{ padding: '1rem', fontFamily: 'monospace', fontSize: '0.8rem', color: '#64748b' }}>{tt.name}</td>
                <td style={{ padding: '1rem', fontSize: '0.85rem' }}>
                  {tt.engine_mode === 'chunked' ? (
                    <span style={{ color: '#0284c7', background: 'rgba(2,132,199,0.08)', padding: '0.15rem 0.4rem', borderRadius: '4px', fontWeight: 500 }}>分片引擎</span>
                  ) : (
                    <span style={{ color: '#64748b', background: 'rgba(100,116,139,0.08)', padding: '0.15rem 0.4rem', borderRadius: '4px' }}>单引擎</span>
                  )}
                </td>
                <td style={{ padding: '1rem', fontSize: '0.85rem' }}>
                  {tt.ai_backend === 'claude' ? (
                    <span className="code-badge code-badge-backend--claude">Claude</span>
                  ) : tt.ai_backend === 'opencode' ? (
                    <span className="code-badge code-badge-backend--opencode">OpenCode</span>
                  ) : tt.ai_backend === 'codex' ? (
                    <span className="code-badge code-badge-backend--codex">Codex</span>
                  ) : (
                    <span className="code-badge code-badge-backend--default">跟随全局</span>
                  )}
                </td>
                <td style={{ padding: '1rem' }}>{tt.timeout}</td>
                <td style={{ padding: '1rem' }}>{tt.notify_threshold}</td>
                <td style={{ padding: '1rem' }}>
                  <div onClick={() => handleToggleActive(tt)} style={{ display: 'inline-flex', alignItems: 'center', gap: '0.5rem', cursor: 'pointer' }}>
                    <div style={{ width: 34, height: 20, borderRadius: 10, background: tt.is_active ? 'var(--primary-color)' : '#cbd5e1', position: 'relative', transition: '0.2s' }}>
                      <div style={{ width: 16, height: 16, borderRadius: 8, background: 'white', position: 'absolute', top: 2, left: tt.is_active ? 16 : 2, transition: '0.2s' }} />
                    </div>
                    <span style={{ fontSize: '0.8rem', color: tt.is_active ? 'var(--text-color)' : '#94a3b8' }}>{tt.is_active ? '启用' : '停用'}</span>
                  </div>
                </td>
                <td style={{ padding: '1rem', textAlign: 'right' }}>
                  <div style={{ display: 'flex', gap: '0.8rem', justifyContent: 'flex-end', alignItems: 'center' }}>
                    <span title="编辑脚本" onClick={() => openFileEditor(tt)} style={{ cursor: 'pointer', display: 'flex' }}>
                      <Code2 size={18} color="#10b981" />
                    </span>
                    <span title="配置" onClick={() => handleEdit(tt)} style={{ cursor: 'pointer', display: 'flex' }}>
                      <Settings size={18} color="#64748b" />
                    </span>
                    {!tt.is_builtin && (
                      <span title="删除" onClick={() => handleDelete(tt.id)} style={{ cursor: 'pointer', display: 'flex' }}>
                        <Trash2 size={18} color="#dc2626" />
                      </span>
                    )}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* Config Modal */}
      <Modal
        open={showForm}
        onClose={() => { setShowForm(false); resetForm(); }}
        title={editingId ? '编辑任务类型' : '新建任务类型'}
        width="md"
        footer={
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.75rem', width: '100%' }}>
            <button type="button" onClick={() => { setShowForm(false); resetForm(); }} style={{ padding: '0.5rem 1.25rem', border: '1px solid var(--border-color)', background: 'transparent', borderRadius: '6px', cursor: 'pointer', fontSize: '0.875rem' }}>取消</button>
            <button type="button" onClick={handleSubmit} className="btn" style={{ padding: '0.5rem 1.25rem' }}>{editingId ? '保存修改' : '创建'}</button>
          </div>
        }
      >
        <form onSubmit={handleSubmit} style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
            <div>
              <label style={labelStyle}>标识名称（英文）</label>
              <input required style={fieldStyle} value={form.name} onChange={e => setForm({...form, name: e.target.value})} placeholder="如: security_scan" disabled={!!editingId} />
            </div>
            <div>
              <label style={labelStyle}>显示名称</label>
              <input required style={fieldStyle} value={form.display_name} onChange={e => setForm({...form, display_name: e.target.value})} placeholder="如: 安全漏洞扫描" />
            </div>
          </div>
          <div>
            <label style={labelStyle}>描述</label>
            <textarea style={{...fieldStyle, minHeight: '60px', resize: 'vertical'}} value={form.description} onChange={e => setForm({...form, description: e.target.value})} placeholder="任务说明..." />
          </div>

          {/* 专项分析元数据治理配置卡片 */}
          <div style={{ background: 'var(--bg-color)', border: '1px solid var(--border-color)', borderRadius: '8px', padding: '1rem', display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <div>
                <span style={{ fontWeight: 600, fontSize: '0.9rem', color: 'var(--text-color)' }}>启用专项分析看板与闭环治理</span>
                <p style={{ margin: '0.2rem 0 0', fontSize: '0.75rem', color: '#94a3b8' }}>开启后自动在侧边栏挂载专项菜单，启用通用归并引擎、部门排名与 30 天收敛趋势跟踪</p>
              </div>
              <div onClick={() => setForm({ ...form, is_campaign: !form.is_campaign })} style={{ display: 'inline-flex', alignItems: 'center', cursor: 'pointer' }}>
                <div style={{ width: 38, height: 22, borderRadius: 11, background: form.is_campaign ? 'var(--primary-color)' : '#cbd5e1', position: 'relative', transition: '0.2s' }}>
                  <div style={{ width: 18, height: 18, borderRadius: 9, background: 'white', position: 'absolute', top: 2, left: form.is_campaign ? 18 : 2, transition: '0.2s' }} />
                </div>
              </div>
            </div>

            {form.is_campaign && (
              <div style={{ borderTop: '1px solid var(--border-color)', paddingTop: '0.75rem', display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
                <div>
                  <label style={labelStyle}>专项治理模式</label>
                  <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.75rem' }}>
                    <div
                      onClick={() => setForm({ ...form, governance_mode: 'defect_tracking' })}
                      style={{
                        padding: '0.75rem', borderRadius: '6px', cursor: 'pointer',
                        border: `1.5px solid ${form.governance_mode === 'defect_tracking' ? 'var(--primary-color)' : 'var(--border-color)'}`,
                        background: form.governance_mode === 'defect_tracking' ? 'rgba(37,99,235,0.05)' : 'var(--card-bg)'
                      }}
                    >
                      <div style={{ fontWeight: 600, fontSize: '0.85rem', color: form.governance_mode === 'defect_tracking' ? 'var(--primary-color)' : 'var(--text-color)' }}>
                        缺陷攻关模式 (defect_tracking)
                      </div>
                      <div style={{ fontSize: '0.75rem', color: '#64748b', marginTop: '0.2rem' }}>
                        度量缺陷总数/待处理/修复率，适合浮点数、内存泄露、代码检视等缺陷发现
                      </div>
                    </div>
                    <div
                      onClick={() => setForm({ ...form, governance_mode: 'entity_assessment' })}
                      style={{
                        padding: '0.75rem', borderRadius: '6px', cursor: 'pointer',
                        border: `1.5px solid ${form.governance_mode === 'entity_assessment' ? 'var(--primary-color)' : 'var(--border-color)'}`,
                        background: form.governance_mode === 'entity_assessment' ? 'rgba(16,185,129,0.05)' : 'var(--card-bg)'
                      }}
                    >
                      <div style={{ fontWeight: 600, fontSize: '0.85rem', color: form.governance_mode === 'entity_assessment' ? '#059669' : 'var(--text-color)' }}>
                        全量实体评估模式 (entity_assessment)
                      </div>
                      <div style={{ fontSize: '0.75rem', color: '#64748b', marginTop: '0.2rem' }}>
                        度量实体总数/合格数/合格率，适合单元测试用例有效性等全量评级
                      </div>
                    </div>
                  </div>
                </div>

                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
                  <div>
                    <label style={labelStyle}>路由别名 (URL Path) <span style={{ fontWeight: 400, color: '#94a3b8' }}>(空则同标识名)</span></label>
                    <input style={fieldStyle} value={form.campaign_path} onChange={e => setForm({ ...form, campaign_path: e.target.value.trim() })} placeholder="如: float, ut, coredump" />
                  </div>
                  <div>
                    <label style={labelStyle}>专项图标类名/SVG <span style={{ fontWeight: 400, color: '#94a3b8' }}>(可选)</span></label>
                    <input style={fieldStyle} value={form.campaign_icon} onChange={e => setForm({ ...form, campaign_icon: e.target.value })} placeholder="SVG path 或图标名称" />
                  </div>
                </div>
              </div>
            )}
          </div>

          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
            <div>
              <label style={labelStyle}>执行模式</label>
              <select style={fieldStyle} value={form.engine_mode} onChange={e => setForm({...form, engine_mode: e.target.value})}>
                <option value="single">单引擎 (single)</option>
                <option value="chunked">分片引擎 (chunked)</option>
              </select>
            </div>
            <div style={{ display: 'flex', flexDirection: 'column' }}>
              <label style={labelStyle}>引擎配置 <span style={{ fontWeight: 400, color: '#94a3b8' }}>(JSON，可选)</span></label>
              <textarea
                style={{...fieldStyle, minHeight: '60px', resize: 'vertical', fontFamily: "'JetBrains Mono', 'Fira Code', monospace", fontSize: '0.8rem'}}
                value={form.engine_config}
                onChange={e => setForm({...form, engine_config: e.target.value})}
                placeholder={'{\n  "max_files": 50,\n  "depth": 1\n}'}
              />
            </div>
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
            <div>
              <label style={labelStyle}>AI 后端 <span style={{ fontWeight: 400, color: 'var(--color-text-muted)' }}>(默认值，定时策略可覆盖)</span></label>
              <select style={fieldStyle} value={form.ai_backend} onChange={e => setForm({...form, ai_backend: e.target.value})}>
                <option value="">跟随全局配置</option>
                <option value="claude">Claude</option>
                <option value="opencode">OpenCode</option>
                <option value="codex">Codex</option>
              </select>
            </div>
            <div>
              <label style={labelStyle}>处理范围 <span style={{ fontWeight: 400, color: '#94a3b8' }}>(默认值，可被覆盖)</span></label>
              <select style={fieldStyle} value={form.target_scope} onChange={e => setForm({...form, target_scope: e.target.value})}>
                <option value="all">全部代码 (源码与测试)</option>
                <option value="business">仅业务代码 (跳过测试)</option>
                <option value="test">仅测试代码</option>
              </select>
            </div>
          </div>
          <div style={{ background: '#f0fdf4', border: '1px solid #bbf7d0', borderRadius: '6px', padding: '0.6rem 1rem', fontSize: '0.8rem', color: '#15803d' }}>
              💡 提示词和脚本文件位于 <code style={{ background: '#dcfce7', padding: '0.1rem 0.3rem', borderRadius: '3px' }}>tasks/{(form.name || '<标识名>').replace(/_/g, '-')}/</code> 目录下{editingId ? '，可通过「编辑脚本」修改内容' : '，创建后可通过「编辑脚本」修改内容'}。
            </div>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
            <div>
              <label style={labelStyle}>执行超时（分钟）</label>
              <input type="number" style={fieldStyle} value={form.timeout} onChange={e => setForm({...form, timeout: parseInt(e.target.value) || 60})} />
            </div>
            <div>
              <label style={labelStyle}>通知阈值（风险评分≥此值才通知）</label>
              <input type="number" style={fieldStyle} value={form.notify_threshold} onChange={e => setForm({...form, notify_threshold: parseInt(e.target.value) || 0})} />
            </div>
          </div>
          <div>
            <label style={labelStyle}>通知抄送 <span style={{ fontWeight: 400, color: '#94a3b8' }}>（任务完成后额外抄送的邮箱）</span></label>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.4rem', padding: '0.5rem', border: '1px solid var(--border-color)', borderRadius: '6px', minHeight: '38px', alignItems: 'center' }}>
              {form.notify_cc.map(email => (
                <span key={email} style={{ display: 'inline-flex', alignItems: 'center', gap: '0.3rem', background: 'rgba(37,99,235,0.08)', color: 'var(--primary-color)', padding: '0.2rem 0.5rem', borderRadius: '4px', fontSize: '0.8rem' }}>
                  {email}
                  <span onClick={() => handleRemoveCc(email)} style={{ cursor: 'pointer', color: '#94a3b8', fontWeight: 700, fontSize: '0.9rem', lineHeight: 1 }}>×</span>
                </span>
              ))}
              <input
                type="email"
                value={ccInput}
                onChange={e => setCcInput(e.target.value)}
                onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); handleAddCc(); } }}
                placeholder={form.notify_cc.length === 0 ? '输入邮箱后按回车添加' : '继续添加...'}
                style={{ border: 'none', outline: 'none', flex: 1, minWidth: '150px', fontSize: '0.85rem', padding: '0.2rem' }}
              />
            </div>
          </div>
        </form>
      </Modal>

      {/* File Editor Drawer */}
      <Drawer
        open={showFileEditor}
        onClose={() => setShowFileEditor(false)}
        title={`编辑脚本 — ${fileEditorTaskName}`}
        subtitle="配置 AI 任务提示词（分析/综合阶段）与前置 Bash 检查脚本"
        width="min(1000px, 92vw)"
        bodyStyle={{ padding: 0, gap: 0, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}
        footer={
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', width: '100%' }}>
            <span style={{ fontSize: '0.78rem', color: '#94a3b8' }}>
              {activeFileTab === 'analysis_prompt' ? '分析阶段提示词 · AI 输出 JSON' : activeFileTab === 'synthesis_prompt' ? '综合报告提示词 · AI 输出 Markdown' : 'Bash 脚本 · 前置: exit 0=继续, 1=跳过, 2=失败'}
            </span>
            <button
              className="btn"
              onClick={() => handleFileSave(activeFileTab)}
              disabled={fileSaving || !fileDirty[activeFileTab]}
              style={{ padding: '0.4rem 1.2rem', fontSize: '0.85rem', opacity: fileDirty[activeFileTab] ? 1 : 0.5 }}
            >
              {fileSaving ? '保存中...' : '保存'}
            </button>
          </div>
        }
      >
        {/* Tabs */}
        <div style={{ display: 'flex', borderBottom: '1px solid var(--border-color)', flexShrink: 0, background: 'var(--bg-color)' }}>
          {(['analysis_prompt', 'synthesis_prompt', 'precondition'] as FileTab[]).map(tab => (
            <button
              key={tab}
              onClick={() => setActiveFileTab(tab)}
              style={{
                padding: '0.75rem 1.25rem', border: 'none', background: 'transparent', cursor: 'pointer',
                fontWeight: activeFileTab === tab ? 600 : 400, fontSize: '0.85rem',
                color: activeFileTab === tab ? 'var(--primary-color)' : '#64748b',
                borderBottom: activeFileTab === tab ? '2px solid var(--primary-color)' : '2px solid transparent',
                transition: 'all 0.15s'
              }}
            >
              {fileTabLabels[tab]}
              {fileDirty[tab] && <span style={{ marginLeft: '0.3rem', color: '#f59e0b', fontSize: '0.7rem' }}>●</span>}
            </button>
          ))}
        </div>

        {/* Editor */}
        <div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
          <textarea
            value={fileContents[activeFileTab]}
            onChange={e => updateFileContent(activeFileTab, e.target.value)}
            spellCheck={false}
            style={{
              flex: 1, width: '100%', height: '100%', padding: '1.25rem', border: 'none', outline: 'none', resize: 'none',
              fontFamily: "'JetBrains Mono', 'Fira Code', 'Consolas', monospace", fontSize: '0.85rem', lineHeight: '1.6',
              background: '#1e293b', color: '#e2e8f0', boxSizing: 'border-box'
            }}
            placeholder={activeFileTab === 'analysis_prompt' || activeFileTab === 'synthesis_prompt' ? '在此编写 AI 任务提示词（Markdown 格式）...' : '在此编写 Bash 脚本...'}
          />
        </div>
      </Drawer>

    </div>
  );
}

export default TaskTypeManagement;
