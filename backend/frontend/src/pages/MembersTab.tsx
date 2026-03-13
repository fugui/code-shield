import React, { useEffect, useState, useRef } from 'react';

function MembersTab() {
  const [members, setMembers] = useState<any[]>([]);
  const [showModal, setShowModal] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [formData, setFormData] = useState({ id: '', name: '', email: '', department: '' });
  const fileInputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    fetchMembers();
  }, []);

  const fetchMembers = () => {
    fetch('/api/members')
      .then(res => res.json())
      .then(data => setMembers(data || []))
      .catch(console.error);
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const url = editingId ? `/api/members/${editingId}` : '/api/members';
    const method = editingId ? 'PATCH' : 'POST';

    fetch(url, {
      method,
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(formData)
    })
    .then(res => {
      if (res.ok) {
        setShowModal(false);
        fetchMembers();
      } else {
        res.json().then(err => alert(err.error || '保存失败'));
      }
    })
    .catch(console.error);
  };

  const handleDelete = (id: string, name: string) => {
    if (window.confirm(`确认删除人员 "${name}" 吗？如果Ta还是负责人则可能会报错。`)) {
      fetch(`/api/members/${id}`, { method: 'DELETE' })
        .then(res => {
          if (res.ok) fetchMembers();
          else alert('删除失败，Ta可能是某些代码仓的责任人。');
        })
        .catch(console.error);
    }
  };

  const openAdd = () => {
    setEditingId(null);
    setFormData({ id: '', name: '', email: '', department: '' });
    setShowModal(true);
  };

  const openEdit = (m: any) => {
    setEditingId(m.id);
    setFormData({ id: m.id, name: m.name, email: m.email || '', department: m.department || '' });
    setShowModal(true);
  };

  const handleFileUpload = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;

    const form = new FormData();
    form.append('file', file);

    fetch('/api/members/import', {
      method: 'POST',
      body: form
    })
    .then(res => res.json())
    .then(data => {
      if (data.error) alert(`导入失败: ${data.error}`);
      else {
        alert(data.message || '导入成功');
        fetchMembers();
      }
    })
    .catch(err => {
      console.error(err);
      alert('网络或发生未知错误');
    })
    .finally(() => {
      if (fileInputRef.current) fileInputRef.current.value = '';
    });
  };

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1rem' }}>
        <h3 style={{ margin: 0 }}>组织名单集</h3>
        <div>
          <input type="file" accept=".csv" ref={fileInputRef} style={{ display: 'none' }} onChange={handleFileUpload} />
          <button className="btn" style={{ background: 'var(--border-color)', color: 'white', marginRight: '0.5rem' }} onClick={() => fileInputRef.current?.click()}>批量导入(CSV)</button>
          <button className="btn" onClick={openAdd}>新增人员</button>
        </div>
      </div>

      <div className="card" style={{ padding: 0, overflow: 'hidden' }}>
        <table className="table">
          <thead>
            <tr>
              <th>ID/工号</th>
              <th>姓名</th>
              <th>部门</th>
              <th>邮箱</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {members.length === 0 ? (
              <tr><td colSpan={5} style={{ textAlign: 'center', padding: '3rem', color: '#94a3b8' }}>暂未录入任何人员</td></tr>
            ) : members.map(m => (
              <tr key={m.id}>
                <td style={{ fontWeight: 500 }}>{m.id}</td>
                <td>{m.name}</td>
                <td>{m.department || <span style={{ color: '#aaa' }}>未配置</span>}</td>
                <td>{m.email || <span style={{ color: '#aaa' }}>未配置</span>}</td>
                <td style={{ display: 'flex', gap: '0.5rem' }}>
                  <button className="btn" onClick={() => openEdit(m)} style={{ padding: '0.4rem 0.8rem', fontSize: '0.875rem' }}>编辑</button>
                  <button className="btn" onClick={() => handleDelete(m.id, m.name)} style={{ padding: '0.4rem 0.8rem', fontSize: '0.875rem', background: 'transparent', color: 'var(--danger-color)', border: '1px solid var(--danger-color)' }}>删除</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {showModal && (
        <div style={{ position: 'fixed', top: 0, left: 0, right: 0, bottom: 0, backgroundColor: 'rgba(0,0,0,0.7)', display: 'flex', justifyContent: 'center', alignItems: 'center', zIndex: 1000 }}>
          <div className="card" style={{ width: '400px', maxWidth: '90%' }}>
            <h3 style={{ marginTop: 0, marginBottom: '1.5rem' }}>{editingId ? '编辑人员信息' : '新增人员'}</h3>
            <form onSubmit={handleSubmit} style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
              <div>
                <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem' }}>工号 / ID</label>
                <input required disabled={!!editingId} type="text" value={formData.id} onChange={e => setFormData({...formData, id: e.target.value})} style={{ width: '100%', padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: editingId ? '#334155' : 'var(--bg-color)', color: 'var(--text-color)', boxSizing: 'border-box' }} />
              </div>
              <div>
                <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem' }}>姓名</label>
                <input required type="text" value={formData.name} onChange={e => setFormData({...formData, name: e.target.value})} style={{ width: '100%', padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--bg-color)', color: 'var(--text-color)', boxSizing: 'border-box' }} />
              </div>
              <div>
                <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem' }}>所属部门</label>
                <input type="text" value={formData.department} onChange={e => setFormData({...formData, department: e.target.value})} style={{ width: '100%', padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--bg-color)', color: 'var(--text-color)', boxSizing: 'border-box' }} />
              </div>
              <div>
                <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem' }}>邮箱 (可选)</label>
                <input type="email" value={formData.email} onChange={e => setFormData({...formData, email: e.target.value})} style={{ width: '100%', padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--bg-color)', color: 'var(--text-color)', boxSizing: 'border-box' }} />
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

export default MembersTab;
