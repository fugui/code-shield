import React, { useState, useEffect } from 'react';
import ScheduleSidebar, { ScheduleFormData } from '../components/ScheduleSidebar';

function Configuration() {
  const [activeTab, setActiveTab] = useState<'users' | 'teams' | 'tasks'>('users');
  const [teams, setTeams] = useState<any[]>([]);
  const [repos, setRepos] = useState<any[]>([]);
  const [users, setUsers] = useState<any[]>([]);
  const [schedules, setSchedules] = useState<any[]>([]);
  const [newTeamName, setNewTeamName] = useState('');
  const [newTeamLeader, setNewTeamLeader] = useState('');
  const [newUserForm, setNewUserForm] = useState({ username: '', password: '', is_admin: false });
  const [isSidebarOpen, setIsSidebarOpen] = useState(false);
  const [isUserModalOpen, setIsUserModalOpen] = useState(false);
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
    if (activeTab === 'teams') {
      fetchTeams();
    } else if (activeTab === 'tasks') {
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
        alert('新建部门失败: ' + error.error);
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
        setNewUserForm({ username: '', password: '', is_admin: false });
        setIsUserModalOpen(false);
        fetchUsers();
      } else {
        const error = await res.json();
        alert('新建用户失败: ' + error.error);
      }
    } catch (err) {
      console.error('Error creating user:', err);
    }
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
        alert('更新失败: ' + d.error);
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
        alert('删除失败: ' + d.error);
      }
    } catch (err) { console.error(err); }
  };

  const triggerReview = async (repoId: number) => {
    try {
      const res = await fetch('/api/reviews/trigger', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ repo_id: repoId }),
      });
      
      if (res.ok) {
        alert('已成功向 AI 助手下发该代码检视任务！');
      } else {
        const data = await res.json();
        alert(`触发检视任务失败: ${data.error}`);
      }
    } catch (error) {
      console.error('Error triggering review:', error);
      alert('网络异常，触发检视失败');
    }
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
        alert((editingSchedule ? '更新' : '新建') + '调度失败: ' + d.error);
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


  return (
    <div className="card">
      <div style={{ display: 'flex', borderBottom: '1px solid var(--border-color)', marginBottom: '1.5rem', gap: '2rem' }}>
        <button 
          onClick={() => setActiveTab('users')}
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
          onClick={() => setActiveTab('tasks')}
          style={{ 
            background: 'transparent', border: 'none', padding: '1rem 0.5rem', cursor: 'pointer',
            fontSize: '0.875rem', fontWeight: 600,
            color: activeTab === 'tasks' ? 'var(--primary-color)' : '#64748b',
            borderBottom: activeTab === 'tasks' ? '2px solid var(--primary-color)' : '2px solid transparent',
            marginBottom: '-1px'
          }}
        >
          定时检视任务
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
                <th style={{ padding: '1rem 0' }}>用户名</th>
                <th style={{ padding: '1rem 0' }}>角色标识</th>
                <th style={{ padding: '1rem 0' }}>账号状态</th>
                <th style={{ padding: '1rem 0', textAlign: 'right' }}>特权操作</th>
              </tr>
            </thead>
            <tbody>
              {users.length === 0 ? (
                <tr>
                  <td colSpan={5} style={{ padding: '2rem 0', textAlign: 'center', color: '#64748b' }}>无法获取人员列表或暂无数据（可能非管理员权限）。</td>
                </tr>
              ) : (
                users.map(u => (
                  <tr key={u.id} style={{ borderBottom: '1px solid var(--border-color)' }}>
                    <td style={{ padding: '1rem 0' }}>#{u.id}</td>
                    <td style={{ padding: '1rem 0', fontWeight: 500 }}>{u.username}</td>
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
                    <td style={{ padding: '1rem 0', textAlign: 'right', display: 'flex', gap: '0.5rem', justifyContent: 'flex-end' }}>
                      <button 
                        onClick={() => handleUpdateUserStatus(u.id, !u.is_active)}
                        className="btn" 
                        style={{ padding: '0.4rem 0.8rem', fontSize: '0.75rem', background: 'transparent', border: '1px solid var(--border-color)', color: 'var(--text-color)' }}
                      >
                        {u.is_active ? '封禁' : '解封'}
                      </button>
                      <button 
                        onClick={() => handleDeleteUser(u.id)}
                        className="btn" 
                        style={{ padding: '0.4rem 0.8rem', fontSize: '0.75rem', background: '#fee2e2', color: '#dc2626' }}
                      >
                        注销用户
                      </button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      )}


      {activeTab === 'tasks' && (
        <div>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.5rem' }}>
            <h3 style={{ margin: 0 }}>自动巡检策略配置</h3>
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
                <th style={{ padding: '1rem 0' }}>Cron 表达式</th>
                <th style={{ padding: '1rem 0' }}>目标代码仓</th>
                <th style={{ padding: '1rem 0' }}>状态</th>
                <th style={{ padding: '1rem 0', textAlign: 'right' }}>操作</th>
              </tr>
            </thead>
            <tbody>
              {schedules.length === 0 ? (
                <tr>
                  <td colSpan={5} style={{ padding: '2rem 0', textAlign: 'center', color: '#64748b' }}>暂无可用的定时检视策略，点击右上方分配。</td>
                </tr>
              ) : (
                schedules.map(sched => (
                  <tr key={sched.id} style={{ borderBottom: '1px solid var(--border-color)' }}>
                    <td style={{ padding: '1rem 0', fontWeight: 500 }}>{sched.name}</td>
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

      {isUserModalOpen && (
        <div style={{ position: 'fixed', top: 0, left: 0, right: 0, bottom: 0, backgroundColor: 'rgba(0,0,0,0.5)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 }}>
          <div style={{ background: 'white', padding: '2rem', borderRadius: '8px', width: '400px', boxShadow: '0 20px 25px -5px rgba(0,0,0,0.1), 0 10px 10px -5px rgba(0,0,0,0.04)' }}>
            <h3 style={{ margin: '0 0 1.5rem 0' }}>分配新系统账号</h3>
            <form onSubmit={handleCreateUser} style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
              <div>
                <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem', color: 'var(--text-color)', fontWeight: 500 }}>登录用户名</label>
                <input required value={newUserForm.username} onChange={e => setNewUserForm({...newUserForm, username: e.target.value})} placeholder="如: zhangsan" style={{ width: '100%', padding: '0.6rem', borderRadius: '4px', border: '1px solid var(--border-color)', outline: 'none' }} />
              </div>
              <div>
                <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem', color: 'var(--text-color)', fontWeight: 500 }}>初始密码</label>
                <input required type="password" value={newUserForm.password} onChange={e => setNewUserForm({...newUserForm, password: e.target.value})} placeholder="不少于6位" style={{ width: '100%', padding: '0.6rem', borderRadius: '4px', border: '1px solid var(--border-color)', outline: 'none' }} />
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
        </div>
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
