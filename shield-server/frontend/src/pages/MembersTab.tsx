import React, { useEffect, useState, useRef } from 'react';
import { useToast } from '../components/Toast';

function MembersTab() {
  const { showToast } = useToast();
  const [members, setMembers] = useState<any[]>([]);
  const [showModal, setShowModal] = useState(false);
  const [editingId, setEditingId] = useState<number | null>(null);
  const [formData, setFormData] = useState({ employee_id: '', name: '', email: '', department_id: '' as string | number, password: '' });
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [teams, setTeams] = useState<any[]>([]);

  const [page, setPage] = useState<number>(1);
  const [pageSize] = useState<number>(15);
  const [totalItems, setTotalItems] = useState<number>(0);
  const [totalPages, setTotalPages] = useState<number>(0);
  const [searchQuery, setSearchQuery] = useState<string>('');

  useEffect(() => {
    fetchMembers();
    fetch('/api/departments').then(res => res.json()).then(data => setTeams(Array.isArray(data) ? data : (data.items || []))).catch(console.error);
  }, [page, searchQuery]);

  const fetchMembers = () => {
    const params = new URLSearchParams({
      page: page.toString(),
      pageSize: pageSize.toString(),
    });
    if (searchQuery) params.append('search', searchQuery);

    fetch(`/api/users?${params.toString()}`)
      .then(res => res.json())
      .then(data => {
        setMembers(data.items || []);
        setTotalItems(data.total || 0);
        setTotalPages(data.totalPages || 0);
      })
      .catch(console.error);
  };

  const handleSearchChange = (value: string) => {
    setSearchQuery(value);
    setPage(1);
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!formData.department_id) {
      showToast('用户必须选择归属部门', 'error');
      return;
    }
    if (!editingId && !formData.password) {
      showToast('新建账号必须输入初始密码', 'error');
      return;
    }

    const url = editingId ? `/api/users/${editingId}` : '/api/users';
    const method = editingId ? 'PUT' : 'POST';

    const payload: any = {
      email: formData.email,
      name: formData.name,
      employee_id: formData.employee_id,
      department_id: Number(formData.department_id)
    };
    if (formData.password) {
      payload.password = formData.password;
    }

    fetch(url, {
      method,
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    })
    .then(res => {
      if (res.ok) {
        setShowModal(false);
        fetchMembers();
      } else {
        res.json().then(err => showToast(err.error || '保存失败', 'error'));
      }
    })
    .catch(console.error);
  };

  const handleDelete = (id: number, name: string) => {
    if (window.confirm(`确认删除人员 "${name}" 吗？如果Ta还是负责人则可能会报错。`)) {
      fetch(`/api/users/${id}`, { method: 'DELETE' })
        .then(res => {
          if (res.ok) fetchMembers();
          else showToast('删除失败，Ta可能是某些代码仓的责任人或有其他依赖。', 'error');
        })
        .catch(console.error);
    }
  };

  const openAdd = () => {
    setEditingId(null);
    setFormData({ employee_id: '', name: '', email: '', department_id: '', password: '' });
    setShowModal(true);
  };

  const openEdit = (m: any) => {
    setEditingId(m.id);
    setFormData({
      employee_id: m.employee_id || '',
      name: m.name || '',
      email: m.email || '',
      department_id: m.department_id || '',
      password: ''
    });
    setShowModal(true);
  };

  const handleFileUpload = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;

    const form = new FormData();
    form.append('file', file);

    fetch('/api/users/import', {
      method: 'POST',
      body: form
    })
    .then(res => res.json())
    .then(data => {
      if (data.error) showToast(`导入失败: ${data.error}`, 'error');
      else {
        showToast(data.message || '导入成功', 'success');
        fetchMembers();
      }
    })
    .catch(err => {
      console.error(err);
      showToast('网络或发生未知错误', 'error');
    })
    .finally(() => {
      if (fileInputRef.current) fileInputRef.current.value = '';
    });
  };

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'flex-end', alignItems: 'center', marginBottom: '1rem' }}>
        <div style={{ display: 'flex', gap: '1rem', alignItems: 'center' }}>
          <input 
            type="text" 
            placeholder="搜索姓名、工号或部门..." 
            value={searchQuery} 
            onChange={e => handleSearchChange(e.target.value)} 
            style={{ padding: '0.4rem 0.8rem', borderRadius: '4px', border: '1px solid var(--border-color)', outline: 'none' }} 
          />
          <input type="file" accept=".csv" ref={fileInputRef} style={{ display: 'none' }} onChange={handleFileUpload} />
          <button className="btn" onClick={openAdd}>新增人员</button>
          <button className="btn" style={{ background: 'var(--success-color)', borderColor: 'var(--success-color)', color: 'white' }} onClick={() => fileInputRef.current?.click()}>批量导入</button>
          <button className="btn" style={{ background: 'var(--success-color)', borderColor: 'var(--success-color)', color: 'white' }} onClick={() => {
            fetch('/api/users/export')
              .then(res => res.blob())
              .then(blob => {
                const url = URL.createObjectURL(blob);
                const a = document.createElement('a');
                a.href = url;
                a.download = 'users.csv';
                a.click();
                URL.revokeObjectURL(url);
              })
              .catch(() => showToast('导出失败', 'error'));
          }}>批量导出</button>
        </div>
      </div>

      <div className="card" style={{ padding: 0, overflow: 'hidden' }}>
        <table className="table">
          <thead>
            <tr>
              <th>工号</th>
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
                <td style={{ fontWeight: 500 }}>{m.employee_id || m.id}</td>
                <td>{m.name}</td>
                <td>{m.department?.name || <span style={{ color: '#aaa' }}>未配置</span>}</td>
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

      {totalPages > 0 && (
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginTop: '1rem', padding: '0.5rem', background: 'var(--card-bg)', borderRadius: '6px', border: '1px solid var(--border-color)' }}>
          <span style={{ fontSize: '0.875rem', color: 'var(--text-color)' }}>
            共 {totalItems} 条记录，当前第 {page} / {totalPages} 页
          </span>
          <div style={{ display: 'flex', gap: '0.5rem' }}>
            <button 
              className="btn" 
              disabled={page === 1} 
              onClick={() => setPage(page - 1)}
              style={{ 
                background: page === 1 ? 'var(--bg-color)' : 'var(--card-bg)', 
                color: page === 1 ? 'var(--text-secondary)' : 'var(--text-color)', 
                border: '1px solid var(--border-color)',
                opacity: page === 1 ? 0.5 : 1,
                cursor: page === 1 ? 'not-allowed' : 'pointer'
              }}>
              上一页
            </button>
            <button 
              className="btn" 
              disabled={page >= totalPages} 
              onClick={() => setPage(page + 1)}
              style={{ 
                background: page >= totalPages ? 'var(--bg-color)' : 'var(--card-bg)', 
                color: page >= totalPages ? 'var(--text-secondary)' : 'var(--text-color)', 
                border: '1px solid var(--border-color)',
                opacity: page >= totalPages ? 0.5 : 1,
                cursor: page >= totalPages ? 'not-allowed' : 'pointer'
              }}>
              下一页
            </button>
          </div>
        </div>
      )}

      {showModal && (
        <div style={{ position: 'fixed', top: 0, left: 0, right: 0, bottom: 0, backgroundColor: 'rgba(0,0,0,0.7)', display: 'flex', justifyContent: 'center', alignItems: 'center', zIndex: 1000 }}>
          <div className="card" style={{ width: '400px', maxWidth: '90%' }}>
            <h3 style={{ marginTop: 0, marginBottom: '1.5rem' }}>{editingId ? '编辑人员信息' : '新增人员'}</h3>
            <form onSubmit={handleSubmit} style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
              <div>
                <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem' }}>工号</label>
                <input required type="text" value={formData.employee_id} onChange={e => setFormData({...formData, employee_id: e.target.value})} style={{ width: '100%', padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--bg-color)', color: 'var(--text-color)', boxSizing: 'border-box' }} />
              </div>
              <div>
                <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem' }}>姓名</label>
                <input required type="text" value={formData.name} onChange={e => setFormData({...formData, name: e.target.value})} style={{ width: '100%', padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--bg-color)', color: 'var(--text-color)', boxSizing: 'border-box' }} />
              </div>
              <div>
                <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem' }}>所属部门</label>
                <select required value={formData.department_id} onChange={e => setFormData({...formData, department_id: e.target.value})} style={{ width: '100%', padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--bg-color)', color: 'var(--text-color)', boxSizing: 'border-box' }}>
                  <option value="">请选择部门</option>
                  {teams.map(t => (
                    <option key={t.id} value={t.id}>{t.name}</option>
                  ))}
                </select>
              </div>
              <div>
                <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem' }}>邮箱 (登录账号)</label>
                <input required type="email" value={formData.email} onChange={e => setFormData({...formData, email: e.target.value})} style={{ width: '100%', padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--bg-color)', color: 'var(--text-color)', boxSizing: 'border-box' }} />
              </div>
              <div>
                <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem' }}>
                  {editingId ? '修改密码 (留空表示不修改)' : '登录密码'}
                </label>
                <input required={!editingId} type="password" placeholder={editingId ? "不修改请留空" : "初始登录密码"} value={formData.password} onChange={e => setFormData({...formData, password: e.target.value})} style={{ width: '100%', padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--bg-color)', color: 'var(--text-color)', boxSizing: 'border-box' }} />
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
