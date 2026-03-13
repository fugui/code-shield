import React, { useState, useEffect } from 'react';

function Configuration() {
  const [activeTab, setActiveTab] = useState<'users' | 'teams' | 'tasks'>('users');
  const [teams, setTeams] = useState<any[]>([]);
  const [repos, setRepos] = useState<any[]>([]);
  const [users, setUsers] = useState<any[]>([]);
  const [autoNotify, setAutoNotify] = useState<boolean>(false);
  const [newTeamName, setNewTeamName] = useState('');
  const [newTeamLeader, setNewTeamLeader] = useState('');
  const [newUserForm, setNewUserForm] = useState({ username: '', password: '', is_admin: false });

  const fetchConfig = async () => {
    try {
      const res = await fetch('/api/config');
      if (res.ok) {
        const data = await res.json();
        setAutoNotify(data.auto_notify);
      }
    } catch (err) {
      console.error('Failed to fetch config:', err);
    }
  };

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
      fetchRepos();
      fetchConfig();
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

  const handleToggleAutoNotify = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const checked = e.target.checked;
    try {
      const res = await fetch('/api/config', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ auto_notify: checked })
      });
      if (res.ok) {
        setAutoNotify(checked);
      } else {
        alert('配置保存失败');
      }
    } catch (err) {
      console.error('Failed to update config:', err);
    }
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
          全量用户管理
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
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.5rem' }}>
            <h3 style={{ margin: 0 }}>核心人员名单</h3>
          </div>
          
          <div style={{ background: 'var(--bg-color)', padding: '1rem', borderRadius: '8px', marginBottom: '2rem', border: '1px solid var(--border-color)' }}>
            <h4 style={{ margin: '0 0 1rem 0', fontSize: '0.875rem' }}>分配新系统账号</h4>
            <form onSubmit={handleCreateUser} style={{ display: 'flex', gap: '1rem', alignItems: 'flex-end' }}>
              <div style={{ flex: 1 }}>
                <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.75rem', color: '#64748b' }}>登录用户名</label>
                <input required value={newUserForm.username} onChange={e => setNewUserForm({...newUserForm, username: e.target.value})} placeholder="如: zhangsan" style={{ width: '100%', padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'white' }} />
              </div>
              <div style={{ flex: 1 }}>
                <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.75rem', color: '#64748b' }}>初始密码</label>
                <input required type="password" value={newUserForm.password} onChange={e => setNewUserForm({...newUserForm, password: e.target.value})} placeholder="不少于6位" style={{ width: '100%', padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'white' }} />
              </div>
              <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', paddingBottom: '0.5rem' }}>
                <input type="checkbox" id="isAdmin" checked={newUserForm.is_admin} onChange={e => setNewUserForm({...newUserForm, is_admin: e.target.checked})} />
                <label htmlFor="isAdmin" style={{ fontSize: '0.875rem', color: 'var(--text-color)' }}>设为管理员</label>
              </div>
              <button type="submit" className="btn" style={{ padding: '0.6rem 1.5rem' }}>创建账号</button>
            </form>
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
            <h3 style={{ margin: 0 }}>定时检视任务配置</h3>
            <label style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', cursor: 'pointer', fontSize: '0.875rem', color: 'var(--text-color)', background: 'var(--bg-color)', padding: '0.5rem 1rem', borderRadius: '4px', border: '1px solid var(--border-color)' }}>
              <input type="checkbox" checked={autoNotify} onChange={handleToggleAutoNotify} />
              检视后自动通知责任人 (全局)
            </label>
          </div>

          <table style={{ width: '100%', borderCollapse: 'collapse' }}>
            <thead>
              <tr style={{ borderBottom: '1px solid var(--border-color)', color: '#64748b', fontSize: '0.875rem', textAlign: 'left' }}>
                <th style={{ padding: '1rem 0' }}>目标代码仓</th>
                <th style={{ padding: '1rem 0' }}>主干分支</th>
                <th style={{ padding: '1rem 0' }}>映射状态</th>
                <th style={{ padding: '1rem 0', textAlign: 'right' }}>人工干预操作</th>
              </tr>
            </thead>
            <tbody>
              {repos.length === 0 ? (
                <tr>
                  <td colSpan={4} style={{ padding: '2rem 0', textAlign: 'center', color: '#64748b' }}>暂无可供调度检视的代码仓。</td>
                </tr>
              ) : (
                repos.map(repo => (
                  <tr key={repo.id} style={{ borderBottom: '1px solid var(--border-color)' }}>
                    <td style={{ padding: '1rem 0', fontWeight: 500 }}>{repo.name}</td>
                    <td style={{ padding: '1rem 0' }}>
                      <span style={{ background: 'var(--bg-color)', padding: '0.25rem 0.5rem', borderRadius: '4px', fontSize: '0.75rem', border: '1px solid var(--border-color)' }}>
                        {repo.branch}
                      </span>
                    </td>
                    <td style={{ padding: '1rem 0' }}>
                      {repo.is_active ? 
                        <span style={{ color: 'var(--success-color)', fontSize: '0.875rem', fontWeight: 500 }}>调度活跃 (Active)</span> : 
                        <span style={{ color: '#64748b', fontSize: '0.875rem' }}>暂未绑定 (Inactive)</span>
                      }
                    </td>
                    <td style={{ padding: '1rem 0', textAlign: 'right' }}>
                      <button 
                        onClick={() => triggerReview(repo.id)}
                        disabled={!repo.is_active}
                        className="btn" 
                        style={{ padding: '0.4rem 0.8rem', fontSize: '0.75rem', display: 'inline-flex', alignItems: 'center', gap: '0.25rem', opacity: repo.is_active ? 1 : 0.5 }}
                      >
                        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><polygon points="5 3 19 12 5 21 5 3"></polygon></svg>
                        强制发起 AI 检视
                      </button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

export default Configuration;
