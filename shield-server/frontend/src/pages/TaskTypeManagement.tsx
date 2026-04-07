import React, { useEffect, useState } from 'react';
import { createPortal } from 'react-dom';
import { useToast } from '../components/Toast';
import { PlayCircle, Code2, Settings, Trash2 } from 'lucide-react';

type FileTab = 'prompt' | 'precondition' | 'postprocess';

function TaskTypeManagement() {
  const { showToast } = useToast();
  const [taskTypes, setTaskTypes] = useState<any[]>([]);
  const [editingId, setEditingId] = useState<number | null>(null);
  const [form, setForm] = useState({
    name: '', display_name: '', description: '', prompt_file: '',
    precondition_script: '', postprocess_script: '', notify_template: '',
    notify_threshold: 0, notify_cc: [] as string[], timeout: 30, is_active: true
  });
  const [ccInput, setCcInput] = useState('');
  const [showForm, setShowForm] = useState(false);

  // File editor state
  const [showFileEditor, setShowFileEditor] = useState(false);
  const [fileEditorTaskId, setFileEditorTaskId] = useState<number | null>(null);
  const [fileEditorTaskName, setFileEditorTaskName] = useState('');
  const [activeFileTab, setActiveFileTab] = useState<FileTab>('prompt');
  const [fileContents, setFileContents] = useState({ prompt: '', precondition: '', postprocess: '' });
  const [fileDirty, setFileDirty] = useState({ prompt: false, precondition: false, postprocess: false });
  const [fileSaving, setFileSaving] = useState(false);

  const fetchTaskTypes = async () => {
    const res = await fetch('/api/task-types');
    if (res.ok) setTaskTypes(await res.json());
  };

  useEffect(() => { fetchTaskTypes(); }, []);

  const resetForm = () => {
    setForm({ name: '', display_name: '', description: '', prompt_file: '', precondition_script: '', postprocess_script: '', notify_template: '', notify_threshold: 0, notify_cc: [], timeout: 30, is_active: true });
    setEditingId(null);
    setCcInput('');
  };

  const handleEdit = (tt: any) => {
    let ccList: string[] = [];
    if (tt.notify_cc) {
      try { ccList = typeof tt.notify_cc === 'string' ? JSON.parse(tt.notify_cc) : tt.notify_cc; } catch { ccList = []; }
    }
    setForm({
      name: tt.name, display_name: tt.display_name, description: tt.description || '',
      prompt_file: tt.prompt_file || '', precondition_script: tt.precondition_script || '',
      postprocess_script: tt.postprocess_script || '', notify_template: tt.notify_template || '',
      notify_threshold: tt.notify_threshold || 0, notify_cc: ccList, timeout: tt.timeout || 30, is_active: tt.is_active
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
    const res = await fetch(url, {
      method, headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(form)
    });
    if (res.ok) {
      showToast(editingId ? '任务类型已更新' : '任务类型已创建', 'success');
      setShowForm(false);
      resetForm();
      fetchTaskTypes();
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
    } else {
      const d = await res.json();
      showToast(d.error || '删除失败', 'error');
    }
  };

  const handleToggleActive = async (tt: any) => {
    await fetch(`/api/task-types/${tt.id}`, {
      method: 'PATCH', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ is_active: !tt.is_active })
    });
    fetchTaskTypes();
  };

  const handleTriggerAll = async (tt: any) => {
    if (!window.confirm(`确认要立即对全体代码仓下发执行【${tt.display_name}】扫描任务吗？\n警告：如果你有很多仓库，这可能非常耗时并触发大量资源占用。`)) return;
    const res = await fetch(`/api/task-types/${tt.id}/trigger-all`, { method: 'POST' });
    if (res.ok) {
      const d = await res.json();
      showToast(d.message || '全仓扫描已下发', 'success');
    } else {
      const d = await res.json();
      showToast(d.error || '触发失败', 'error');
    }
  };

  // File editor functions
  const openFileEditor = async (tt: any) => {
    setFileEditorTaskId(tt.id);
    setFileEditorTaskName(tt.display_name);
    setActiveFileTab('prompt');
    setFileDirty({ prompt: false, precondition: false, postprocess: false });
    try {
      const res = await fetch(`/api/task-types/${tt.id}/files`);
      if (res.ok) {
        const data = await res.json();
        setFileContents({ prompt: data.prompt || '', precondition: data.precondition || '', postprocess: data.postprocess || '' });
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

  const fileTabLabels: Record<FileTab, string> = { prompt: 'Prompt 提示词', precondition: '前置检查脚本', postprocess: '后置分析脚本' };

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.5rem' }}>
        <p style={{ color: '#64748b', margin: 0, fontSize: '0.875rem' }}>管理系统支持的任务类型，配置 Prompt 文件和前置/后置脚本</p>
        <button className="btn" onClick={() => { resetForm(); setShowForm(true); }}>+ 新建任务类型</button>
      </div>

      <div style={{ padding: 0, overflow: 'hidden' }}>
        <table style={{ width: '100%', borderCollapse: 'collapse' }}>
          <thead>
            <tr style={{ borderBottom: '1px solid var(--border-color)', color: '#64748b', fontSize: '0.875rem', textAlign: 'left', background: 'var(--bg-color)' }}>
              <th style={{ padding: '1rem' }}>名称</th>
              <th style={{ padding: '1rem' }}>标识</th>
              <th style={{ padding: '1rem' }}>Prompt 文件</th>
              <th style={{ padding: '1rem' }}>超时(分钟)</th>
              <th style={{ padding: '1rem' }}>通知阈值</th>
              <th style={{ padding: '1rem' }}>状态</th>
              <th style={{ padding: '1rem', textAlign: 'right' }}>操作</th>
            </tr>
          </thead>
          <tbody>
            {taskTypes.length === 0 ? (
              <tr><td colSpan={7} style={{ padding: '3rem', textAlign: 'center', color: '#94a3b8' }}>暂无任务类型</td></tr>
            ) : taskTypes.map(tt => (
              <tr key={tt.id} style={{ borderBottom: '1px solid var(--border-color)' }}>
                <td style={{ padding: '1rem', fontWeight: 500 }}>
                  {tt.display_name}
                  {tt.is_builtin && <span style={{ marginLeft: '0.5rem', fontSize: '0.7rem', background: '#dbeafe', color: '#2563eb', padding: '0.1rem 0.4rem', borderRadius: '4px' }}>内置</span>}
                </td>
                <td style={{ padding: '1rem', fontFamily: 'monospace', fontSize: '0.8rem', color: '#64748b' }}>{tt.name}</td>
                <td style={{ padding: '1rem', fontSize: '0.8rem', color: '#64748b', maxWidth: '200px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={tt.prompt_file}>{tt.prompt_file || '-'}</td>
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
                    <span title="全仓扫描" onClick={() => handleTriggerAll(tt)} style={{ cursor: 'pointer', display: 'flex' }}>
                      <PlayCircle size={18} color="var(--primary-color)" />
                    </span>
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
      {showForm && createPortal(
        <div style={{ position: 'fixed', top: 0, left: 0, right: 0, bottom: 0, background: 'rgba(0,0,0,0.5)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 }}>
          <div className="card" style={{ width: '560px', maxHeight: '80vh', overflowY: 'auto', maxWidth: '95%' }}>
            <h3 style={{ margin: '0 0 1.5rem 0' }}>{editingId ? '编辑任务类型' : '新建任务类型'}</h3>
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
              {editingId ? (
                <>
                <div>
                  <label style={labelStyle}>Prompt 文件路径</label>
                  <input style={fieldStyle} value={form.prompt_file} onChange={e => setForm({...form, prompt_file: e.target.value})} placeholder="如: tasks/my-task/prompt.md" />
                </div>
                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
                  <div>
                    <label style={labelStyle}>前置检查脚本</label>
                    <input style={fieldStyle} value={form.precondition_script} onChange={e => setForm({...form, precondition_script: e.target.value})} placeholder="如: tasks/my-task/precondition.sh" />
                  </div>
                  <div>
                    <label style={labelStyle}>后置分析脚本</label>
                    <input style={fieldStyle} value={form.postprocess_script} onChange={e => setForm({...form, postprocess_script: e.target.value})} placeholder="如: tasks/my-task/postprocess.sh" />
                  </div>
                </div>
                </>
              ) : (
                <div style={{ background: '#f0fdf4', border: '1px solid #bbf7d0', borderRadius: '6px', padding: '0.6rem 1rem', fontSize: '0.8rem', color: '#15803d' }}>
                  💡 提示词和脚本文件将自动创建在 <code style={{ background: '#dcfce7', padding: '0.1rem 0.3rem', borderRadius: '3px' }}>tasks/{form.name || '<标识名>'}/</code> 目录下，创建后可通过「编辑脚本」修改内容。
                </div>
              )}
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
                <div>
                  <label style={labelStyle}>执行超时（分钟）</label>
                  <input type="number" style={fieldStyle} value={form.timeout} onChange={e => setForm({...form, timeout: parseInt(e.target.value) || 30})} />
                </div>
                <div>
                  <label style={labelStyle}>通知阈值（评分≥此值才通知）</label>
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
              <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '1rem', marginTop: '0.5rem' }}>
                <button type="button" onClick={() => { setShowForm(false); resetForm(); }} style={{ padding: '0.6rem 1.5rem', border: '1px solid var(--border-color)', background: 'transparent', borderRadius: '6px', cursor: 'pointer' }}>取消</button>
                <button type="submit" className="btn" style={{ padding: '0.6rem 1.5rem' }}>{editingId ? '保存修改' : '创建'}</button>
              </div>
            </form>
          </div>
        </div>, document.body
      )}

      {/* File Editor Modal */}
      {showFileEditor && createPortal(
        <div style={{ position: 'fixed', top: 0, left: 0, right: 0, bottom: 0, background: 'rgba(0,0,0,0.6)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 }}>
          <div className="card" style={{ width: '800px', maxWidth: '95vw', height: '75vh', display: 'flex', flexDirection: 'column', padding: 0, overflow: 'hidden' }}>
            {/* Header */}
            <div style={{ padding: '1rem 1.5rem', borderBottom: '1px solid var(--border-color)', display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexShrink: 0 }}>
              <h3 style={{ margin: 0, fontSize: '1rem', fontWeight: 600 }}>
                编辑脚本 — {fileEditorTaskName}
              </h3>
              <button onClick={() => setShowFileEditor(false)} style={{ background: 'transparent', border: 'none', cursor: 'pointer', padding: '0.25rem', borderRadius: '4px', color: '#64748b', fontSize: '1.2rem', lineHeight: 1 }}>✕</button>
            </div>

            {/* Tabs */}
            <div style={{ display: 'flex', borderBottom: '1px solid var(--border-color)', flexShrink: 0 }}>
              {(['prompt', 'precondition', 'postprocess'] as FileTab[]).map(tab => (
                <button
                  key={tab}
                  onClick={() => setActiveFileTab(tab)}
                  style={{
                    padding: '0.6rem 1.2rem', border: 'none', background: 'transparent', cursor: 'pointer',
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
            <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
              <textarea
                value={fileContents[activeFileTab]}
                onChange={e => updateFileContent(activeFileTab, e.target.value)}
                spellCheck={false}
                style={{
                  flex: 1, width: '100%', padding: '1rem 1.5rem', border: 'none', outline: 'none', resize: 'none',
                  fontFamily: "'JetBrains Mono', 'Fira Code', 'Consolas', monospace", fontSize: '0.85rem', lineHeight: '1.6',
                  background: '#1e293b', color: '#e2e8f0', boxSizing: 'border-box'
                }}
                placeholder={activeFileTab === 'prompt' ? '在此编写 AI 任务提示词（Markdown 格式）...' : '在此编写 Bash 脚本...'}
              />
            </div>

            {/* Footer */}
            <div style={{ padding: '0.75rem 1.5rem', borderTop: '1px solid var(--border-color)', display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexShrink: 0, background: 'var(--bg-color)' }}>
              <span style={{ fontSize: '0.78rem', color: '#94a3b8' }}>
                {activeFileTab === 'prompt' ? 'Markdown 格式' : 'Bash 脚本 · 前置: exit 0=继续, 1=跳过, 2=失败 · 后置: 输出 JSON {"score":N,"summary":"...","metrics":{}}'}
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
          </div>
        </div>, document.body
      )}
    </div>
  );
}

export default TaskTypeManagement;
