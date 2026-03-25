import React, { useEffect, useState } from 'react';
import { useToast } from '../components/Toast';

function TaskTypeManagement() {
  const { showToast } = useToast();
  const [taskTypes, setTaskTypes] = useState<any[]>([]);
  const [editingId, setEditingId] = useState<number | null>(null);
  const [form, setForm] = useState({
    name: '', display_name: '', description: '', prompt_file: '',
    precondition_script: '', postprocess_script: '', notify_template: '',
    notify_threshold: 0, timeout: 30, is_active: true
  });
  const [showForm, setShowForm] = useState(false);

  const fetchTaskTypes = async () => {
    const res = await fetch('/api/task-types');
    if (res.ok) setTaskTypes(await res.json());
  };

  useEffect(() => { fetchTaskTypes(); }, []);

  const resetForm = () => {
    setForm({ name: '', display_name: '', description: '', prompt_file: '', precondition_script: '', postprocess_script: '', notify_template: '', notify_threshold: 0, timeout: 30, is_active: true });
    setEditingId(null);
  };

  const handleEdit = (tt: any) => {
    setForm({
      name: tt.name, display_name: tt.display_name, description: tt.description || '',
      prompt_file: tt.prompt_file || '', precondition_script: tt.precondition_script || '',
      postprocess_script: tt.postprocess_script || '', notify_template: tt.notify_template || '',
      notify_threshold: tt.notify_threshold || 0, timeout: tt.timeout || 30, is_active: tt.is_active
    });
    setEditingId(tt.id);
    setShowForm(true);
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

  const fieldStyle: React.CSSProperties = { width: '100%', padding: '0.6rem', borderRadius: '6px', border: '1px solid var(--border-color)', outline: 'none', boxSizing: 'border-box', fontSize: '0.875rem' };
  const labelStyle: React.CSSProperties = { display: 'block', marginBottom: '0.4rem', fontSize: '0.8rem', color: '#64748b', fontWeight: 600 };

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.5rem' }}>
        <p style={{ color: '#64748b', margin: 0, fontSize: '0.875rem' }}>管理系统支持的任务类型，配置 Prompt 文件和前置/后置脚本</p>
        <button className="btn" onClick={() => { resetForm(); setShowForm(true); }}>+ 新建任务类型</button>
      </div>

      <div className="card" style={{ padding: 0, overflow: 'hidden' }}>
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
                  <div style={{ display: 'flex', gap: '0.5rem', justifyContent: 'flex-end' }}>
                    <button onClick={() => handleEdit(tt)} style={{ padding: '0.3rem 0.6rem', fontSize: '0.75rem', background: 'transparent', border: '1px solid var(--border-color)', color: 'var(--primary-color)', cursor: 'pointer', borderRadius: '4px' }}>编辑</button>
                    {!tt.is_builtin && (
                      <button onClick={() => handleDelete(tt.id)} style={{ padding: '0.3rem 0.6rem', fontSize: '0.75rem', background: 'transparent', border: 'none', color: '#dc2626', cursor: 'pointer' }}>删除</button>
                    )}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* Create/Edit Modal */}
      {showForm && (
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
              <div>
                <label style={labelStyle}>Prompt 文件路径</label>
                <input style={fieldStyle} value={form.prompt_file} onChange={e => setForm({...form, prompt_file: e.target.value})} placeholder="如: my-task/prompt.md" />
              </div>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
                <div>
                  <label style={labelStyle}>前置检查脚本</label>
                  <input style={fieldStyle} value={form.precondition_script} onChange={e => setForm({...form, precondition_script: e.target.value})} placeholder="如: my-task/precondition.sh" />
                </div>
                <div>
                  <label style={labelStyle}>后置分析脚本</label>
                  <input style={fieldStyle} value={form.postprocess_script} onChange={e => setForm({...form, postprocess_script: e.target.value})} placeholder="如: my-task/postprocess.sh" />
                </div>
              </div>
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
              <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '1rem', marginTop: '0.5rem' }}>
                <button type="button" onClick={() => { setShowForm(false); resetForm(); }} style={{ padding: '0.6rem 1.5rem', border: '1px solid var(--border-color)', background: 'transparent', borderRadius: '6px', cursor: 'pointer' }}>取消</button>
                <button type="submit" className="btn" style={{ padding: '0.6rem 1.5rem' }}>{editingId ? '保存修改' : '创建'}</button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}

export default TaskTypeManagement;
