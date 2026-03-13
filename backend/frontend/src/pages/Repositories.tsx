import React, { useEffect, useState, useRef } from 'react';
import { useToast } from '../components/Toast';

function Repositories() {
  const { showToast } = useToast();
  const [repos, setRepos] = useState<any[]>([]);
  const [showModal, setShowModal] = useState(false);
  const [formData, setFormData] = useState({ name: '', url: '', owner_id: '', branch: 'main', team_id: 1, service_group: '' });
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
      .then(data => setTeams(data || []))
      .catch(console.error);
    fetch('/api/members')
      .then(res => res.json())
      .then(data => setMembers(data || []))
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
    setPage(1); // Reset to first page whenever filter changes
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

  const handleAddRepo = (e: React.FormEvent) => {
    e.preventDefault();
    fetch('/api/repos', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        name: formData.name,
        url: formData.url,
        owner_id: formData.owner_id,
        branch: formData.branch,
        team_id: Number(formData.team_id),
        service_group: formData.service_group
      })
    })
    .then(res => {
      if (res.ok) {
        setShowModal(false);
        setFormData({ name: '', url: '', owner_id: members.length > 0 ? members[0].id : '', branch: 'main', team_id: teams.length > 0 ? teams[0].id : 1, service_group: '' });
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
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.5rem' }}>
        <h2>代码仓清单</h2>
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
          <button className="btn" onClick={() => setShowModal(true)}>录入代码仓</button>
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
                <td style={{ fontWeight: 500, color: 'var(--primary-color)' }}>{repo.name}</td>
                <td>{repo.team?.name || '未知'}</td>
                <td>{repo.owner ? `${repo.owner.name} (${repo.owner.id})` : repo.owner_id}</td>
                <td><span className="badge" style={{ background: 'var(--border-color)', color: 'white' }}>{repo.branch}</span></td>
                <td>{repo.service_group}</td>
                <td style={{ display: 'flex', gap: '0.5rem' }}>
                  <button className="btn" onClick={() => triggerReview(repo.id)} style={{ padding: '0.4rem 0.8rem', fontSize: '0.875rem' }}>检视</button>
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

      {showModal && (
        <div style={{ position: 'fixed', top: 0, left: 0, right: 0, bottom: 0, backgroundColor: 'rgba(0,0,0,0.7)', display: 'flex', justifyContent: 'center', alignItems: 'center', zIndex: 1000 }}>
          <div className="card" style={{ width: '400px', maxWidth: '90%' }}>
            <h3 style={{ marginTop: 0, marginBottom: '1.5rem' }}>新增代码仓</h3>
            <form onSubmit={handleAddRepo} style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
              <div>
                <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem', color: 'var(--text-color)' }}>代码仓名称 / ID</label>
                <input required type="text" value={formData.name} onChange={e => setFormData({...formData, name: e.target.value})} style={{ width: '100%', padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--bg-color)', color: 'var(--text-color)', boxSizing: 'border-box' }} />
              </div>
              <div>
                <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem', color: 'var(--text-color)' }}>代码仓地址 (URL)</label>
                <input required type="url" value={formData.url} onChange={e => setFormData({...formData, url: e.target.value})} style={{ width: '100%', padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--bg-color)', color: 'var(--text-color)', boxSizing: 'border-box' }} />
              </div>
              <div>
                <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem', color: 'var(--text-color)' }}>项目责任人</label>
                <select required value={formData.owner_id} onChange={e => setFormData({...formData, owner_id: e.target.value})} style={{ width: '100%', padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--bg-color)', color: 'var(--text-color)', boxSizing: 'border-box' }}>
                  <option value="" disabled>选择挂靠责任人</option>
                  {members.map(m => (
                    <option key={m.id} value={m.id}>{m.name} ({m.id})</option>
                  ))}
                </select>
              </div>
              <div>
                <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem', color: 'var(--text-color)' }}>主干分支</label>
                <input required type="text" value={formData.branch} onChange={e => setFormData({...formData, branch: e.target.value})} style={{ width: '100%', padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--bg-color)', color: 'var(--text-color)', boxSizing: 'border-box' }} />
              </div>
              <div>
                <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem', color: 'var(--text-color)' }}>归属部门</label>
                <select required value={formData.team_id} onChange={e => setFormData({...formData, team_id: Number(e.target.value)})} style={{ width: '100%', padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--bg-color)', color: 'var(--text-color)', boxSizing: 'border-box' }}>
                  {teams.length === 0 && <option value="" disabled>无可用部门</option>}
                  {teams.map(t => (
                    <option key={t.id} value={t.id}>{t.name}</option>
                  ))}
                </select>
              </div>
              <div>
                <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem', color: 'var(--text-color)' }}>服务组 (最长30字符)</label>
                <input required type="text" maxLength={30} value={formData.service_group} onChange={e => setFormData({...formData, service_group: e.target.value})} style={{ width: '100%', padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--bg-color)', color: 'var(--text-color)', boxSizing: 'border-box' }} />
              </div>
              <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '1rem', marginTop: '1rem' }}>
                <button type="button" onClick={() => setShowModal(false)} style={{ background: 'transparent', border: 'none', color: '#64748b', cursor: 'pointer', padding: '0.5rem 1rem' }}>取消</button>
                <button type="submit" className="btn">确认录入</button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}

export default Repositories;
