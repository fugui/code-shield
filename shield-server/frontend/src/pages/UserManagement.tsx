import React, { useState, useEffect } from 'react';
import { createPortal } from 'react-dom';
import { useToast } from '../components/Toast';
import { AUTH_TOKEN_KEY } from '../config';

function UserManagement() {
  const { showToast } = useToast();
  const [users, setUsers] = useState<any[]>([]);
  const [newUserForm, setNewUserForm] = useState({ email: '', name: '', password: '', employee_id: '', unique_id: '', employee_type: '', is_admin: false, department_id: '' as string | number });
  const [isUserModalOpen, setIsUserModalOpen] = useState(false);
  const [editingUser, setEditingUser] = useState<any>(null);
  const [editUserForm, setEditUserForm] = useState({ name: '', employee_id: '', unique_id: '', employee_type: '', is_admin: false, password: '', department_id: '' as string | number });
  const [isEditUserModalOpen, setIsEditUserModalOpen] = useState(false);
  const [departments, setDepartments] = useState<any[]>([]);

  // Pagination states
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(25);
  const [totalItems, setTotalItems] = useState(0);
  const [totalPages, setTotalPages] = useState(0);

  // SSO and login type visibility config
  const [passwordLoginEnabled, setPasswordLoginEnabled] = useState(true);

  const fileInputRef = React.useRef<HTMLInputElement>(null);

  const handleFileUpload = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;

    const form = new FormData();
    form.append('file', file);

    fetch('/api/users/import', {
      method: 'POST',
      headers: { 'Authorization': `Bearer ${localStorage.getItem(AUTH_TOKEN_KEY)}` },
      body: form
    })
    .then(res => res.json())
    .then(data => {
      if (data.error) showToast(`导入失败: ${data.error}`, 'error');
      else {
        showToast(data.message || '导入成功', 'success');
        fetchUsers(page, pageSize);
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

  const fetchUsers = async (currentPage = page, currentPageSize = pageSize) => {
    try {
      const params = new URLSearchParams({
        page: currentPage.toString(),
        pageSize: currentPageSize.toString(),
      });
      const res = await fetch(`/api/users?${params.toString()}`, {
        headers: { 'Authorization': `Bearer ${localStorage.getItem(AUTH_TOKEN_KEY)}` }
      });
      if (res.ok) {
        const data = await res.json();
        setUsers(data.items || []);
        setTotalItems(data.total || 0);
        setTotalPages(data.totalPages || 0);
      } else if (res.status === 403) {
        setUsers([]);
        setTotalItems(0);
        setTotalPages(0);
      }
    } catch (err) {
      console.error('Failed to fetch users:', err);
    }
  };

  useEffect(() => {
    fetchUsers(page, pageSize);
  }, [page, pageSize]);

  useEffect(() => {
    fetch('/api/auth/config')
      .then(res => res.json())
      .then((data: any) => {
        if (data && typeof data.password_login_enabled === 'boolean') {
          setPasswordLoginEnabled(data.password_login_enabled);
        }
      })
      .catch(err => console.error('Failed to fetch auth config:', err));

    fetch('/api/departments')
      .then(res => res.json())
      .then(data => setDepartments(data || []))
      .catch(err => console.error('Failed to fetch departments:', err));
  }, []);

  const handleCreateUser = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newUserForm.email || !newUserForm.password) return;
    if (!newUserForm.department_id) {
      showToast('用户必须选择归属部门', 'error');
      return;
    }
    try {
      const res = await fetch('/api/users', {
        method: 'POST',
        headers: { 
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${localStorage.getItem(AUTH_TOKEN_KEY)}`
        },
        body: JSON.stringify({
          ...newUserForm,
          department_id: Number(newUserForm.department_id)
        })
      });
      if (res.ok) {
        setNewUserForm({ email: '', name: '', password: '', employee_id: '', unique_id: '', employee_type: '', is_admin: false, department_id: '' });
        setIsUserModalOpen(false);
        fetchUsers(1, pageSize);
        setPage(1);
      } else {
        const error = await res.json();
        showToast('新建用户失败: ' + error.error, 'error');
      }
    } catch (err) {
      console.error('Error creating user:', err);
    }
  };

  const handleEditUser = (user: any) => {
    setEditingUser(user);
    setEditUserForm({
      name: user.name || '',
      employee_id: user.employee_id || '',
      unique_id: user.unique_id || '',
      employee_type: user.employee_type || '',
      is_admin: user.is_admin,
      password: '',
      department_id: user.department_id || ''
    });
    setIsEditUserModalOpen(true);
  };

  const handleSaveEditUser = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!editingUser) return;
    if (!editUserForm.department_id) {
      showToast('用户必须选择归属部门', 'error');
      return;
    }
    try {
      const payload: any = {
        name: editUserForm.name,
        employee_id: editUserForm.employee_id,
        unique_id: editUserForm.unique_id,
        employee_type: editUserForm.employee_type,
        is_admin: editUserForm.is_admin,
        department_id: Number(editUserForm.department_id)
      };
      if (editUserForm.password) payload.password = editUserForm.password;
      const res = await fetch(`/api/users/${editingUser.id}`, {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${localStorage.getItem(AUTH_TOKEN_KEY)}`
        },
        body: JSON.stringify(payload)
      });
      if (res.ok) {
        setIsEditUserModalOpen(false);
        setEditingUser(null);
        fetchUsers(page, pageSize);
        showToast('用户信息已更新', 'success');
      } else {
        const d = await res.json();
        showToast('更新失败: ' + d.error, 'error');
      }
    } catch (err) { console.error(err); }
  };

  const handleUpdateUserStatus = async (id: number, isActive: boolean) => {
    if (!window.confirm(`确认要${isActive ? '启用' : '禁用'}该用户吗？`)) return;
    try {
      const res = await fetch(`/api/users/${id}/status`, {
        method: 'PATCH',
        headers: { 
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${localStorage.getItem(AUTH_TOKEN_KEY)}`
        },
        body: JSON.stringify({ is_active: isActive })
      });
      if (res.ok) fetchUsers(page, pageSize);
      else {
        const d = await res.json();
        showToast('更新失败: ' + d.error, 'error');
      }
    } catch (err) { console.error(err); }
  };

  const handleDeleteUser = async (id: number) => {
    if (!window.confirm('此操作不可逆，确认删除该用户吗？')) return;
    try {
      const res = await fetch(`/api/users/${id}`, {
        method: 'DELETE',
        headers: { 'Authorization': `Bearer ${localStorage.getItem(AUTH_TOKEN_KEY)}` }
      });
      if (res.ok) {
        const nextUsersLength = users.length - 1;
        if (nextUsersLength === 0 && page > 1) {
          setPage(prev => prev - 1);
        } else {
          fetchUsers(page, pageSize);
        }
      }
      else {
        const d = await res.json();
        showToast('删除失败: ' + d.error, 'error');
      }
    } catch (err) { console.error(err); }
  };

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.5rem' }}>
        <input type="file" accept=".csv" ref={fileInputRef} style={{ display: 'none' }} onChange={handleFileUpload} />
        <div />
        <div style={{ display: 'flex', gap: '0.75rem' }}>
          {passwordLoginEnabled && (
            <button className="btn" onClick={() => setIsUserModalOpen(true)}>+ 分配新系统账号</button>
          )}
          <button className="btn" style={{ background: 'var(--success-color)', borderColor: 'var(--success-color)', color: 'white' }} onClick={() => fileInputRef.current?.click()}>批量导入</button>
          <button className="btn" style={{ background: 'var(--success-color)', borderColor: 'var(--success-color)', color: 'white' }} onClick={() => {
            fetch('/api/users/export', {
              headers: { 'Authorization': `Bearer ${localStorage.getItem(AUTH_TOKEN_KEY)}` }
            })
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

      <div className="card" style={{ padding: 0 }}>
        <table style={{ width: '100%', borderCollapse: 'collapse' }}>
          <thead>
            <tr style={{ borderBottom: '1px solid var(--border-color)', color: '#64748b', fontSize: '0.875rem', textAlign: 'left' }}>
              <th style={{ padding: '1rem' }}>系统 ID</th>
              <th style={{ padding: '1rem' }}>登录邮箱</th>
              <th style={{ padding: '1rem' }}>姓名</th>
              <th style={{ padding: '1rem' }}>工号</th>
              <th style={{ padding: '1rem' }}>归属部门</th>
              <th style={{ padding: '1rem' }}>员工类型</th>
              <th style={{ padding: '1rem' }}>录入方式</th>
              <th style={{ padding: '1rem' }}>角色标识</th>
              <th style={{ padding: '1rem' }}>账号状态</th>
              <th style={{ padding: '1rem' }}>最近登录IP</th>
              <th style={{ padding: '1rem' }}>最近登录时间</th>
              <th style={{ padding: '1rem', textAlign: 'right' }}>操作</th>
            </tr>
          </thead>
          <tbody>
            {users.length === 0 ? (
              <tr>
                <td colSpan={12} style={{ padding: '2rem 0', textAlign: 'center', color: '#64748b' }}>无法获取人员列表或暂无数据（可能非管理员权限）。</td>
              </tr>
            ) : (
              users.map(u => (
                <tr key={u.id} style={{ borderBottom: '1px solid var(--border-color)' }}>
                  <td style={{ padding: '1rem' }}>#{u.id}</td>
                  <td style={{ padding: '1rem', fontWeight: 500 }}>{u.email || u.username}</td>
                  <td style={{ padding: '1rem' }}>{u.name || '-'}</td>
                  <td style={{ padding: '1rem' }}>{u.employee_id || <span style={{ color: '#94a3b8', fontStyle: 'italic' }}>-</span>}</td>
                  <td style={{ padding: '1rem' }}>{u.department?.name || <span style={{ color: '#94a3b8', fontStyle: 'italic' }}>-</span>}</td>
                  <td style={{ padding: '1rem' }}>{u.employee_type || <span style={{ color: '#94a3b8', fontStyle: 'italic' }}>-</span>}</td>
                  <td style={{ padding: '1rem' }}>
                    {u.reg_method === 'sso' ? 
                      <span style={{ display: 'inline-flex', padding: '0.2rem 0.6rem', borderRadius: '4px', background: 'rgba(59,130,246,0.1)', color: '#3b82f6', fontSize: '0.75rem', fontWeight: 600 }}>SSO 单点</span> : 
                      u.reg_method === 'imported' ?
                      <span style={{ display: 'inline-flex', padding: '0.2rem 0.6rem', borderRadius: '4px', background: 'rgba(107,114,128,0.1)', color: '#6b7280', fontSize: '0.75rem', fontWeight: 600 }}>被动导入</span> :
                      <span style={{ display: 'inline-flex', padding: '0.2rem 0.6rem', borderRadius: '4px', background: 'rgba(16,185,129,0.1)', color: '#10b981', fontSize: '0.75rem', fontWeight: 600 }}>本地录入</span>}
                  </td>
                  <td style={{ padding: '1rem' }}>
                    {u.is_admin ? 
                      <span style={{ display: 'inline-flex', padding: '0.2rem 0.6rem', borderRadius: '4px', background: '#fef3c7', color: '#d97706', fontSize: '0.75rem', fontWeight: 600 }}>管理员</span> : 
                      <span style={{ display: 'inline-flex', padding: '0.2rem 0.6rem', borderRadius: '4px', background: 'var(--bg-color)', color: '#64748b', fontSize: '0.75rem', fontWeight: 600 }}>普通骨干</span>}
                  </td>
                  <td style={{ padding: '1rem' }}>
                    {u.is_active ? 
                      <span style={{ color: 'var(--success-color)', fontSize: '0.875rem', fontWeight: 500 }}>正常使用</span> : 
                      <span style={{ color: 'var(--danger-color)', fontSize: '0.875rem', fontWeight: 500 }}>已被禁用</span>}
                  </td>
                  <td style={{ padding: '1rem', fontSize: '0.875rem', color: '#64748b' }}>
                    {u.last_ip || <span style={{ color: '#94a3b8', fontStyle: 'italic' }}>-</span>}
                  </td>
                  <td style={{ padding: '1rem', fontSize: '0.875rem', color: '#64748b' }}>
                    {u.last_login ? new Date(u.last_login).toLocaleString() : <span style={{ color: '#94a3b8', fontStyle: 'italic' }}>从未登录</span>}
                  </td>
                  <td style={{ padding: '1rem', textAlign: 'right', display: 'flex', gap: '0.25rem', justifyContent: 'flex-end', alignItems: 'center' }}>
                    <button
                      title="编辑用户"
                      onClick={() => handleEditUser(u)}
                      style={{ padding: '0.4rem', background: 'transparent', border: 'none', cursor: 'pointer', borderRadius: '4px', color: 'var(--primary-color)', display: 'inline-flex', alignItems: 'center', justifyContent: 'center', transition: 'all 0.15s' }}
                      onMouseEnter={e => { e.currentTarget.style.background = 'rgba(37,99,235,0.08)'; }}
                      onMouseLeave={e => { e.currentTarget.style.background = 'transparent'; }}
                    >
                      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>
                    </button>
                    <button
                      title={u.is_active ? '封禁用户' : '解封用户'}
                      onClick={() => handleUpdateUserStatus(u.id, !u.is_active)}
                      style={{ padding: '0.4rem', background: 'transparent', border: 'none', cursor: 'pointer', borderRadius: '4px', color: u.is_active ? '#f59e0b' : 'var(--success-color)', display: 'inline-flex', alignItems: 'center', justifyContent: 'center', transition: 'all 0.15s' }}
                      onMouseEnter={e => { e.currentTarget.style.background = u.is_active ? 'rgba(245,158,11,0.08)' : 'rgba(16,185,129,0.08)'; }}
                      onMouseLeave={e => { e.currentTarget.style.background = 'transparent'; }}
                    >
                      {u.is_active ? (
                        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="10"/><line x1="4.93" y1="4.93" x2="19.07" y2="19.07"/></svg>
                      ) : (
                        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/><polyline points="22 4 12 14.01 9 11.01"/></svg>
                      )}
                    </button>
                    <button
                      title="注销用户"
                      onClick={() => handleDeleteUser(u.id)}
                      style={{ padding: '0.4rem', background: 'transparent', border: 'none', cursor: 'pointer', borderRadius: '4px', color: '#dc2626', display: 'inline-flex', alignItems: 'center', justifyContent: 'center', transition: 'all 0.15s' }}
                      onMouseEnter={e => { e.currentTarget.style.background = 'rgba(220,38,38,0.08)'; }}
                      onMouseLeave={e => { e.currentTarget.style.background = 'transparent'; }}
                    >
                      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/><line x1="10" y1="11" x2="10" y2="17"/><line x1="14" y1="11" x2="14" y2="17"/></svg>
                    </button>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>

      {/* Pagination Controls */}
      {totalPages > 0 && (
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginTop: '1rem', padding: '0.5rem 1rem', background: 'var(--card-bg)', border: '1px solid var(--border-color)', borderRadius: '6px', fontSize: '0.875rem' }}>
          <div style={{ color: '#64748b' }}>
            共 {totalItems} 个系统用户
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <button
              disabled={page === 1}
              onClick={() => setPage(prev => Math.max(prev - 1, 1))}
              style={{
                padding: '0.3rem 0.6rem', border: '1px solid var(--border-color)', background: 'transparent',
                borderRadius: '4px', cursor: page === 1 ? 'not-allowed' : 'pointer',
                color: page === 1 ? 'var(--text-secondary)' : 'var(--text-color)', fontSize: '0.825rem',
                transition: 'all 0.2s'
              }}
              onMouseEnter={e => { if (page !== 1) e.currentTarget.style.background = 'rgba(0,0,0,0.02)'; }}
              onMouseLeave={e => { e.currentTarget.style.background = 'transparent'; }}
            >
              上一页
            </button>
            
            {/* Page numbers */}
            {Array.from({ length: Math.min(5, totalPages) }, (_, i) => {
              let pageNum = page;
              if (page <= 3) pageNum = i + 1;
              else if (page >= totalPages - 2) pageNum = totalPages - 4 + i;
              else pageNum = page - 2 + i;

              if (pageNum < 1 || pageNum > totalPages) return null;

              return (
                <button
                  key={pageNum}
                  onClick={() => setPage(pageNum)}
                  style={{
                    minWidth: '28px', height: '28px', padding: '0 0.3rem',
                    border: '1px solid',
                    borderColor: page === pageNum ? 'var(--primary-color)' : 'var(--border-color)',
                    background: page === pageNum ? 'var(--primary-color)' : 'transparent',
                    color: page === pageNum ? 'white' : 'var(--text-color)',
                    borderRadius: '4px', cursor: page === pageNum ? 'not-allowed' : 'pointer',
                    fontSize: '0.825rem', fontWeight: page === pageNum ? 600 : 400,
                    transition: 'all 0.2s'
                  }}
                  onMouseEnter={e => { if (page !== pageNum) e.currentTarget.style.background = 'rgba(37,99,235,0.04)'; }}
                  onMouseLeave={e => { if (page !== pageNum) e.currentTarget.style.background = 'transparent'; }}
                >
                  {pageNum}
                </button>
              );
            })}

            <button
              disabled={page === totalPages}
              onClick={() => setPage(prev => Math.min(prev + 1, totalPages))}
              style={{
                padding: '0.3rem 0.6rem', border: '1px solid var(--border-color)', background: 'transparent',
                borderRadius: '4px', cursor: page === totalPages ? 'not-allowed' : 'pointer',
                color: page === totalPages ? 'var(--text-secondary)' : 'var(--text-color)', fontSize: '0.825rem',
                transition: 'all 0.2s'
              }}
              onMouseEnter={e => { if (page !== totalPages) e.currentTarget.style.background = 'rgba(0,0,0,0.02)'; }}
              onMouseLeave={e => { e.currentTarget.style.background = 'transparent'; }}
            >
              下一页
            </button>

            <select
              value={pageSize}
              onChange={e => {
                setPageSize(Number(e.target.value));
                setPage(1);
              }}
              style={{
                padding: '0.25rem 0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)',
                fontSize: '0.825rem', outline: 'none', background: 'transparent', color: 'var(--text-color)', marginLeft: '0.5rem',
                cursor: 'pointer'
              }}
            >
              <option value="15">15 条/页</option>
              <option value="25">25 条/页</option>
              <option value="50">50 条/页</option>
              <option value="100">100 条/页</option>
            </select>
          </div>
        </div>
      )}

      {isUserModalOpen && createPortal(
        <div style={{ position: 'fixed', top: 0, left: 0, right: 0, bottom: 0, backgroundColor: 'rgba(0,0,0,0.5)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 }}>
          <div style={{ background: 'white', padding: '2rem', borderRadius: '8px', width: '480px', boxShadow: '0 20px 25px -5px rgba(0,0,0,0.1), 0 10px 10px -5px rgba(0,0,0,0.04)' }}>
            <h3 style={{ margin: '0 0 1.5rem 0' }}>分配新系统账号</h3>
            <form onSubmit={handleCreateUser} style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
                <div>
                  <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem', color: 'var(--text-color)', fontWeight: 500 }}>真实姓名</label>
                  <input required value={newUserForm.name} onChange={e => setNewUserForm({...newUserForm, name: e.target.value})} placeholder="如: 张三" style={{ width: '100%', padding: '0.6rem', borderRadius: '4px', border: '1px solid var(--border-color)', outline: 'none' }} />
                </div>
                <div>
                  <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem', color: 'var(--text-color)', fontWeight: 500 }}>员工工号</label>
                  <input value={newUserForm.employee_id} onChange={e => setNewUserForm({...newUserForm, employee_id: e.target.value})} placeholder="如: 00124" style={{ width: '100%', padding: '0.6rem', borderRadius: '4px', border: '1px solid var(--border-color)', outline: 'none' }} />
                </div>
              </div>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
                <div>
                  <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem', color: 'var(--text-color)', fontWeight: 500 }}>登录邮箱账号</label>
                  <input required type="email" value={newUserForm.email} onChange={e => setNewUserForm({...newUserForm, email: e.target.value})} placeholder="如: zhangsan@company.com" style={{ width: '100%', padding: '0.6rem', borderRadius: '4px', border: '1px solid var(--border-color)', outline: 'none' }} />
                </div>
                <div>
                  <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem', color: 'var(--text-color)', fontWeight: 500 }}>初始密码</label>
                  <input required type="password" value={newUserForm.password} onChange={e => setNewUserForm({...newUserForm, password: e.target.value})} placeholder="不少于6位" style={{ width: '100%', padding: '0.6rem', borderRadius: '4px', border: '1px solid var(--border-color)', outline: 'none' }} />
                </div>
              </div>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
                <div>
                  <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem', color: 'var(--text-color)', fontWeight: 500 }}>员工类型</label>
                  <input value={newUserForm.employee_type} onChange={e => setNewUserForm({...newUserForm, employee_type: e.target.value})} placeholder="如: 正式员工" style={{ width: '100%', padding: '0.6rem', borderRadius: '4px', border: '1px solid var(--border-color)', outline: 'none' }} />
                </div>
                <div>
                  <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem', color: 'var(--text-color)', fontWeight: 500 }}>所属部门</label>
                  <select required value={newUserForm.department_id} onChange={e => setNewUserForm({...newUserForm, department_id: e.target.value})} style={{ width: '100%', padding: '0.6rem', borderRadius: '4px', border: '1px solid var(--border-color)', outline: 'none', background: 'var(--bg-color)', color: 'var(--text-color)' }}>
                    <option value="">请选择部门</option>
                    {departments.map(d => (
                      <option key={d.id} value={d.id}>{d.name}</option>
                    ))}
                  </select>
                </div>
              </div>
              <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginTop: '0.5rem' }}>
                <input type="checkbox" id="isAdmin" checked={newUserForm.is_admin} onChange={e => setNewUserForm({...newUserForm, is_admin: e.target.checked})} />
                <label htmlFor="isAdmin" style={{ fontSize: '0.875rem', color: 'var(--text-color)' }}>设为管理员</label>
              </div>
              <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '1rem', marginTop: '1.5rem' }}>
                <button type="button" onClick={() => setIsUserModalOpen(false)} style={{ padding: '0.6rem 1.5rem', border: '1px solid var(--border-color)', background: 'transparent', borderRadius: '4px', cursor: 'pointer' }}>取消</button>
                <button type="submit" className="btn" style={{ padding: '0.6rem 1.5rem' }}>确认创建</button>
              </div>
            </form>
          </div>
        </div>, document.body
      )}

      {isEditUserModalOpen && editingUser && createPortal(
        <div style={{ position: 'fixed', top: 0, left: 0, right: 0, bottom: 0, backgroundColor: 'rgba(0,0,0,0.5)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 }}>
          <div style={{ background: 'white', padding: '2rem', borderRadius: '8px', width: '480px', boxShadow: '0 20px 25px -5px rgba(0,0,0,0.1), 0 10px 10px -5px rgba(0,0,0,0.04)' }}>
            <h3 style={{ margin: '0 0 1.5rem 0' }}>编辑用户</h3>
            <form onSubmit={handleSaveEditUser} style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
              <div>
                <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem', color: '#64748b', fontWeight: 500 }}>登录邮箱</label>
                <div style={{ padding: '0.6rem', borderRadius: '4px', background: 'var(--bg-color)', border: '1px solid var(--border-color)', color: '#64748b', fontSize: '0.875rem' }}>{editingUser.email || editingUser.username}</div>
              </div>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
                <div>
                  <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem', color: 'var(--text-color)', fontWeight: 500 }}>真实姓名</label>
                  <input required value={editUserForm.name} onChange={e => setEditUserForm({...editUserForm, name: e.target.value})} placeholder="如: 张三" style={{ width: '100%', padding: '0.6rem', borderRadius: '4px', border: '1px solid var(--border-color)', outline: 'none' }} />
                </div>
                <div>
                  <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem', color: 'var(--text-color)', fontWeight: 500 }}>员工工号</label>
                  <input value={editUserForm.employee_id} onChange={e => setEditUserForm({...editUserForm, employee_id: e.target.value})} placeholder="如: 00124" style={{ width: '100%', padding: '0.6rem', borderRadius: '4px', border: '1px solid var(--border-color)', outline: 'none' }} />
                </div>
              </div>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
                <div>
                  <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem', color: 'var(--text-color)', fontWeight: 500 }}>员工类型</label>
                  <input value={editUserForm.employee_type} onChange={e => setEditUserForm({...editUserForm, employee_type: e.target.value})} placeholder="如: 正式员工" style={{ width: '100%', padding: '0.6rem', borderRadius: '4px', border: '1px solid var(--border-color)', outline: 'none' }} />
                </div>
                <div>
                  <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem', color: 'var(--text-color)', fontWeight: 500 }}>所属部门</label>
                  <select required value={editUserForm.department_id} onChange={e => setEditUserForm({...editUserForm, department_id: e.target.value})} style={{ width: '100%', padding: '0.6rem', borderRadius: '4px', border: '1px solid var(--border-color)', outline: 'none', background: 'var(--bg-color)', color: 'var(--text-color)' }}>
                    <option value="">请选择部门</option>
                    {departments.map(d => (
                      <option key={d.id} value={d.id}>{d.name}</option>
                    ))}
                  </select>
                </div>
              </div>
              <div>
                <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem', color: 'var(--text-color)', fontWeight: 500 }}>重置密码 <span style={{ color: '#94a3b8', fontWeight: 400 }}>(留空表示不修改)</span></label>
                <input type="password" value={editUserForm.password} onChange={e => setEditUserForm({...editUserForm, password: e.target.value})} placeholder="输入新密码" style={{ width: '100%', padding: '0.6rem', borderRadius: '4px', border: '1px solid var(--border-color)', outline: 'none' }} />
              </div>
              <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginTop: '0.5rem' }}>
                <input type="checkbox" id="editIsAdmin" checked={editUserForm.is_admin} onChange={e => setEditUserForm({...editUserForm, is_admin: e.target.checked})} />
                <label htmlFor="editIsAdmin" style={{ fontSize: '0.875rem', color: 'var(--text-color)' }}>设为管理员</label>
              </div>
              <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '1rem', marginTop: '1.5rem' }}>
                <button type="button" onClick={() => { setIsEditUserModalOpen(false); setEditingUser(null); }} style={{ padding: '0.6rem 1.5rem', border: '1px solid var(--border-color)', background: 'transparent', borderRadius: '4px', cursor: 'pointer' }}>取消</button>
                <button type="submit" className="btn" style={{ padding: '0.6rem 1.5rem' }}>保存修改</button>
              </div>
            </form>
          </div>
        </div>, document.body
      )}
    </div>
  );
}

export default UserManagement;
