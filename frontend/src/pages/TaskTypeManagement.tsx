import React, { useEffect, useState } from 'react';
import { Modal, Drawer, EmptyState } from '@code/common';
import { useToast } from '../components/Toast';
import { Code2, Settings, Trash2, Plus, RefreshCw, X, Shield, Layers } from 'lucide-react';

type FileTab = 'analysis_prompt' | 'synthesis_prompt' | 'precondition';

export interface DefenseDimension {
  key?: string;
  name?: string;
  dimension?: string;
  description: string;
}

export interface DomainFamilyInfo {
  key: string;
  name: string;
  description: string;
  recommended_mode: string;
  default_dimensions: DefenseDimension[];
}

export interface TaskTypeItem {
  id: number;
  name: string;
  display_name: string;
  description: string;
  engine_mode: string;
  engine_config?: Record<string, unknown> | string | null;
  target_scope: string;
  notify_template?: string;
  notify_threshold?: number;
  notify_cc?: string[] | string | null;
  timeout: number;
  is_active: boolean;
  is_builtin?: boolean;
  is_campaign?: boolean;
  campaign_path?: string;
  governance_mode?: string;
  campaign_icon?: string;
  campaign_config?: Record<string, unknown> | string | null;
  domain_family?: string;
  defense_dimensions?: DefenseDimension[] | string | null;
  categories?: string[] | string | null;
}

const DOMAIN_FAMILY_LABELS: Record<string, { label: string; color: string; bg: string }> = {
  memory_crash: { label: '内存底层崩溃', color: 'var(--color-danger, #ef4444)', bg: 'rgba(239, 68, 68, 0.1)' },
  architecture_governance: { label: '架构规范治理', color: 'var(--color-warning, #f59e0b)', bg: 'rgba(245, 158, 11, 0.1)' },
  numerical_determinism: { label: '数值计算确定性', color: 'var(--color-primary, #3b82f6)', bg: 'rgba(59, 130, 246, 0.1)' },
  test_engineering: { label: '测试工程质量', color: 'var(--color-success, #10b981)', bg: 'rgba(16, 185, 129, 0.1)' },
  security_injection: { label: '应用安全注入', color: '#8b5cf6', bg: 'rgba(139, 92, 246, 0.1)' },
  comprehensive_evolution: { label: '综合演进深度', color: '#06b6d4', bg: 'rgba(6, 182, 212, 0.1)' }
};

