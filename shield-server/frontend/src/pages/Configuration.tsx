import React, { useState, useEffect } from 'react';
import { createPortal } from 'react-dom';
import { useParams, useNavigate } from 'react-router-dom';
import ScheduleSidebar, { ScheduleFormData } from '../components/ScheduleSidebar';
import { useToast } from '../components/Toast';
import TaskTypeManagement from './TaskTypeManagement';

function Configuration() {
  const { showToast } = useToast();
  const { tab } = useParams<{ tab: string }>();
  const navigate = useNavigate();
  const activeTab = tab || 'users';
  const [teams, setTeams] = useState<any[]>([]);
  const [repos, setRepos] = useState<any[]>([]);
  const [users, setUsers] = useState<any[]>([]);
  const [schedules, setSchedules] = useState<any[]>([]);
  const [newTeamName, setNewTeamName] = useState('');
  const [newTeamLeader, setNewTeamLeader] = useState('');
  const [newUserForm, setNewUserForm] = useState({ username: '', name: '', password: '', is_admin: false });
  const [isSidebarOpen, setIsSidebarOpen] = useState(false);
  const [isUserModalOpen, setIsUserModalOpen] = useState(false);
  const [editingUser, setEditingUser] = useState<any>(null);
  const [editUserForm, setEditUserForm] = useState({ name: '', is_admin: false, password: '' });
  const [isEditUserModalOpen, setIsEditUserModalOpen] = useState(false);
  const [editingSchedule, setEditingSchedule] = useState<any>(null);

  const fetchTeams = async () => {
    try {
      const res = await fetch('/api/teams');
      if (res.ok) {
        setTeams(await res.json());
      }
    } catch (err) {
      console.error('Failed to fetch teams:', err);
    }
  };

  const fetchRepos = async () => {
    try {
      const res = await fetch('/api/repos');
      if (res.ok) {
        setRepos(await res.json());
      }
    } catch (err) {
      console.error('Failed to fetch repositories:', err);
    }
  };

  const fetchSchedules = async () => {
    try {
      const res = await fetch('/api/schedules');
      if (res.ok) {
        setSchedules(await res.json());
      }
    } catch (err) {
      console.error('Failed to fetch schedules:', err);
    }
  };

  const fetchUsers = async () => {
    try {
      const res = await fetch('/api/users', {
        headers: { 'Authorization': `Bearer ${localStorage.getItem('token')}` }
      });
      if (res.ok) {
        setUsers(await res.json());
      } else if (res.status === 403) {
        // Not an admin, silently ignore or handle accordingly
        setUsers([]);
      }
    } catch (err) {
      console.error('Failed to fetch users:', err);
    }
  };

  useEffect(() => {
    if (activeTab === 'tasks') {
      fetchSchedules();
      fetchRepos();
      fetchTeams();
    } else if (activeTab === 'users') {
      fetchUsers();
    }
  }, [activeTab]);

  const handleCreateTeam = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newTeamName || !newTeamLeader) return;
    try {
      const res = await fetch('/api/teams', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: newTeamName, leader_name: newTeamLeader })
      });
      if (res.ok) {
        setNewTeamName('');
        setNewTeamLeader('');
        fetchTeams();
      } else {
        const error = await res.json();
        showToast('新建部门失败: ' + error.error, 'error');
      }
    } catch (err) {
      console.error('Error creating team:', err);
    }
  };

  const handleCreateUser = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newUserForm.username || !newUserForm.password) return;
    try {
      const res = await fetch('/api/users', {
        method: 'POST',
        headers: { 
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${localStorage.getItem('token')}`
        },
        body: JSON.stringify(newUserForm)
      });
      if (res.ok) {
        setNewUserForm({ username: '', name: '', password: '', is_admin: false });
        setIsUserModalOpen(false);
        fetchUsers();
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
    setEditUserForm({ name: user.name || '', is_admin: user.is_admin, password: '' });
    setIsEditUserModalOpen(true);
  };

  const handleSaveEditUser = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!editingUser) return;
    try {
      const payload: any = { name: editUserForm.name, is_admin: editUserForm.is_admin };
      if (editUserForm.password) payload.password = editUserForm.password;
      const res = await fetch(`/api/users/${editingUser.id}`, {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${localStorage.getItem('token')}`
        },
        body: JSON.stringify(payload)
      });
      if (res.ok) {
        setIsEditUserModalOpen(false);
        setEditingUser(null);
        fetchUsers();
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
          'Authorization': `Bearer ${localStorage.getItem('token')}`
        },
        body: JSON.stringify({ is_active: isActive })
      });
      if (res.ok) fetchUsers();
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
        headers: { 'Authorization': `Bearer ${localStorage.getItem('token')}` }
      });
      if (res.ok) fetchUsers();
      else {
        const d = await res.json();
        showToast('删除失败: ' + d.error, 'error');
      }
    } catch (err) { console.error(err); }
  };


  const handleSaveSchedule = async (form: ScheduleFormData) => {
    try {
      const url = editingSchedule ? `/api/schedules/${editingSchedule.id}` : '/api/schedules';
      const method = editingSchedule ? 'PUT' : 'POST';
      const res = await fetch(url, {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(form)
      });
      if (res.ok) {
        setIsSidebarOpen(false);
        setEditingSchedule(null);
        fetchSchedules();
      } else {
        const d = await res.json();
        showToast((editingSchedule ? '更新' : '新建') + '调度失败: ' + d.error, 'error');
      }
    } catch (err) { console.error(err); }
  };

  const handleEditSchedule = (sched: any) => {
    setEditingSchedule(sched);
    setIsSidebarOpen(true);
  };

  const handleDeleteSchedule = async (id: number) => {
    if (!window.confirm('确认删除该定时策略吗？')) return;
    try {
      const res = await fetch(`/api/schedules/${id}`, { method: 'DELETE' });
      if (res.ok) fetchSchedules();
    } catch (err) { console.error(err); }
  };
  
  const toggleScheduleStatus = async (sched: any) => {
    try {
      const res = await fetch(`/api/schedules/${sched.id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ...sched, is_active: !sched.is_active })
      });
      if (res.ok) fetchSchedules();
    } catch (err) { console.error(err); }
  };

  const handleTriggerSchedule = async (id: number) => {
    try {
      showToast('正在触发任务，请耐心等待...', 'info');
      const res = await fetch(`/api/schedules/${id}/trigger`, { method: 'POST' });
      if (res.ok) {
        showToast('任务已成功触发', 'success');
      } else {
        const d = await res.json();
        showToast('触发失败: ' + d.error, 'error');
      }
    } catch (err) { console.error(err); }
  };


  return (
    <div className="card">
      <div style={{ display: 'flex', borderBottom: '1px solid var(--border-color)', marginBottom: '1.5rem', gap: '2rem' }}>
        <button 
          onClick={() => navigate('/config/users')}
          style={{ 
            background: 'transparent', border: 'none', padding: '1rem 0.5rem', cursor: 'pointer',
            fontSize: '0.875rem', fontWeight: 600,
            color: activeTab === 'users' ? 'var(--primary-color)' : '#64748b',
            borderBottom: activeTab === 'users' ? '2px solid var(--primary-color)' : '2px solid transparent',
            marginBottom: '-1px'
          }}
        >
          用户管理
        </button>

        <button 
          onClick={() => navigate('/config/task-types')}
          style={{ 
            background: 'transparent', border: 'none', padding: '1rem 0.5rem', cursor: 'pointer',
            fontSize: '0.875rem', fontWeight: 600,
            color: activeTab === 'task-types' ? 'var(--primary-color)' : '#64748b',
            borderBottom: activeTab === 'task-types' ? '2px solid var(--primary-color)' : '2px solid transparent',
            marginBottom: '-1px'
          }}
        >
          任务类型管理
        </button>

        <button 
          onClick={() => navigate('/config/tasks')}
          style={{ 
            background: 'transparent', border: 'none', padding: '1rem 0.5rem', cursor: 'pointer',
            fontSize: '0.875rem', fontWeight: 600,
            color: activeTab === 'tasks' ? 'var(--primary-color)' : '#64748b',
            borderBottom: activeTab === 'tasks' ? '2px solid var(--primary-color)' : '2px solid transparent',
            marginBottom: '-1px'
          }}
        >
          定时执行策略
        </button>
      </div>

      {activeTab === 'users' && (
        <div>
          <div style={{ display: 'flex', justifyContent: 'flex-end', alignItems: 'center', marginBottom: '1.5rem' }}>
            <button className="btn" onClick={() => setIsUserModalOpen(true)}>+ 分配新系统账号</button>
          </div>

          <table style={{ width: '100%', borderCollapse: 'collapse' }}>
            <thead>
              <tr style={{ borderBottom: '1px solid var(--border-color)', color: '#64748b', fontSize: '0.875rem', textAlign: 'left' }}>
                <th style={{ padding: '1rem 0' }}>系统 ID</th>
                <th style={{ padding: '1rem 0' }}>邮箱账号</th>
                <th style={{ padding: '1rem 0' }}>姓名</th>
                <th style={{ padding: '1rem 0' }}>角色标识</th>
                <th style={{ padding: '1rem 0' }}>账号状态</th>
                <th style={{ padding: '1rem 0' }}>最近登录时间</th>
                <th style={{ padding: '1rem 0', textAlign: 'right' }}>操作</th>
              </tr>
            </thead>
            <tbody>
              {users.length === 0 ? (
                <tr>
                  <td colSpan={6} style={{ padding: '2rem 0', textAlign: 'center', color: '#64748b' }}>无法获取人员列表或暂无数据（可能非管理员权限）。</td>
                </tr>
              ) : (
                users.map(u => (
                  <tr key={u.id} style={{ borderBottom: '1px solid var(--border-color)' }}>
                    <td style={{ padding: '1rem 0' }}>#{u.id}</td>
                    <td style={{ padding: '1rem 0', fontWeight: 500 }}>{u.username}</td>
                    <td style={{ padding: '1rem 0' }}>{u.name || '-'}</td>
                    <td style={{ padding: '1rem 0' }}>
                      {u.is_admin ? 
                        <span style={{ display: 'inline-flex', padding: '0.2rem 0.6rem', borderRadius: '4px', background: '#fef3c7', color: '#d97706', fontSize: '0.75rem', fontWeight: 600 }}>管理员</span> : 
                        <span style={{ display: 'inline-flex', padding: '0.2rem 0.6rem', borderRadius: '4px', background: 'var(--bg-color)', color: '#64748b', fontSize: '0.75rem', fontWeight: 600 }}>普通骨干</span>}
                    </td>
                    <td style={{ padding: '1rem 0' }}>
                      {u.is_active ? 
                        <span style={{ color: 'var(--success-color)', fontSize: '0.875rem', fontWeight: 500 }}>正常使用</span> : 
                        <span style={{ color: 'var(--danger-color)', fontSize: '0.875rem', fontWeight: 500 }}>已被禁用</span>}
                    </td>
                    <td style={{ padding: '1rem 0', fontSize: '0.875rem', color: '#64748b' }}>
                      {u.last_login ? new Date(u.last_login).toLocaleString() : <span style={{ color: '#94a3b8', fontStyle: 'italic' }}>从未登录</span>}
                    </td>
                    <td style={{ padding: '1rem 0', textAlign: 'right', display: 'flex', gap: '0.25rem', justifyContent: 'flex-end', alignItems: 'center' }}>
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
      )}

      {activeTab === 'task-types' && (
        <TaskTypeManagement />
      )}


      {activeTab === 'tasks' && (
        <div>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.5rem' }}>
            <h3 style={{ margin: 0 }}>定时任务策略配置</h3>
            <div style={{ display: 'flex', gap: '1rem', alignItems: 'center' }}>
              <button className="btn" onClick={() => { setEditingSchedule(null); setIsSidebarOpen(true); }}>
                + 新增定时策略
              </button>
            </div>
          </div>

          <table style={{ width: '100%', borderCollapse: 'collapse' }}>
            <thead>
              <tr style={{ borderBottom: '1px solid var(--border-color)', color: '#64748b', fontSize: '0.875rem', textAlign: 'left' }}>
                <th style={{ padding: '1rem 0' }}>策略名称</th>
                <th style={{ padding: '1rem 0' }}>任务类型</th>
                <th style={{ padding: '1rem 0' }}>Cron 表达式</th>
                <th style={{ padding: '1rem 0' }}>目标代码仓</th>
                <th style={{ padding: '1rem 0' }}>状态</th>
                <th style={{ padding: '1rem 0', textAlign: 'right' }}>操作</th>
              </tr>
            </thead>
            <tbody>
              {schedules.length === 0 ? (
                <tr>
                  <td colSpan={6} style={{ padding: '2rem 0', textAlign: 'center', color: '#64748b' }}>暂无可用的定时任务策略，点击右上方新增。</td>
                </tr>
              ) : (
                schedules.map(sched => (
                  <tr key={sched.id} style={{ borderBottom: '1px solid var(--border-color)' }}>
                    <td style={{ padding: '1rem 0', fontWeight: 500 }}>{sched.name}</td>
                    <td style={{ padding: '1rem 0' }}>
                      <span style={{ display: 'inline-block', padding: '0.15rem 0.5rem', borderRadius: '4px', background: 'rgba(37, 99, 235, 0.08)', color: 'var(--primary-color)', fontSize: '0.75rem', fontWeight: 500 }}>
                        {sched.task_type?.display_name || '-'}
                      </span>
                    </td>
                    <td style={{ padding: '1rem 0', fontFamily: 'monospace' }}>{sched.cron_expr}</td>
                    <td style={{ padding: '1rem 0' }}>
                      <span style={{ background: 'var(--bg-color)', padding: '0.25rem 0.6rem', borderRadius: '4px', fontSize: '0.75rem', border: '1px solid var(--border-color)', textTransform: 'capitalize' }}>
                        {sched.target_mode === 'all' ? '所有代码仓' : sched.target_mode === 'service_group' ? '按服务组' : sched.target_mode === 'team' ? '按团队' : '指定代码仓'}
                      </span>
                    </td>
                    <td style={{ padding: '1rem 0' }}>
                      <div 
                        style={{ display: 'inline-flex', alignItems: 'center', gap: '0.5rem', cursor: 'pointer' }}
                        onClick={() => toggleScheduleStatus(sched)}
                      >
                        <div style={{ width: 34, height: 20, borderRadius: 10, background: sched.is_active ? 'var(--primary-color)' : '#cbd5e1', position: 'relative', transition: '0.2s' }}>
                          <div style={{ width: 16, height: 16, borderRadius: 8, background: 'white', position: 'absolute', top: 2, left: sched.is_active ? 16 : 2, transition: '0.2s' }} />
                        </div>
                        <span style={{ fontSize: '0.875rem', color: sched.is_active ? 'var(--text-color)' : '#64748b' }}>
                          {sched.is_active ? '已启用' : '已停用'}
                        </span>
                      </div>
                    </td>
                    <td style={{ padding: '1rem 0', textAlign: 'right', display: 'flex', gap: '0.75rem', justifyContent: 'flex-end' }}>
                      <button 
                        onClick={() => handleTriggerSchedule(sched.id)}
                        style={{ padding: '0.4rem 0.8rem', fontSize: '0.75rem', background: 'transparent', border: '1px solid var(--border-color)', color: 'var(--success-color)', cursor: 'pointer', borderRadius: '4px', fontWeight: 500, transition: 'all 0.15s' }}
                        onMouseEnter={e => { e.currentTarget.style.background = 'rgba(16,185,129,0.06)'; }}
                        onMouseLeave={e => { e.currentTarget.style.background = 'transparent'; }}
                      >
                        触发
                      </button>
                      <button 
                        onClick={() => handleEditSchedule(sched)}
                        style={{ padding: '0.4rem 0.8rem', fontSize: '0.75rem', background: 'transparent', border: '1px solid var(--border-color)', color: 'var(--primary-color)', cursor: 'pointer', borderRadius: '4px', fontWeight: 500, transition: 'all 0.15s' }}
                        onMouseEnter={e => { e.currentTarget.style.background = 'rgba(37,99,235,0.06)'; }}
                        onMouseLeave={e => { e.currentTarget.style.background = 'transparent'; }}
                      >
                        编辑
                      </button>
                      <button 
                        onClick={() => handleDeleteSchedule(sched.id)}
                        style={{ padding: '0.4rem 0.8rem', fontSize: '0.75rem', background: 'transparent', border: 'none', color: '#dc2626', cursor: 'pointer' }}
                      >
                        删除
                      </button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      )}

      {isUserModalOpen && createPortal(
        <div style={{ position: 'fixed', top: 0, left: 0, right: 0, bottom: 0, backgroundColor: 'rgba(0,0,0,0.5)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 }}>
          <div style={{ background: 'white', padding: '2rem', borderRadius: '8px', width: '400px', boxShadow: '0 20px 25px -5px rgba(0,0,0,0.1), 0 10px 10px -5px rgba(0,0,0,0.04)' }}>
            <h3 style={{ margin: '0 0 1.5rem 0' }}>分配新系统账号</h3>
            <form onSubmit={handleCreateUser} style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
              <div>
                <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem', color: 'var(--text-color)', fontWeight: 500 }}>真实姓名</label>
                <input required value={newUserForm.name} onChange={e => setNewUserForm({...newUserForm, name: e.target.value})} placeholder="如: 张三" style={{ width: '100%', padding: '0.6rem', borderRadius: '4px', border: '1px solid var(--border-color)', outline: 'none' }} />
              </div>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
                <div>
                  <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem', color: 'var(--text-color)', fontWeight: 500 }}>登录邮箱账号</label>
                  <input required type="email" value={newUserForm.username} onChange={e => setNewUserForm({...newUserForm, username: e.target.value})} placeholder="如: zhangsan@company.com" style={{ width: '100%', padding: '0.6rem', borderRadius: '4px', border: '1px solid var(--border-color)', outline: 'none' }} />
                </div>
                <div>
                  <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem', color: 'var(--text-color)', fontWeight: 500 }}>初始密码</label>
                  <input required type="password" value={newUserForm.password} onChange={e => setNewUserForm({...newUserForm, password: e.target.value})} placeholder="不少于6位" style={{ width: '100%', padding: '0.6rem', borderRadius: '4px', border: '1px solid var(--border-color)', outline: 'none' }} />
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
          <div style={{ background: 'white', padding: '2rem', borderRadius: '8px', width: '400px', boxShadow: '0 20px 25px -5px rgba(0,0,0,0.1), 0 10px 10px -5px rgba(0,0,0,0.04)' }}>
            <h3 style={{ margin: '0 0 1.5rem 0' }}>编辑用户</h3>
            <form onSubmit={handleSaveEditUser} style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
              <div>
                <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem', color: '#64748b', fontWeight: 500 }}>登录邮箱</label>
                <div style={{ padding: '0.6rem', borderRadius: '4px', background: 'var(--bg-color)', border: '1px solid var(--border-color)', color: '#64748b', fontSize: '0.875rem' }}>{editingUser.username}</div>
              </div>
              <div>
                <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem', color: 'var(--text-color)', fontWeight: 500 }}>真实姓名</label>
                <input required value={editUserForm.name} onChange={e => setEditUserForm({...editUserForm, name: e.target.value})} placeholder="如: 张三" style={{ width: '100%', padding: '0.6rem', borderRadius: '4px', border: '1px solid var(--border-color)', outline: 'none' }} />
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

      {/* Schedule Sidebar */}
      <ScheduleSidebar
        isOpen={isSidebarOpen}
        onClose={() => { setIsSidebarOpen(false); setEditingSchedule(null); }}
        onSave={handleSaveSchedule}
        editingSchedule={editingSchedule}
        teams={teams}
        repos={repos}
      />
    </div>
  );
}

export default Configuration;
