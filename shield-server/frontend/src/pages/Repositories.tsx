import React, { useEffect, useState, useRef } from 'react';
import { useToast } from '../components/Toast';
import { sshToHttps } from '../utils/urlUtils';
import MemberSearchSelect from '../components/MemberSearchSelect';
import MultiMemberSearchSelect from '../components/MultiMemberSearchSelect';

const inputStyle: React.CSSProperties = { width: '100%', padding: '0.625rem 0.75rem', borderRadius: '6px', border: '1px solid var(--border-color)', background: 'var(--bg-color)', color: 'var(--text-color)', boxSizing: 'border-box', fontSize: '0.875rem', transition: 'border-color 0.2s' };
const labelStyle: React.CSSProperties = { display: 'block', marginBottom: '0.375rem', fontSize: '0.8rem', color: '#64748b', fontWeight: 500 };

function Repositories() {
  const { showToast } = useToast();
  const [repos, setRepos] = useState<any[]>([]);
  const [drawerMode, setDrawerMode] = useState<'add' | 'edit' | null>(null);
  const [editingRepoId, setEditingRepoId] = useState<number | null>(null);
  const [formData, setFormData] = useState({ name: '', url: '', owner_id: '', branch: 'main', team_id: 0, service_group: '', related_members: [] as string[] });
  const [teams, setTeams] = useState<any[]>([]);
  const [members, setMembers] = useState<any[]>([]);
  const [filterTeam, setFilterTeam] = useState<string>('');
  const [filterServiceGroup, setFilterServiceGroup] = useState<string>('');
  const [filterOwner, setFilterOwner] = useState<string>('');
  const fileInputRef = useRef<HTMLInputElement>(null);
  
  // Pagination state
  const [page, setPage] = useState<number>(1);
  const [pageSize] = useState<number>(15);
  const [totalItems, setTotalItems] = useState<number>(0);
  const [totalPages, setTotalPages] = useState<number>(0);

  useEffect(() => {
    fetchRepos();
  }, [page, filterTeam, filterServiceGroup, filterOwner]);

  useEffect(() => {
    fetch('/api/teams')
      .then(res => res.json())
      .then(data => {
        const list = Array.isArray(data) ? data : (data.items || []);
        setTeams(list);
        if (list.length > 0) {
          setFormData(prev => ({ ...prev, team_id: prev.team_id === 0 ? list[0].id : prev.team_id }));
        }
      })
      .catch(console.error);
    fetch('/api/members?pageSize=1000')
      .then(res => res.json())
      .then(data => {
        const list = Array.isArray(data) ? data : (data.items || []);
        setMembers(list);
        if (list.length > 0) {
          setFormData(prev => ({ ...prev, owner_id: prev.owner_id === '' ? list[0].id : prev.owner_id }));
        }
      })
      .catch(console.error);
  }, []);

  const fetchRepos = () => {
    const params = new URLSearchParams({
      page: page.toString(),
      pageSize: pageSize.toString(),
    });
    if (filterTeam) params.append('team_id', filterTeam);
    if (filterServiceGroup) params.append('service_group', filterServiceGroup);
    if (filterOwner) params.append('owner', filterOwner);

    fetch(`/api/repos?${params.toString()}`)
      .then(res => res.json())
      .then(data => {
        setRepos(data.items || []);
        setTotalItems(data.total || 0);
        setTotalPages(data.totalPages || 0);
      })
      .catch(console.error);
  };

  const handleFilterChange = (setter: React.Dispatch<React.SetStateAction<string>>, value: string) => {
    setter(value);
    setPage(1);
  };

  const triggerReview = (repoId: number) => {
    fetch('/api/reviews/trigger', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ repo_id: repoId })
    }).then(() => {
        showToast('已成功触发检视任务！', 'success');
    }).catch(err => {
        console.error(err);
        showToast('触发检视任务失败，请检查网络。', 'error');
    });
  };

  const openAddDrawer = () => {
    setFormData({ name: '', url: '', owner_id: members.length > 0 ? members[0].id : '', branch: 'main', team_id: teams.length > 0 ? teams[0].id : 0, service_group: '', related_members: [] });
    setEditingRepoId(null);
    setDrawerMode('add');
  };

  const openEditDrawer = (repo: any) => {
    setFormData({
      name: repo.name || '',
      url: repo.url || '',
      owner_id: repo.owner_id || '',
      branch: repo.branch || 'main',
      team_id: repo.team_id || (teams.length > 0 ? teams[0].id : 0),
      service_group: repo.service_group || '',
      related_members: Array.isArray(repo.related_members) ? repo.related_members : []
    });
    setEditingRepoId(repo.id);
    setDrawerMode('edit');
  };

  const closeDrawer = () => {
    setDrawerMode(null);
    setEditingRepoId(null);
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (drawerMode === 'edit' && editingRepoId) {
      handleEditRepo();
    } else {
      handleAddRepo();
    }
  };

  const handleAddRepo = () => {
    fetch('/api/repos', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        name: formData.name,
        url: formData.url,
        owner_id: formData.owner_id,
        branch: formData.branch,
        team_id: Number(formData.team_id),
        service_group: formData.service_group,
        related_members: formData.related_members
      })
    })
    .then(res => {
      if (res.ok) {
        closeDrawer();
        fetchRepos();
        showToast('成功录入代码仓', 'success');
      } else {
        showToast('录入代码仓失败', 'error');
      }
    })
    .catch(err => {
      console.error(err);
      showToast('网络错误，录入失败', 'error');
    });
  };

  const handleEditRepo = () => {
    fetch(`/api/repos/${editingRepoId}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        name: formData.name,
        url: formData.url,
        owner_id: formData.owner_id,
        branch: formData.branch,
        team_id: Number(formData.team_id),
        service_group: formData.service_group,
        related_members: formData.related_members
      })
    })
    .then(res => {
      if (res.ok) {
        closeDrawer();
        fetchRepos();
        showToast('代码仓信息已更新', 'success');
      } else {
        showToast('更新代码仓失败', 'error');
      }
    })
    .catch(err => {
      console.error(err);
      showToast('网络错误，更新失败', 'error');
    });
  };

  const handleDeleteRepo = async (id: number, name: string) => {
    if (window.confirm(`确定要删除代码仓 "${name}" 吗？此操作不可恢复。`)) {
      try {
        const res = await fetch(`/api/repos/${id}`, { method: 'DELETE' });
        if (res.ok) {
          fetchRepos();
          showToast('成功删除代码仓', 'success');
        } else {
          showToast('删除代码仓失败', 'error');
        }
      } catch (err) {
        console.error('Failed to delete repo', err);
        showToast('网络错误，删除失败', 'error');
      }
    }
  };

  const handleFileUpload = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;

    const data = new FormData();
    data.append('file', file);

    fetch('/api/repos/import', {
      method: 'POST',
      body: data,
    })
    .then(async res => {
      if (res.ok) {
        const json = await res.json();
        showToast(json.message || '导入成功！', 'success');
        fetchRepos();
      } else {
        const json = await res.json();
        showToast(json.error || '导入失败，请检查CSV格式。', 'error');
      }
    })
    .catch(err => {
      console.error(err);
      showToast('网络错误，导入失败。', 'error');
    })
    .finally(() => {
      if (fileInputRef.current) {
        fileInputRef.current.value = '';
      }
    });
  };

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'flex-end', alignItems: 'center', marginBottom: '1.5rem' }}>
        <div style={{ display: 'flex', gap: '1rem' }}>
          <input 
            type="file" 
            accept=".csv" 
            ref={fileInputRef} 
            onChange={handleFileUpload} 
            style={{ display: 'none' }} 
          />
          <button 
            className="btn" 
            style={{ background: 'var(--success-color)', borderColor: 'var(--success-color)' }}
            onClick={() => fileInputRef.current?.click()}
          >
            批量导入(CSV)
          </button>
          <button className="btn" onClick={openAddDrawer}>录入代码仓</button>
        </div>
      </div>

      <div style={{ display: 'flex', gap: '1rem', marginBottom: '1rem' }}>
        <select value={filterTeam} onChange={e => handleFilterChange(setFilterTeam, e.target.value)} style={{ padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', outline: 'none' }}>
          <option value="">全部部门</option>
          {teams.map(t => <option key={t.id} value={t.id}>{t.name}</option>)}
        </select>
        <input type="text" placeholder="按服务组过滤..." value={filterServiceGroup} onChange={e => handleFilterChange(setFilterServiceGroup, e.target.value)} style={{ padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', outline: 'none' }} />
        <input type="text" placeholder="按责任人过滤..." value={filterOwner} onChange={e => handleFilterChange(setFilterOwner, e.target.value)} style={{ padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', outline: 'none' }} />
      </div>

      <div className="card" style={{ padding: 0, overflow: 'hidden' }}>
        <table className="table">
          <thead>
            <tr>
              <th>名称</th>
              <th>归属部门</th>
              <th>负责人</th>
              <th>分支</th>
              <th>服务组</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {repos.length === 0 ? (
              <tr><td colSpan={6} style={{ textAlign: 'center', padding: '3rem', color: '#94a3b8' }}>暂无匹配的记录或未录入任何代码仓</td></tr>
            ) : repos.map(repo => (
              <tr key={repo.id}>
                <td style={{ fontWeight: 500 }}>
                  {repo.url ? (
                    <a
                      href={sshToHttps(repo.url)}
                      target="_blank"
                      rel="noreferrer"
                      style={{ color: 'var(--primary-color)', textDecoration: 'none', display: 'inline-flex', alignItems: 'center', gap: '0.3rem' }}
                      onMouseEnter={e => e.currentTarget.style.textDecoration = 'underline'}
                      onMouseLeave={e => e.currentTarget.style.textDecoration = 'none'}
                      title={repo.url}
                    >
                      {repo.name}
                      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={{ opacity: 0.6, flexShrink: 0 }}>
                        <path d="M18 13v6a2 2 0 01-2 2H5a2 2 0 01-2-2V8a2 2 0 012-2h6"/>
                        <polyline points="15 3 21 3 21 9"/>
                        <line x1="10" y1="14" x2="21" y2="3"/>
                      </svg>
                    </a>
                  ) : (
                    <span style={{ color: 'var(--primary-color)' }}>{repo.name}</span>
                  )}
                </td>
                <td>{repo.team?.name || '未知'}</td>
                <td>
                  {repo.owner ? (
                    <span title={repo.owner.id}>{repo.owner.name}<span style={{ color: '#94a3b8', fontSize: '0.8rem', marginLeft: '0.3rem' }}>({repo.owner.id})</span></span>
                  ) : (
                    <span style={{ color: '#94a3b8' }}>{repo.owner_id || '-'}</span>
                  )}
                </td>
                <td><span className="badge" style={{ background: 'var(--border-color)', color: 'white' }}>{repo.branch}</span></td>
                <td>{repo.service_group}</td>
                <td style={{ display: 'flex', gap: '0.5rem' }}>
                  <button className="btn" onClick={() => openEditDrawer(repo)} style={{ padding: '0.4rem 0.8rem', fontSize: '0.875rem', background: 'transparent', color: 'var(--primary-color)', border: '1px solid var(--primary-color)' }}>编辑</button>
                  <button className="btn" onClick={() => handleDeleteRepo(repo.id, repo.name)} style={{ padding: '0.4rem 0.8rem', fontSize: '0.875rem', background: 'transparent', color: 'var(--danger-color)', border: '1px solid var(--danger-color)' }}>删除</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {totalPages > 0 && (
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginTop: '1rem', padding: '0.5rem', background: 'white', borderRadius: '6px', border: '1px solid var(--border-color)' }}>
          <span style={{ fontSize: '0.875rem', color: 'var(--text-color)' }}>
            共 {totalItems} 条记录，当前第 {page} / {totalPages} 页
          </span>
          <div style={{ display: 'flex', gap: '0.5rem' }}>
            <button 
              className="btn" 
              disabled={page === 1} 
              onClick={() => setPage(page - 1)}
              style={{ background: page === 1 ? '#f1f5f9' : 'white', color: page === 1 ? '#94a3b8' : 'var(--text-color)', border: '1px solid var(--border-color)' }}>
              上一页
            </button>
            <button 
              className="btn" 
              disabled={page >= totalPages} 
              onClick={() => setPage(page + 1)}
              style={{ background: page >= totalPages ? '#f1f5f9' : 'white', color: page >= totalPages ? '#94a3b8' : 'var(--text-color)', border: '1px solid var(--border-color)' }}>
              下一页
            </button>
          </div>
        </div>
      )}

      {/* Right-side Drawer */}
      {drawerMode && (
        <>
          {/* Backdrop */}
          <div
            onClick={closeDrawer}
            style={{
              position: 'fixed', top: 0, left: 0, right: 0, bottom: 0,
              backgroundColor: 'rgba(0,0,0,0.3)', zIndex: 999,
              animation: 'fadeIn 0.2s ease'
            }}
          />
          {/* Drawer panel */}
          <div style={{
            position: 'fixed', top: 0, right: 0, bottom: 0, width: '420px', maxWidth: '90vw',
            background: 'var(--card-bg)', boxShadow: '-4px 0 24px rgba(0,0,0,0.12)',
            zIndex: 1000, display: 'flex', flexDirection: 'column',
            animation: 'slideInRight 0.25s ease'
          }}>
            {/* Drawer header */}
            <div style={{
              padding: '1.25rem 1.5rem', borderBottom: '1px solid var(--border-color)',
              display: 'flex', justifyContent: 'space-between', alignItems: 'center'
            }}>
              <h3 style={{ margin: 0, fontSize: '1.1rem', fontWeight: 600 }}>
                {drawerMode === 'edit' ? '编辑代码仓' : '新增代码仓'}
              </h3>
              <button
                onClick={closeDrawer}
                style={{ background: 'transparent', border: 'none', cursor: 'pointer', padding: '0.25rem', borderRadius: '4px', color: '#64748b', display: 'flex', alignItems: 'center', justifyContent: 'center' }}
                onMouseEnter={e => e.currentTarget.style.backgroundColor = 'var(--bg-color)'}
                onMouseLeave={e => e.currentTarget.style.backgroundColor = 'transparent'}
              >
                <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
                </svg>
              </button>
            </div>

            {/* Drawer body */}
            <form onSubmit={handleSubmit} style={{ flex: 1, overflowY: 'auto', padding: '1.5rem', display: 'flex', flexDirection: 'column', gap: '1.25rem' }}>
              <div>
                <label style={labelStyle}>代码仓名称 / ID</label>
                <input required type="text" value={formData.name} onChange={e => setFormData({...formData, name: e.target.value})} style={inputStyle} />
              </div>
              <div>
                <label style={labelStyle}>代码仓地址 (URL)</label>
                <input required type="text" placeholder="https://... 或 git@host:path/repo.git" value={formData.url} onChange={e => setFormData({...formData, url: e.target.value})} style={inputStyle} />
              </div>
              <div>
                <label style={labelStyle}>项目责任人</label>
                <MemberSearchSelect value={formData.owner_id} onChange={id => setFormData({...formData, owner_id: id})} />
              </div>
              <div>
                <label style={labelStyle}>相关人员 (最多20人，分析结果将抄送给他们)</label>
                <MultiMemberSearchSelect value={formData.related_members} onChange={ids => setFormData({...formData, related_members: ids})} />
              </div>
              <div>
                <label style={labelStyle}>主干分支</label>
                <input required type="text" value={formData.branch} onChange={e => setFormData({...formData, branch: e.target.value})} style={inputStyle} />
              </div>
              <div>
                <label style={labelStyle}>归属部门</label>
                <select required value={formData.team_id} onChange={e => setFormData({...formData, team_id: Number(e.target.value)})} style={inputStyle}>
                  {teams.length === 0 && <option value="" disabled>无可用部门</option>}
                  {teams.map(t => (
                    <option key={t.id} value={t.id}>{t.name}</option>
                  ))}
                </select>
              </div>
              <div>
                <label style={labelStyle}>服务组 (最长30字符)</label>
                <input required type="text" maxLength={30} value={formData.service_group} onChange={e => setFormData({...formData, service_group: e.target.value})} style={inputStyle} />
              </div>

              {/* Spacer to push button to bottom */}
              <div style={{ flex: 1 }} />

              {/* Drawer footer */}
              <div style={{ display: 'flex', gap: '0.75rem', paddingTop: '1rem', borderTop: '1px solid var(--border-color)' }}>
                <button type="button" onClick={closeDrawer} style={{ flex: 1, padding: '0.625rem', border: '1px solid var(--border-color)', background: 'white', borderRadius: '6px', cursor: 'pointer', fontSize: '0.875rem', color: '#64748b' }}>取消</button>
                <button type="submit" className="btn" style={{ flex: 1, padding: '0.625rem', fontSize: '0.875rem' }}>
                  {drawerMode === 'edit' ? '保存修改' : '确认录入'}
                </button>
              </div>
            </form>
          </div>

          <style>{`
            @keyframes slideInRight {
              from { transform: translateX(100%); }
              to { transform: translateX(0); }
            }
            @keyframes fadeIn {
              from { opacity: 0; }
              to { opacity: 1; }
            }
          `}</style>
        </>
      )}
    </div>
  );
}

export default Repositories;
