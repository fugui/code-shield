import React, { useEffect, useState, useRef } from 'react';
import { useToast } from '../components/Toast';

function TeamsTab() {
  const [teams, setTeams] = useState<any[]>([]);
  const [members, setMembers] = useState<any[]>([]);
  const [showModal, setShowModal] = useState(false);
  const [editingId, setEditingId] = useState<number | null>(null);
  const [formData, setFormData] = useState({ name: '', leader_id: '' });
  const fileInputRef = useRef<HTMLInputElement>(null);
  const { showToast } = useToast();

  useEffect(() => {
    fetchTeams();
    fetch('/api/members').then(res => res.json()).then(data => setMembers(data || [])).catch(console.error);
  }, []);

  const fetchTeams = () => {
    fetch('/api/teams')
      .then(res => res.json())
      .then(data => setTeams(data || []))
      .catch(console.error);
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const url = editingId ? `/api/teams/${editingId}` : '/api/teams';
    const method = editingId ? 'PATCH' : 'POST';

    fetch(url, {
      method,
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(formData)
    })
    .then(res => {
      if (res.ok) {
        setShowModal(false);
        fetchTeams();
      } else {
        res.json().then(err => showToast(err.error || '保存失败', 'error'));
      }
    })
    .catch(console.error);
  };

  const handleDelete = (id: number, name: string) => {
    if (window.confirm(`确认删除部门 "${name}" 吗？如果关联了代码仓可能无法删除。`)) {
      fetch(`/api/teams/${id}`, { method: 'DELETE' })
        .then(res => {
          if (res.ok) fetchTeams();
          else showToast('删除失败，此部门下可能挂靠了代码仓。', 'error');
        })
        .catch(console.error);
    }
  };

  const openAdd = () => {
    setEditingId(null);
    setFormData({ name: '', leader_id: '' });
    setShowModal(true);
  };

  const openEdit = (t: any) => {
    setEditingId(t.id);
    setFormData({ name: t.name, leader_id: t.leader_id || '' });
    setShowModal(true);
  };

  const handleFileUpload = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;

    const formData = new FormData();
    formData.append('file', file);

    fetch('/api/teams/import', {
      method: 'POST',
      body: formData,
    })
      .then(res => res.json())
      .then(data => {
        if (data.error) {
          showToast(`导入失败: ${data.error}`, 'error');
        } else {
          showToast(data.message || '导入成功', 'success');
          fetchTeams();
        }
      })
      .catch(err => {
        console.error(err);
        showToast('导入请求出错', 'error');
      })
      .finally(() => {
        if (fileInputRef.current) fileInputRef.current.value = '';
      });
  };

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'flex-end', alignItems: 'center', marginBottom: '1rem' }}>
        <div style={{ display: 'flex', gap: '0.5rem' }}>
          <input 
            type="file" 
            accept=".csv" 
            ref={fileInputRef} 
            onChange={handleFileUpload} 
            style={{ display: 'none' }} 
          />
          <button 
            className="btn" 
            style={{ background: 'var(--success-color)', borderColor: 'var(--success-color)', color: 'white' }}
            onClick={() => fileInputRef.current?.click()}
          >
            批量导入
          </button>
          <button className="btn" onClick={openAdd}>新增部门</button>
        </div>
      </div>

      <div className="card" style={{ padding: 0, overflow: 'hidden' }}>
        <table className="table">
          <thead>
            <tr>
              <th>ID</th>
              <th>部门名称</th>
              <th>部门负责人</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {teams.length === 0 ? (
              <tr><td colSpan={4} style={{ textAlign: 'center', padding: '3rem', color: '#94a3b8' }}>暂未录入任何部门</td></tr>
            ) : teams.map(t => (
              <tr key={t.id}>
                <td style={{ fontWeight: 500 }}>{t.id}</td>
                <td>{t.name}</td>
                <td>{t.leader ? `${t.leader.name} (${t.leader.id})` : t.leader_id || <span style={{ color: '#aaa' }}>未配置</span>}</td>
                <td style={{ display: 'flex', gap: '0.5rem' }}>
                  <button className="btn" onClick={() => openEdit(t)} style={{ padding: '0.4rem 0.8rem', fontSize: '0.875rem' }}>编辑</button>
                  <button className="btn" onClick={() => handleDelete(t.id, t.name)} style={{ padding: '0.4rem 0.8rem', fontSize: '0.875rem', background: 'transparent', color: 'var(--danger-color)', border: '1px solid var(--danger-color)' }}>删除</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {showModal && (
        <div style={{ position: 'fixed', top: 0, left: 0, right: 0, bottom: 0, backgroundColor: 'rgba(0,0,0,0.7)', display: 'flex', justifyContent: 'center', alignItems: 'center', zIndex: 1000 }}>
          <div className="card" style={{ width: '400px', maxWidth: '90%' }}>
            <h3 style={{ marginTop: 0, marginBottom: '1.5rem' }}>{editingId ? '编辑部门' : '新增部门'}</h3>
            <form onSubmit={handleSubmit} style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
              <div>
                <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem' }}>部门名称</label>
                <input required type="text" value={formData.name} onChange={e => setFormData({...formData, name: e.target.value})} style={{ width: '100%', padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--bg-color)', color: 'var(--text-color)', boxSizing: 'border-box' }} />
              </div>
              <div>
                <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem' }}>部门负责人</label>
                <select value={formData.leader_id} onChange={e => setFormData({...formData, leader_id: e.target.value})} style={{ width: '100%', padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--bg-color)', color: 'var(--text-color)', boxSizing: 'border-box' }}>
                  <option value="">暂不指定</option>
                  {members.map(m => (
                    <option key={m.id} value={m.id}>{m.name} ({m.id})</option>
                  ))}
                </select>
              </div>
              <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '1rem', marginTop: '1rem' }}>
                <button type="button" onClick={() => setShowModal(false)} style={{ background: 'transparent', border: 'none', color: '#64748b', cursor: 'pointer', padding: '0.5rem 1rem' }}>取消</button>
                <button type="submit" className="btn">{editingId ? '保存' : '确认录入'}</button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}

export default TeamsTab;