function TaskTypeManagement() {
  const { showToast } = useToast();
  const [taskTypes, setTaskTypes] = useState<TaskTypeItem[]>([]);
  const [domainFamilies, setDomainFamilies] = useState<DomainFamilyInfo[]>([]);
  const [editingId, setEditingId] = useState<number | null>(null);

  const [form, setForm] = useState({
    name: '',
    display_name: '',
    description: '',
    engine_mode: 'single',
    engine_config: '',
    target_scope: 'business',
    domain_family: 'comprehensive_evolution',
    defense_dimensions: [] as DefenseDimension[],
    categories: [] as string[],
    notify_template: '',
    notify_threshold: 0,
    notify_cc: [] as string[],
    timeout: 60,
    is_active: true,
    is_campaign: false,
    campaign_path: '',
    governance_mode: 'defect_tracking',
    campaign_icon: '',
    campaign_config: ''
  });

  const [ccInput, setCcInput] = useState('');
  const [newCatInput, setNewCatInput] = useState('');
  const [newDimKey, setNewDimKey] = useState('');
  const [newDimName, setNewDimName] = useState('');
  const [newDimDesc, setNewDimDesc] = useState('');

  const [showForm, setShowForm] = useState(false);

  // File editor state
  const [showFileEditor, setShowFileEditor] = useState(false);
  const [fileEditorTaskId, setFileEditorTaskId] = useState<number | null>(null);
  const [fileEditorTaskName, setFileEditorTaskName] = useState('');
  const [activeFileTab, setActiveFileTab] = useState<FileTab>('analysis_prompt');
  const [fileContents, setFileContents] = useState({ analysis_prompt: '', synthesis_prompt: '', precondition: '' });
  const [fileDirty, setFileDirty] = useState({ analysis_prompt: false, synthesis_prompt: false, precondition: false });
  const [fileSaving, setFileSaving] = useState(false);

  const fetchTaskTypes = React.useCallback(async () => {
    try {
      const res = await fetch('/api/task-types');
      if (res.ok) {
        const data: TaskTypeItem[] = await res.json();
        setTaskTypes(data);
      }
    } catch {
      showToast('获取任务类型列表失败', 'error');
    }
  }, [showToast]);

  const fetchDomainFamilies = React.useCallback(async () => {
    try {
      const res = await fetch('/api/task-types/domain-families');
      if (res.ok) {
        const data: DomainFamilyInfo[] = await res.json();
        setDomainFamilies(data);
      }
    } catch {
      // 容错忽略
    }
  }, []);

  useEffect(() => {
    fetchTaskTypes();
    fetchDomainFamilies();
  }, [fetchTaskTypes, fetchDomainFamilies]);

  const resetForm = () => {
    setForm({
      name: '',
      display_name: '',
      description: '',
      engine_mode: 'single',
      engine_config: '',
      target_scope: 'business',
      domain_family: 'comprehensive_evolution',
      defense_dimensions: [],
      categories: [],
      notify_template: '',
      notify_threshold: 0,
      notify_cc: [],
      timeout: 60,
      is_active: true,
      is_campaign: false,
      campaign_path: '',
      governance_mode: 'defect_tracking',
      campaign_icon: '',
      campaign_config: ''
    });
    setEditingId(null);
    setCcInput('');
    setNewCatInput('');
    setNewDimKey('');
    setNewDimName('');
    setNewDimDesc('');
  };

  const handleEdit = (tt: TaskTypeItem) => {
    let ccList: string[] = [];
    if (tt.notify_cc) {
      try {
        ccList = typeof tt.notify_cc === 'string' ? JSON.parse(tt.notify_cc) : (tt.notify_cc as string[]);
      } catch {
        ccList = [];
      }
    }

    let dims: DefenseDimension[] = [];
    if (tt.defense_dimensions) {
      try {
        dims = typeof tt.defense_dimensions === 'string' ? JSON.parse(tt.defense_dimensions) : (tt.defense_dimensions as DefenseDimension[]);
      } catch {
        dims = [];
      }
    }

    let cats: string[] = [];
    if (tt.categories) {
      try {
        cats = typeof tt.categories === 'string' ? JSON.parse(tt.categories) : (tt.categories as string[]);
      } catch {
        cats = [];
      }
    }

    let configStr = '';
    if (tt.engine_config) {
      configStr = typeof tt.engine_config === 'string' ? tt.engine_config : JSON.stringify(tt.engine_config, null, 2);
    }

    let campConfigStr = '';
    if (tt.campaign_config) {
      campConfigStr = typeof tt.campaign_config === 'string' ? tt.campaign_config : JSON.stringify(tt.campaign_config, null, 2);
    }

    setForm({
      name: tt.name,
      display_name: tt.display_name,
      description: tt.description || '',
      engine_mode: tt.engine_mode || 'single',
      engine_config: configStr,
      target_scope: tt.target_scope || 'business',
      domain_family: tt.domain_family || 'comprehensive_evolution',
      defense_dimensions: Array.isArray(dims) ? dims : [],
      categories: Array.isArray(cats) ? cats : [],
      notify_template: tt.notify_template || '',
      notify_threshold: tt.notify_threshold || 0,
      notify_cc: Array.isArray(ccList) ? ccList : [],
      timeout: tt.timeout || 60,
      is_active: tt.is_active,
      is_campaign: !!tt.is_campaign,
      campaign_path: tt.campaign_path || '',
      governance_mode: tt.governance_mode || 'defect_tracking',
      campaign_icon: tt.campaign_icon || '',
      campaign_config: campConfigStr
    });
    setEditingId(tt.id);
    setCcInput('');
    setNewCatInput('');
    setNewDimKey('');
    setNewDimName('');
    setNewDimDesc('');
    setShowForm(true);
  };

  const handleAddCc = () => {
    const email = ccInput.trim();
    if (!email) return;
    if (!/\S+@\S+\.\S+/.test(email)) {
      showToast('请输入有效的邮箱地址', 'error');
      return;
    }
    if (form.notify_cc.includes(email)) {
      showToast('该邮箱已添加', 'error');
      return;
    }
    setForm({ ...form, notify_cc: [...form.notify_cc, email] });
    setCcInput('');
  };

  const handleRemoveCc = (email: string) => {
    setForm({ ...form, notify_cc: form.notify_cc.filter(e => e !== email) });
  };

  const handleAddCategory = () => {
    const cat = newCatInput.trim();
    if (!cat) return;
    if (form.categories.includes(cat)) {
      showToast('分类已存在', 'error');
      return;
    }
    setForm({ ...form, categories: [...form.categories, cat] });
    setNewCatInput('');
  };

  const handleRemoveCategory = (cat: string) => {
    setForm({ ...form, categories: form.categories.filter(c => c !== cat) });
  };

  const handleAddDefenseDimension = () => {
    const key = newDimKey.trim();
    const desc = newDimDesc.trim();
    const name = newDimName.trim();
    if (!key && !name) {
      showToast('请填写维度英文标识或中文名称', 'error');
      return;
    }
    if (!desc) {
      showToast('请填写维度描述说明', 'error');
      return;
    }
    const exists = form.defense_dimensions.some(d => (d.key && d.key === key) || (d.name && d.name === name));
    if (exists) {
      showToast('该维度已存在', 'error');
      return;
    }

    setForm({
      ...form,
      defense_dimensions: [
        ...form.defense_dimensions,
        { key: key || name, name: name || key, description: desc }
      ]
    });
    setNewDimKey('');
    setNewDimName('');
    setNewDimDesc('');
  };

  const handleRemoveDefenseDimension = (index: number) => {
    setForm({
      ...form,
      defense_dimensions: form.defense_dimensions.filter((_, i) => i !== index)
    });
  };

  const handleLoadFamilyDefaults = () => {
    const currentFamily = domainFamilies.find(f => f.key === form.domain_family);
    if (!currentFamily || !currentFamily.default_dimensions) {
      showToast('当前所选族群暂无默认模板', 'info');
      return;
    }
    setForm(prev => ({
      ...prev,
      defense_dimensions: currentFamily.default_dimensions.map(d => ({ ...d }))
    }));
    showToast(`已载入【${currentFamily.name}】的默认抗辩维度`, 'success');
  };

  const handleApplyFamilyRecommendedMode = () => {
    const currentFamily = domainFamilies.find(f => f.key === form.domain_family);
    if (!currentFamily || !currentFamily.recommended_mode) return;
    setForm(prev => ({
      ...prev,
      engine_mode: currentFamily.recommended_mode
    }));
    showToast(`已切换至族群推荐模式: ${currentFamily.recommended_mode}`, 'info');
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const url = editingId ? `/api/task-types/${editingId}` : '/api/task-types';
    const method = editingId ? 'PATCH' : 'POST';

    const payload: Record<string, unknown> = {
      ...form,
      defense_dimensions: form.defense_dimensions.length > 0 ? form.defense_dimensions : null,
      categories: form.categories.length > 0 ? form.categories : null
    };

    if (form.engine_config) {
      try {
        payload.engine_config = JSON.parse(form.engine_config);
      } catch {
        showToast('引擎配置必须是有效的 JSON', 'error');
        return;
      }
    } else {
      payload.engine_config = null;
    }

    if (form.campaign_config) {
      try {
        payload.campaign_config = JSON.parse(form.campaign_config);
      } catch {
        showToast('专项高级配置必须是有效的 JSON', 'error');
        return;
      }
    } else {
      payload.campaign_config = null;
    }

    const res = await fetch(url, {
      method,
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });

    if (res.ok) {
      showToast(editingId ? '任务类型已更新' : '任务类型已创建', 'success');
      setShowForm(false);
      resetForm();
      fetchTaskTypes();
      window.dispatchEvent(new CustomEvent('shield-task-types-changed'));
    } else {
      const d = await res.json();
      showToast(d.error || '操作失败', 'error');
    }
  };

  const handleDelete = async (id: number) => {
    if (!window.confirm('确认删除此任务类型？关联的报告与配置将被一并清理。')) return;
    const res = await fetch(`/api/task-types/${id}`, { method: 'DELETE' });
    if (res.ok) {
      showToast('已删除任务类型', 'success');
      fetchTaskTypes();
      window.dispatchEvent(new CustomEvent('shield-task-types-changed'));
    } else {
      const d = await res.json();
      showToast(d.error || '删除失败', 'error');
    }
  };

  const handleToggleActive = async (tt: TaskTypeItem) => {
    const res = await fetch(`/api/task-types/${tt.id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ is_active: !tt.is_active })
    });
    if (res.ok) {
      fetchTaskTypes();
      window.dispatchEvent(new CustomEvent('shield-task-types-changed'));
    }
  };

  // File editor functions
  const openFileEditor = async (tt: TaskTypeItem) => {
    setFileEditorTaskId(tt.id);
    setFileEditorTaskName(tt.display_name);
    setActiveFileTab('analysis_prompt');
    setFileDirty({ analysis_prompt: false, synthesis_prompt: false, precondition: false });
    try {
      const res = await fetch(`/api/task-types/${tt.id}/files`);
      if (res.ok) {
        const data = await res.json();
        setFileContents({
          analysis_prompt: data.analysis_prompt || '',
          synthesis_prompt: data.synthesis_prompt || '',
          precondition: data.precondition || ''
        });
      }
    } catch {
      // ignore
    }
    setShowFileEditor(true);
  };

  const handleFileSave = async (fileType: FileTab) => {
    if (!fileEditorTaskId) return;
    setFileSaving(true);
    try {
      const res = await fetch(`/api/task-types/${fileEditorTaskId}/files/${fileType}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content: fileContents[fileType] })
      });
      if (res.ok) {
        showToast('文件已保存', 'success');
        setFileDirty({ ...fileDirty, [fileType]: false });
      } else {
        const d = await res.json();
        showToast(d.error || '保存失败', 'error');
      }
    } catch {
      showToast('网络错误', 'error');
    }
    setFileSaving(false);
  };

  const updateFileContent = (tab: FileTab, content: string) => {
    setFileContents({ ...fileContents, [tab]: content });
    setFileDirty({ ...fileDirty, [tab]: true });
  };

  const fieldStyle: React.CSSProperties = {
    width: '100%',
    padding: '0.6rem 0.75rem',
    borderRadius: '6px',
    border: '1px solid var(--color-border-primary)',
    background: 'var(--color-bg-input)',
    color: 'var(--color-text-primary)',
    outline: 'none',
    boxSizing: 'border-box',
    fontSize: '0.875rem'
  };

  const labelStyle: React.CSSProperties = {
    display: 'block',
    marginBottom: '0.4rem',
    fontSize: '0.8rem',
    color: 'var(--color-text-secondary)',
    fontWeight: 600
  };

  const fileTabLabels: Record<FileTab, string> = {
    analysis_prompt: '分析提示词 (analysis_prompt)',
    synthesis_prompt: '综合报告提示词 (synthesis_prompt)',
    precondition: '前置检查脚本 (precondition.sh)'
  };

  const activeFamilyInfo = domainFamilies.find(f => f.key === form.domain_family);

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.5rem' }}>
        <div>
          <h2 style={{ margin: '0 0 0.3rem 0', fontSize: '1.25rem', fontWeight: 600, color: 'var(--color-text-primary)' }}>任务类型管理</h2>
          <p style={{ color: 'var(--color-text-muted)', margin: 0, fontSize: '0.875rem' }}>
            管理多任务自适应提示词体系、抗辩证据维度模型与闭环执行引擎
          </p>
        </div>
        <button className="btn btn-primary" onClick={() => { resetForm(); setShowForm(true); }}>
          + 新建任务类型
        </button>
      </div>

      <div style={{ background: 'var(--color-bg-surface)', border: '1px solid var(--color-border-primary)', borderRadius: '8px', overflow: 'hidden' }}>
        <table style={{ width: '100%', borderCollapse: 'collapse' }}>
          <thead>
            <tr style={{ borderBottom: '1px solid var(--color-border-primary)', color: 'var(--color-text-secondary)', fontSize: '0.85rem', textAlign: 'left', background: 'var(--color-bg-muted)' }}>
              <th style={{ padding: '0.85rem 1rem' }}>任务名称 / 领域族群</th>
              <th style={{ padding: '0.85rem 1rem' }}>标识 (Key)</th>
              <th style={{ padding: '0.85rem 1rem' }}>执行引擎</th>
              <th style={{ padding: '0.85rem 1rem' }}>抗辩维度</th>
              <th style={{ padding: '0.85rem 1rem' }}>超时(分)</th>
              <th style={{ padding: '0.85rem 1rem' }}>状态</th>
              <th style={{ padding: '0.85rem 1rem', textAlign: 'right' }}>操作</th>
            </tr>
          </thead>
          <tbody>
            {taskTypes.length === 0 ? (
              <EmptyState
                inTable
                colSpan={7}
                type="data"
                title="暂无任务类型"
                description="任务类型定义了扫描与分析规则的领域族群、执行引擎与提示词体系。"
                action={
                  <button className="btn btn-primary" onClick={() => { resetForm(); setShowForm(true); }} style={{ padding: '0.45rem 1rem', fontSize: '0.85rem' }}>
                    新建任务类型
                  </button>
                }
              />
            ) : taskTypes.map(tt => {
              const famMeta = DOMAIN_FAMILY_LABELS[tt.domain_family || 'comprehensive_evolution'] || {
                label: tt.domain_family || '通用演进',
                color: 'var(--color-text-secondary)',
                bg: 'var(--color-bg-muted)'
              };

              let dimsCount = 0;
              if (tt.defense_dimensions) {
                try {
                  const parsed = typeof tt.defense_dimensions === 'string' ? JSON.parse(tt.defense_dimensions) : tt.defense_dimensions;
                  dimsCount = Array.isArray(parsed) ? parsed.length : 0;
                } catch {
                  dimsCount = 0;
                }
              }

              return (
                <tr key={tt.id} style={{ borderBottom: '1px solid var(--color-border-primary)' }}>
                  <td style={{ padding: '0.85rem 1rem' }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', flexWrap: 'wrap' }}>
                      <span style={{ fontWeight: 600, color: 'var(--color-text-primary)' }}>{tt.display_name}</span>
                      {tt.is_builtin && (
                        <span style={{ fontSize: '0.7rem', background: 'rgba(59,130,246,0.1)', color: 'var(--color-primary)', padding: '0.1rem 0.4rem', borderRadius: '4px', fontWeight: 500 }}>
                          内置
                        </span>
                      )}
                      <span style={{
                        fontSize: '0.7rem',
                        background: famMeta.bg,
                        color: famMeta.color,
                        padding: '0.1rem 0.45rem',
                        borderRadius: '4px',
                        fontWeight: 600
                      }}>
                        {famMeta.label}
                      </span>
                      {tt.is_campaign && (
                        <span style={{
                          fontSize: '0.7rem',
                          background: 'rgba(99, 102, 241, 0.1)',
                          color: '#6366f1',
                          border: '1px solid rgba(99, 102, 241, 0.25)',
                          padding: '0.1rem 0.4rem',
                          borderRadius: '4px',
                          fontWeight: 600
                        }}>
                          专项看板
                        </span>
                      )}
                    </div>
                  </td>
                  <td style={{ padding: '0.85rem 1rem', fontFamily: 'monospace', fontSize: '0.8rem', color: 'var(--color-text-muted)' }}>
                    {tt.name}
                  </td>
                  <td style={{ padding: '0.85rem 1rem', fontSize: '0.85rem' }}>
                    {tt.engine_mode === 'debate_full' ? (
                      <span style={{ color: '#7c3aed', background: 'rgba(124,58,237,0.1)', padding: '0.15rem 0.45rem', borderRadius: '4px', fontWeight: 600 }}>全量对抗辩论</span>
                    ) : tt.engine_mode === 'debate_selective' ? (
                      <span style={{ color: 'var(--color-primary)', background: 'rgba(37,99,235,0.1)', padding: '0.15rem 0.45rem', borderRadius: '4px', fontWeight: 600 }}>选择性辩论</span>
                    ) : tt.engine_mode === 'chunked_fast' ? (
                      <span style={{ color: 'var(--color-success)', background: 'rgba(16,185,129,0.1)', padding: '0.15rem 0.45rem', borderRadius: '4px', fontWeight: 600 }}>分片快扫</span>
                    ) : tt.engine_mode === 'chunked' ? (
                      <span style={{ color: '#0284c7', background: 'rgba(2,132,199,0.08)', padding: '0.15rem 0.4rem', borderRadius: '4px', fontWeight: 500 }}>分片引擎</span>
                    ) : (
                      <span style={{ color: 'var(--color-text-muted)', background: 'var(--color-bg-muted)', padding: '0.15rem 0.4rem', borderRadius: '4px' }}>单引擎</span>
                    )}
                  </td>
                  <td style={{ padding: '0.85rem 1rem', fontSize: '0.85rem' }}>
                    {dimsCount > 0 ? (
                      <span style={{ color: 'var(--color-text-primary)', fontWeight: 500 }}>
                        {dimsCount} 维专有
                      </span>
                    ) : (
                      <span style={{ color: 'var(--color-text-muted)', fontSize: '0.8rem' }}>
                        族群默认
                      </span>
                    )}
                  </td>
                  <td style={{ padding: '0.85rem 1rem', fontSize: '0.85rem', color: 'var(--color-text-secondary)' }}>
                    {tt.timeout}
                  </td>
                  <td style={{ padding: '0.85rem 1rem' }}>
                    <div onClick={() => handleToggleActive(tt)} style={{ display: 'inline-flex', alignItems: 'center', gap: '0.5rem', cursor: 'pointer' }}>
                      <div style={{
                        width: 34,
                        height: 20,
                        borderRadius: 10,
                        background: tt.is_active ? 'var(--color-primary)' : 'var(--color-border-primary)',
                        position: 'relative',
                        transition: '0.2s'
                      }}>
                        <div style={{
                          width: 16,
                          height: 16,
                          borderRadius: 8,
                          background: 'white',
                          position: 'absolute',
                          top: 2,
                          left: tt.is_active ? 16 : 2,
                          transition: '0.2s'
                        }} />
                      </div>
                      <span style={{ fontSize: '0.8rem', color: tt.is_active ? 'var(--color-text-primary)' : 'var(--color-text-muted)' }}>
                        {tt.is_active ? '启用' : '停用'}
                      </span>
                    </div>
                  </td>
                  <td style={{ padding: '0.85rem 1rem', textAlign: 'right' }}>
                    <div style={{ display: 'flex', gap: '0.75rem', justifyContent: 'flex-end', alignItems: 'center' }}>
                      <span title="编辑 Prompt 与前置脚本" onClick={() => openFileEditor(tt)} style={{ cursor: 'pointer', display: 'flex', color: 'var(--color-success)' }}>
                        <Code2 size={18} />
                      </span>
                      <span title="配置属性与抗辩模型" onClick={() => handleEdit(tt)} style={{ cursor: 'pointer', display: 'flex', color: 'var(--color-text-secondary)' }}>
                        <Settings size={18} />
                      </span>
                      {!tt.is_builtin && (
                        <span title="删除" onClick={() => handleDelete(tt.id)} style={{ cursor: 'pointer', display: 'flex', color: 'var(--color-danger)' }}>
                          <Trash2 size={18} />
                        </span>
                      )}
                    </div>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      {/* Config Modal */}
      <Modal
        open={showForm}
        onClose={() => { setShowForm(false); resetForm(); }}
        title={editingId ? `编辑任务类型: ${form.display_name || form.name}` : '新建任务类型'}
        width="lg"
        footer={
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.75rem', width: '100%' }}>
            <button
              type="button"
              onClick={() => { setShowForm(false); resetForm(); }}
              style={{
                padding: '0.5rem 1.25rem',
                border: '1px solid var(--color-border-primary)',
                background: 'var(--color-bg-surface)',
                color: 'var(--color-text-primary)',
                borderRadius: '6px',
                cursor: 'pointer',
                fontSize: '0.875rem'
              }}
            >
              取消
            </button>
            <button type="button" onClick={handleSubmit} className="btn btn-primary" style={{ padding: '0.5rem 1.5rem' }}>
              {editingId ? '保存配置' : '立即创建'}
            </button>
          </div>
        }
      >
        <form onSubmit={handleSubmit} style={{ display: 'flex', flexDirection: 'column', gap: '1.25rem', maxHeight: '72vh', overflowY: 'auto', paddingRight: '0.25rem' }}>
          {/* 1. 基础标识与族群 */}
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
            <div>
              <label style={labelStyle}>任务英文标识 (Key)</label>
              <input
                required
                style={fieldStyle}
                value={form.name}
                onChange={e => setForm({ ...form, name: e.target.value })}
                placeholder="如: coredump_risk"
                disabled={!!editingId}
              />
            </div>
            <div>
              <label style={labelStyle}>任务中文显示名称</label>
              <input
                required
                style={fieldStyle}
                value={form.display_name}
                onChange={e => setForm({ ...form, display_name: e.target.value })}
                placeholder="如: 潜在 CoreDump 崩溃风险"
              />
            </div>
          </div>

          {/* 领域族群选择与推荐 */}
          <div style={{
            background: 'var(--color-bg-muted)',
            border: '1px solid var(--color-border-primary)',
            borderRadius: '8px',
            padding: '1rem',
            display: 'flex',
            flexDirection: 'column',
            gap: '0.75rem'
          }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '0.4rem' }}>
                <Shield size={16} color="var(--color-primary)" />
                <span style={{ fontWeight: 600, fontSize: '0.9rem', color: 'var(--color-text-primary)' }}>领域族群 (Domain Family)</span>
              </div>
              {activeFamilyInfo && (
                <button
                  type="button"
                  onClick={handleApplyFamilyRecommendedMode}
                  style={{
                    fontSize: '0.75rem',
                    color: 'var(--color-primary)',
                    background: 'transparent',
                    border: 'none',
                    cursor: 'pointer',
                    display: 'flex',
                    alignItems: 'center',
                    gap: '0.2rem'
                  }}
                >
                  <RefreshCw size={12} />
                  应用族群推荐模式: {activeFamilyInfo.recommended_mode}
                </button>
              )}
            </div>

            <div style={{ display: 'grid', gridTemplateColumns: '1fr', gap: '0.5rem' }}>
              <select
                style={fieldStyle}
                value={form.domain_family}
                onChange={e => setForm({ ...form, domain_family: e.target.value })}
              >
                {domainFamilies.map(fam => (
                  <option key={fam.key} value={fam.key}>
                    {fam.name} ({fam.key}) — 推荐模式: {fam.recommended_mode}
                  </option>
                ))}
              </select>
              {activeFamilyInfo && (
                <p style={{ margin: '0.1rem 0 0 0', fontSize: '0.78rem', color: 'var(--color-text-muted)' }}>
                  💡 族群说明: {activeFamilyInfo.description}
                </p>
              )}
            </div>
          </div>

          <div>
            <label style={labelStyle}>任务描述说明</label>
            <textarea
              style={{ ...fieldStyle, minHeight: '55px', resize: 'vertical' }}
              value={form.description}
              onChange={e => setForm({ ...form, description: e.target.value })}
              placeholder="说明该任务的审计目标、重点关注逻辑与防范指标..."
            />
          </div>

          {/* 2. 辩护对抗维度配置 (Challenger Defense Dimensions) */}
          <div style={{
            background: 'var(--color-bg-surface)',
            border: '1px solid var(--color-border-primary)',
            borderRadius: '8px',
            padding: '1rem',
            display: 'flex',
            flexDirection: 'column',
            gap: '0.75rem'
          }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <div>
                <span style={{ fontWeight: 600, fontSize: '0.9rem', color: 'var(--color-text-primary)' }}>
                  辩护对抗维度 (Defense Dimensions)
                </span>
                <p style={{ margin: '0.15rem 0 0', fontSize: '0.75rem', color: 'var(--color-text-muted)' }}>
                  指导辩护人（Challenger）从哪些角度举证反驳。任务专属自定义 &gt; 族群默认模板。
                </p>
              </div>
              <button
                type="button"
                onClick={handleLoadFamilyDefaults}
                style={{
                  padding: '0.3rem 0.75rem',
                  fontSize: '0.78rem',
                  color: 'var(--color-primary)',
                  background: 'rgba(59,130,246,0.08)',
                  border: '1px solid var(--color-border-primary)',
                  borderRadius: '4px',
                  cursor: 'pointer',
                  display: 'flex',
                  alignItems: 'center',
                  gap: '0.3rem'
                }}
              >
                <Layers size={13} />
                从所属族群载入默认模板
              </button>
            </div>

            {/* 维度卡片列表 */}
            <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
              {form.defense_dimensions.length === 0 ? (
                <div style={{
                  padding: '0.75rem',
                  background: 'var(--color-bg-muted)',
                  borderRadius: '6px',
                  fontSize: '0.8rem',
                  color: 'var(--color-text-muted)',
                  textAlign: 'center'
                }}>
                  当前未配置专属抗辩维度，辩论引擎运行时将自动继承【{activeFamilyInfo?.name || form.domain_family}】的默认维度。
                </div>
              ) : (
                form.defense_dimensions.map((dim, idx) => (
                  <div
                    key={idx}
                    style={{
                      display: 'flex',
                      justifyContent: 'space-between',
                      alignItems: 'center',
                      background: 'var(--color-bg-muted)',
                      border: '1px solid var(--color-border-primary)',
                      borderRadius: '6px',
                      padding: '0.5rem 0.75rem'
                    }}
                  >
                    <div style={{ flex: 1, minWidth: 0, paddingRight: '0.75rem' }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', marginBottom: '0.2rem' }}>
                        <span style={{ fontWeight: 600, fontSize: '0.8rem', color: 'var(--color-text-primary)' }}>
                          {dim.name || dim.key || dim.dimension}
                        </span>
                        {dim.key && dim.name && dim.key !== dim.name && (
                          <span style={{ fontSize: '0.7rem', color: 'var(--color-text-muted)', fontFamily: 'monospace' }}>
                            [{dim.key}]
                          </span>
                        )}
                      </div>
                      <div style={{ fontSize: '0.75rem', color: 'var(--color-text-secondary)', lineHeight: 1.4 }}>
                        {dim.description}
                      </div>
                    </div>
                    <button
                      type="button"
                      onClick={() => handleRemoveDefenseDimension(idx)}
                      style={{
                        background: 'transparent',
                        border: 'none',
                        cursor: 'pointer',
                        color: 'var(--color-danger)',
                        padding: '0.2rem'
                      }}
                      title="删除此维度"
                    >
                      <Trash2 size={15} />
                    </button>
                  </div>
                ))
              )}
            </div>

            {/* 添加新维度行 */}
            <div style={{
              display: 'grid',
              gridTemplateColumns: '1.2fr 1.5fr 2.5fr auto',
              gap: '0.5rem',
              alignItems: 'center',
              marginTop: '0.25rem'
            }}>
              <input
                style={fieldStyle}
                value={newDimKey}
                onChange={e => setNewDimKey(e.target.value)}
                placeholder="标识 (如 Guards)"
              />
              <input
                style={fieldStyle}
                value={newDimName}
                onChange={e => setNewDimName(e.target.value)}
                placeholder="名称 (如 前置防御事实)"
              />
              <input
                style={fieldStyle}
                value={newDimDesc}
                onChange={e => setNewDimDesc(e.target.value)}
                placeholder="抗辩举证说明事实..."
              />
              <button
                type="button"
                onClick={handleAddDefenseDimension}
                className="btn btn-secondary"
                style={{ padding: '0.55rem 0.85rem', fontSize: '0.8rem', whiteSpace: 'nowrap' }}
              >
                <Plus size={14} style={{ marginRight: '0.2rem' }} /> 添加
              </button>
            </div>
          </div>

          {/* 3. 受控标准分类白名单 (Category SSOT) */}
          <div style={{
            background: 'var(--color-bg-surface)',
            border: '1px solid var(--color-border-primary)',
            borderRadius: '8px',
            padding: '1rem',
            display: 'flex',
            flexDirection: 'column',
            gap: '0.6rem'
          }}>
            <div>
              <span style={{ fontWeight: 600, fontSize: '0.9rem', color: 'var(--color-text-primary)' }}>
                受控分类白名单 (Categories SSOT)
              </span>
              <p style={{ margin: '0.15rem 0 0', fontSize: '0.75rem', color: 'var(--color-text-muted)' }}>
                法官（Judge）裁决输出受控分类，严格防止 LLM 自造发散性分类。
              </p>
            </div>

            <div style={{
              display: 'flex',
              flexWrap: 'wrap',
              gap: '0.4rem',
              padding: '0.5rem',
              border: '1px solid var(--color-border-primary)',
              background: 'var(--color-bg-input)',
              borderRadius: '6px',
              minHeight: '38px',
              alignItems: 'center'
            }}>
              {form.categories.map(cat => (
                <span
                  key={cat}
                  style={{
                    display: 'inline-flex',
                    alignItems: 'center',
                    gap: '0.3rem',
                    background: 'var(--color-bg-muted)',
                    border: '1px solid var(--color-border-primary)',
                    color: 'var(--color-text-primary)',
                    padding: '0.2rem 0.5rem',
                    borderRadius: '4px',
                    fontSize: '0.8rem'
                  }}
                >
                  {cat}
                  <span
                    onClick={() => handleRemoveCategory(cat)}
                    style={{ cursor: 'pointer', color: 'var(--color-text-muted)', display: 'inline-flex', alignItems: 'center' }}
                    title="删除分类"
                  >
                    <X size={12} />
                  </span>
                </span>
              ))}
              <input
                type="text"
                value={newCatInput}
                onChange={e => setNewCatInput(e.target.value)}
                onKeyDown={e => {
                  if (e.key === 'Enter') {
                    e.preventDefault();
                    handleAddCategory();
                  }
                }}
                placeholder={form.categories.length === 0 ? '输入分类后按回车添加 (如: 空指针风险-判空遗漏)' : '继续添加分类...'}
                style={{
                  border: 'none',
                  outline: 'none',
                  flex: 1,
                  minWidth: '200px',
                  fontSize: '0.85rem',
                  background: 'transparent',
                  color: 'var(--color-text-primary)',
                  padding: '0.2rem'
                }}
              />
            </div>
          </div>

          {/* 4. 执行模式与引擎配置 */}
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
            <div>
              <label style={labelStyle}>执行引擎模式</label>
              <select
                style={fieldStyle}
                value={form.engine_mode}
                onChange={e => setForm({ ...form, engine_mode: e.target.value })}
              >
                <option value="debate_full">🤖 全量多智能体辩论 (debate_full - 猎手+辩护人+法官)</option>
                <option value="debate_selective">⚖️ 选择性智能体辩论 (debate_selective - 疑点仲裁)</option>
                <option value="chunked_fast">⚡ 语义分片快扫 (chunked_fast - 规则极速初筛)</option>
                <option value="chunked">📦 经典分片引擎 (chunked - 目录并发扫描)</option>
                <option value="single">📄 单次全仓引擎 (single - 单次整仓检视)</option>
              </select>
            </div>
            <div>
              <label style={labelStyle}>处理范围 (Target Scope)</label>
              <select
                style={fieldStyle}
                value={form.target_scope}
                onChange={e => setForm({ ...form, target_scope: e.target.value })}
              >
                <option value="business">仅业务核心代码 (跳过测试用例)</option>
                <option value="test">仅测试代码 (针对单元测试套专项)</option>
                <option value="all">全部代码 (业务源码与测试)</option>
              </select>
            </div>
          </div>

          <div style={{ display: 'flex', flexDirection: 'column' }}>
            <label style={labelStyle}>
              引擎高级参数 <span style={{ fontWeight: 400, color: 'var(--color-text-muted)' }}>(JSON 格式，可选)</span>
            </label>
            <textarea
              style={{
                ...fieldStyle,
                minHeight: '60px',
                resize: 'vertical',
                fontFamily: "'JetBrains Mono', 'Fira Code', monospace",
                fontSize: '0.8rem'
              }}
              value={form.engine_config}
              onChange={e => setForm({ ...form, engine_config: e.target.value })}
              placeholder={'{\n  "max_files": 20,\n  "depth": 1,\n  "concurrency": 6\n}'}
            />
          </div>

          {/* 5. 专项分析看板与治理模式 */}
          <div style={{
            background: 'var(--color-bg-muted)',
            border: '1px solid var(--color-border-primary)',
            borderRadius: '8px',
            padding: '1rem',
            display: 'flex',
            flexDirection: 'column',
            gap: '0.75rem'
          }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <div>
                <span style={{ fontWeight: 600, fontSize: '0.9rem', color: 'var(--color-text-primary)' }}>
                  启用专项分析看板与闭环治理
                </span>
                <p style={{ margin: '0.2rem 0 0', fontSize: '0.75rem', color: 'var(--color-text-muted)' }}>
                  开启后自动在系统侧边栏挂载该专项，启用全仓几何漏斗对账与收敛趋势跟踪
                </p>
              </div>
              <div
                onClick={() => setForm({ ...form, is_campaign: !form.is_campaign })}
                style={{ display: 'inline-flex', alignItems: 'center', cursor: 'pointer' }}
              >
                <div style={{
                  width: 38,
                  height: 22,
                  borderRadius: 11,
                  background: form.is_campaign ? 'var(--color-primary)' : 'var(--color-border-primary)',
                  position: 'relative',
                  transition: '0.2s'
                }}>
                  <div style={{
                    width: 18,
                    height: 18,
                    borderRadius: 9,
                    background: 'white',
                    position: 'absolute',
                    top: 2,
                    left: form.is_campaign ? 18 : 2,
                    transition: '0.2s'
                  }} />
                </div>
              </div>
            </div>

            {form.is_campaign && (
              <div style={{ borderTop: '1px solid var(--color-border-primary)', paddingTop: '0.75rem', display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
                <div>
                  <label style={labelStyle}>专项治理模式</label>
                  <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.75rem' }}>
                    <div
                      onClick={() => setForm({ ...form, governance_mode: 'full_ledger' })}
                      style={{
                        padding: '0.75rem',
                        borderRadius: '6px',
                        cursor: 'pointer',
                        border: `1.5px solid ${form.governance_mode === 'full_ledger' ? 'var(--color-primary)' : 'var(--color-border-primary)'}`,
                        background: form.governance_mode === 'full_ledger' ? 'rgba(37,99,235,0.06)' : 'var(--color-bg-surface)'
                      }}
                    >
                      <div style={{ fontWeight: 600, fontSize: '0.85rem', color: form.governance_mode === 'full_ledger' ? 'var(--color-primary)' : 'var(--color-text-primary)' }}>
                        全量台账模式 (full_ledger)
                      </div>
                      <div style={{ fontSize: '0.75rem', color: 'var(--color-text-muted)', marginTop: '0.2rem' }}>
                        沉淀跨轮 SSOT 台账，六级几何漏斗对账与退火休眠，适合架构/全量检视 (推荐)
                      </div>
                    </div>

                    <div
                      onClick={() => setForm({ ...form, governance_mode: 'change_focus' })}
                      style={{
                        padding: '0.75rem',
                        borderRadius: '6px',
                        cursor: 'pointer',
                        border: `1.5px solid ${form.governance_mode === 'change_focus' ? 'var(--color-success)' : 'var(--color-border-primary)'}`,
                        background: form.governance_mode === 'change_focus' ? 'rgba(16,185,129,0.06)' : 'var(--color-bg-surface)'
                      }}
                    >
                      <div style={{ fontWeight: 600, fontSize: '0.85rem', color: form.governance_mode === 'change_focus' ? 'var(--color-success)' : 'var(--color-text-primary)' }}>
                        变更增量焦点模式 (change_focus)
                      </div>
                      <div style={{ fontSize: '0.75rem', color: 'var(--color-text-muted)', marginTop: '0.2rem' }}>
                        未变动文件物理旁路隔离，专注增量缺陷拦截与顺带修复核销
                      </div>
                    </div>

                    <div
                      onClick={() => setForm({ ...form, governance_mode: 'entity_assessment' })}
                      style={{
                        padding: '0.75rem',
                        borderRadius: '6px',
                        cursor: 'pointer',
                        border: `1.5px solid ${form.governance_mode === 'entity_assessment' ? 'var(--color-primary)' : 'var(--color-border-primary)'}`,
                        background: form.governance_mode === 'entity_assessment' ? 'rgba(13,148,136,0.06)' : 'var(--color-bg-surface)'
                      }}
                    >
                      <div style={{ fontWeight: 600, fontSize: '0.85rem', color: form.governance_mode === 'entity_assessment' ? '#0d9488' : 'var(--color-text-primary)' }}>
                        全量实体评估模式 (entity_assessment)
                      </div>
                      <div style={{ fontSize: '0.75rem', color: 'var(--color-text-muted)', marginTop: '0.2rem' }}>
                        度量实体总数/合格数/合格率，适合单元测试用例有效性等场景
                      </div>
                    </div>

                    <div
                      onClick={() => setForm({ ...form, governance_mode: 'defect_tracking' })}
                      style={{
                        padding: '0.75rem',
                        borderRadius: '6px',
                        cursor: 'pointer',
                        border: `1.5px solid ${form.governance_mode === 'defect_tracking' ? 'var(--color-primary)' : 'var(--color-border-primary)'}`,
                        background: form.governance_mode === 'defect_tracking' ? 'rgba(99,102,241,0.06)' : 'var(--color-bg-surface)'
                      }}
                    >
                      <div style={{ fontWeight: 600, fontSize: '0.85rem', color: form.governance_mode === 'defect_tracking' ? '#4f46e5' : 'var(--color-text-primary)' }}>
                        缺陷攻关模式 (defect_tracking)
                      </div>
                      <div style={{ fontSize: '0.75rem', color: 'var(--color-text-muted)', marginTop: '0.2rem' }}>
                        经典平铺攻关模式（引擎内部将自动按 full_ledger 自愈对账）
                      </div>
                    </div>
                  </div>
                </div>

                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
                  <div>
                    <label style={labelStyle}>
                      路由别名 (URL Path) <span style={{ fontWeight: 400, color: 'var(--color-text-muted)' }}>(空则同标识名)</span>
                    </label>
                    <input
                      style={fieldStyle}
                      value={form.campaign_path}
                      onChange={e => setForm({ ...form, campaign_path: e.target.value.trim() })}
                      placeholder="如: float, ut, coredump"
                    />
                  </div>
                  <div>
                    <label style={labelStyle}>
                      专项图标类名/SVG <span style={{ fontWeight: 400, color: 'var(--color-text-muted)' }}>(可选)</span>
                    </label>
                    <input
                      style={fieldStyle}
                      value={form.campaign_icon}
                      onChange={e => setForm({ ...form, campaign_icon: e.target.value })}
                      placeholder="SVG path 或图标名称"
                    />
                  </div>
                </div>
              </div>
            )}
          </div>

          {/* 6. 超时与通知配置 */}
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem' }}>
            <div>
              <label style={labelStyle}>执行超时（分钟）</label>
              <input
                type="number"
                style={fieldStyle}
                value={form.timeout}
                onChange={e => setForm({ ...form, timeout: parseInt(e.target.value, 10) || 60 })}
              />
            </div>
            <div>
              <label style={labelStyle}>通知阈值（缺陷评分 &ge; 此值才发送通知）</label>
              <input
                type="number"
                style={fieldStyle}
                value={form.notify_threshold}
                onChange={e => setForm({ ...form, notify_threshold: parseInt(e.target.value, 10) || 0 })}
              />
            </div>
          </div>

          <div>
            <label style={labelStyle}>
              通知抄送 <span style={{ fontWeight: 400, color: 'var(--color-text-muted)' }}>（任务完成后额外抄送的邮箱）</span>
            </label>
            <div style={{
              display: 'flex',
              flexWrap: 'wrap',
              gap: '0.4rem',
              padding: '0.5rem',
              border: '1px solid var(--color-border-primary)',
              background: 'var(--color-bg-input)',
              borderRadius: '6px',
              minHeight: '38px',
              alignItems: 'center'
            }}>
              {form.notify_cc.map(email => (
                <span
                  key={email}
                  style={{
                    display: 'inline-flex',
                    alignItems: 'center',
                    gap: '0.3rem',
                    background: 'rgba(37,99,235,0.08)',
                    color: 'var(--color-primary)',
                    padding: '0.2rem 0.5rem',
                    borderRadius: '4px',
                    fontSize: '0.8rem'
                  }}
                >
                  {email}
                  <span
                    onClick={() => handleRemoveCc(email)}
                    style={{ cursor: 'pointer', color: 'var(--color-text-muted)', display: 'inline-flex', alignItems: 'center' }}
                    title="移除邮箱"
                  >
                    <X size={12} />
                  </span>
                </span>
              ))}
              <input
                type="email"
                value={ccInput}
                onChange={e => setCcInput(e.target.value)}
                onKeyDown={e => {
                  if (e.key === 'Enter') {
                    e.preventDefault();
                    handleAddCc();
                  }
                }}
                placeholder={form.notify_cc.length === 0 ? '输入邮箱后按回车添加' : '继续添加邮箱...'}
                style={{
                  border: 'none',
                  outline: 'none',
                  flex: 1,
                  minWidth: '150px',
                  fontSize: '0.85rem',
                  background: 'transparent',
                  color: 'var(--color-text-primary)',
                  padding: '0.2rem'
                }}
              />
            </div>
          </div>
        </form>
      </Modal>

      {/* File Editor Drawer */}
      <Drawer
        open={showFileEditor}
        onClose={() => setShowFileEditor(false)}
        title={`编辑脚本 — ${fileEditorTaskName}`}
        subtitle="配置 AI 任务提示词（分析/综合阶段）与前置 Bash 检查脚本"
        width="min(1000px, 92vw)"
        bodyStyle={{ padding: 0, gap: 0, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}
        footer={
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', width: '100%' }}>
            <span style={{ fontSize: '0.78rem', color: 'var(--color-text-muted)' }}>
              {activeFileTab === 'analysis_prompt'
                ? '分析阶段提示词 · 领域规则将注入猎手与辩论流'
                : activeFileTab === 'synthesis_prompt'
                ? '综合报告提示词 · AI 输出 Markdown'
                : 'Bash 脚本 · 前置: exit 0=继续, 1=跳过, 2=失败'}
            </span>
            <button
              className="btn btn-primary"
              onClick={() => handleFileSave(activeFileTab)}
              disabled={fileSaving || !fileDirty[activeFileTab]}
              style={{ padding: '0.4rem 1.2rem', fontSize: '0.85rem', opacity: fileDirty[activeFileTab] ? 1 : 0.5 }}
            >
              {fileSaving ? '保存中...' : '保存'}
            </button>
          </div>
        }
      >
        {/* Tabs */}
        <div style={{ display: 'flex', borderBottom: '1px solid var(--color-border-primary)', flexShrink: 0, background: 'var(--color-bg-muted)' }}>
          {(['analysis_prompt', 'synthesis_prompt', 'precondition'] as FileTab[]).map(tab => (
            <button
              key={tab}
              onClick={() => setActiveFileTab(tab)}
              style={{
                padding: '0.75rem 1.25rem',
                border: 'none',
                background: 'transparent',
                cursor: 'pointer',
                fontWeight: activeFileTab === tab ? 600 : 400,
                fontSize: '0.85rem',
                color: activeFileTab === tab ? 'var(--color-primary)' : 'var(--color-text-secondary)',
                borderBottom: activeFileTab === tab ? '2px solid var(--color-primary)' : '2px solid transparent',
                transition: 'all 0.15s'
              }}
            >
              {fileTabLabels[tab]}
              {fileDirty[tab] && <span style={{ marginLeft: '0.3rem', color: 'var(--color-warning)', fontSize: '0.7rem' }}>●</span>}
            </button>
          ))}
        </div>

        {/* Editor */}
        <div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
          <textarea
            value={fileContents[activeFileTab]}
            onChange={e => updateFileContent(activeFileTab, e.target.value)}
            spellCheck={false}
            style={{
              flex: 1,
              width: '100%',
              height: '100%',
              padding: '1.25rem',
              border: 'none',
              outline: 'none',
              resize: 'none',
              fontFamily: "'JetBrains Mono', 'Fira Code', 'Consolas', monospace",
              fontSize: '0.85rem',
              lineHeight: '1.6',
              background: 'var(--color-bg-input)',
              color: 'var(--color-text-primary)',
              boxSizing: 'border-box'
            }}
            placeholder={
              activeFileTab === 'analysis_prompt' || activeFileTab === 'synthesis_prompt'
                ? '在此编写 AI 任务提示词（Markdown 格式）...'
                : '在此编写 Bash 脚本...'
            }
          />
        </div>
      </Drawer>
    </div>
  );
}

export default TaskTypeManagement;
