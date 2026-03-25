import React, { useState, useEffect, useCallback } from 'react';
import ReactDOM from 'react-dom';

interface ScheduleSidebarProps {
  isOpen: boolean;
  onClose: () => void;
  onSave: (form: ScheduleFormData) => void;
  editingSchedule?: any;
  teams: any[];
  repos: any[];
}

export interface ScheduleFormData {
  name: string;
  cron_expr: string;
  task_type_id: number;
  target_mode: string;
  target_values: any[];
  auto_notify: boolean;
  is_active: boolean;
}

const CRON_PRESETS = [
  { label: '每天凌晨 2 点', value: '0 2 * * *' },
  { label: '每周日午夜', value: '0 0 * * 0' },
  { label: '每 30 分钟', value: '*/30 * * * *' },
  { label: '工作日每天 8 点', value: '0 8 * * 1-5' },
  { label: '每月 1 号', value: '0 0 1 * *' },
  { label: '每 6 小时', value: '0 */6 * * *' },
];

const TARGET_MODES = [
  { key: 'all', label: '所有仓库' },
  { key: 'service_group', label: '按服务组' },
  { key: 'team', label: '按团队' },
  { key: 'specific', label: '指定仓库' },
];

function parseCronToHuman(expr: string): string {
  if (!expr || !expr.trim()) return '';
  const parts = expr.trim().split(/\s+/);
  if (parts.length !== 5) return '自定义 Cron 表达式';

  const [min, hour, dom, , dow] = parts;

  // Common patterns
  if (expr === '*/30 * * * *') return '每 30 分钟执行一次';
  if (expr === '*/15 * * * *') return '每 15 分钟执行一次';
  if (min.startsWith('*/')) return `每 ${min.slice(2)} 分钟执行一次`;
  if (hour.startsWith('*/') && min === '0') return `每 ${hour.slice(2)} 小时执行一次`;
  
  if (dom === '1' && hour !== '*' && min !== '*') {
    return `每月 1 号 ${hour.padStart(2, '0')}:${min.padStart(2, '0')} 执行`;
  }
  if (dow === '0' && hour !== '*' && min !== '*') {
    return `每周日 ${hour.padStart(2, '0')}:${min.padStart(2, '0')} 执行`;
  }
  if (dow === '1-5' && hour !== '*' && min !== '*') {
    return `工作日 ${hour.padStart(2, '0')}:${min.padStart(2, '0')} 执行`;
  }
  if (hour !== '*' && min !== '*' && dom === '*' && dow === '*') {
    return `每天 ${hour.padStart(2, '0')}:${min.padStart(2, '0')} 执行`;
  }

  return '自定义 Cron 表达式';
}

function getTargetSummary(mode: string): string {
  switch (mode) {
    case 'all': return '将对系统中所有已激活的代码仓执行任务';
    case 'service_group': return '仅对所选服务组内的代码仓执行任务';
    case 'team': return '仅对所选团队名下的代码仓执行任务';
    case 'specific': return '仅对手动选定的代码仓执行任务';
    default: return '';
  }
}

export default function ScheduleSidebar({ isOpen, onClose, onSave, editingSchedule, teams, repos }: ScheduleSidebarProps) {
  const [form, setForm] = useState<ScheduleFormData>({
    name: '', cron_expr: '0 2 * * *', task_type_id: 0, target_mode: 'all', target_values: [], auto_notify: true, is_active: true
  });
  const [closing, setClosing] = useState(false);
  const [taskTypes, setTaskTypes] = useState<any[]>([]);

  // Fetch task types
  useEffect(() => {
    fetch('/api/task-types?active_only=true')
      .then(res => res.json())
      .then(data => {
        setTaskTypes(data || []);
        if (!form.task_type_id && data && data.length > 0) {
          setForm(prev => ({ ...prev, task_type_id: data[0].id }));
        }
      })
      .catch(console.error);
  }, []);

  // Populate form when editing
  useEffect(() => {
    if (editingSchedule) {
      setForm({
        name: editingSchedule.name || '',
        cron_expr: editingSchedule.cron_expr || '0 2 * * *',
        task_type_id: editingSchedule.task_type_id || (taskTypes.length > 0 ? taskTypes[0].id : 0),
        target_mode: editingSchedule.target_mode || 'all',
        target_values: editingSchedule.target_values || [],
        auto_notify: editingSchedule.auto_notify ?? true,
        is_active: editingSchedule.is_active ?? true,
      });
    } else {
      setForm({ name: '', cron_expr: '0 2 * * *', task_type_id: taskTypes.length > 0 ? taskTypes[0].id : 0, target_mode: 'all', target_values: [], auto_notify: true, is_active: true });
    }
  }, [editingSchedule, isOpen]);

  const handleClose = useCallback(() => {
    setClosing(true);
    setTimeout(() => {
      setClosing(false);
      onClose();
    }, 250);
  }, [onClose]);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    onSave(form);
  };

  // Close on Escape
  useEffect(() => {
    if (!isOpen) return;
    const handler = (e: KeyboardEvent) => { if (e.key === 'Escape') handleClose(); };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [isOpen, handleClose]);

  if (!isOpen) return null;

  const isEditing = !!editingSchedule;
  const activeCronPreset = CRON_PRESETS.find(p => p.value === form.cron_expr);

  const sidebarContent = (
    <>
      {/* Overlay */}
      <div
        className={`sidebar-overlay${closing ? ' closing' : ''}`}
        onClick={handleClose}
      />

      {/* Sidebar Panel */}
      <div className={`sidebar-panel${closing ? ' closing' : ''}`}>
        {/* Header */}
        <div className="sidebar-header">
          <h3>{isEditing ? '编辑定时策略' : '新增定时策略'}</h3>
          <button className="sidebar-close-btn" onClick={handleClose} type="button">
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
            </svg>
          </button>
        </div>

        {/* Body */}
        <form onSubmit={handleSubmit} style={{ display: 'flex', flexDirection: 'column', flex: 1, overflow: 'hidden' }}>
          <div className="sidebar-body">

            {/* Section 1: Basic Info */}
            <div className="sidebar-section">
              <div className="sidebar-section-title">
                <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M11 4H4a2 2 0 00-2 2v14a2 2 0 002 2h14a2 2 0 002-2v-7" />
                  <path d="M18.5 2.5a2.121 2.121 0 013 3L12 15l-4 1 1-4 9.5-9.5z" />
                </svg>
                基本信息
              </div>
              <label className="sidebar-label">策略名称</label>
              <input
                className="sidebar-input"
                required
                value={form.name}
                onChange={e => setForm({ ...form, name: e.target.value })}
                placeholder="例如：每日核心服务全量巡检"
              />

              <label className="sidebar-label" style={{ marginTop: '1rem' }}>任务类型</label>
              <select
                className="sidebar-input"
                value={form.task_type_id}
                onChange={e => setForm({ ...form, task_type_id: Number(e.target.value) })}
                style={{ cursor: 'pointer' }}
              >
                {taskTypes.map(tt => (
                  <option key={tt.id} value={tt.id}>{tt.display_name}</option>
                ))}
              </select>
              <div style={{ marginTop: '1.25rem', display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                <input
                  type="checkbox"
                  id="autoNotify"
                  checked={form.auto_notify}
                  onChange={e => setForm({ ...form, auto_notify: e.target.checked })}
                  style={{ width: '16px', height: '16px', cursor: 'pointer', accentColor: 'var(--primary-color)' }}
                />
                <label htmlFor="autoNotify" style={{ fontSize: '0.875rem', color: 'var(--text-color)', cursor: 'pointer', fontWeight: 500 }}>
                  任务完成后自动发消息通知相关责任人
                </label>
              </div>
            </div>

            {/* Section 2: Execution Plan */}
            <div className="sidebar-section">
              <div className="sidebar-section-title">
                <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <circle cx="12" cy="12" r="10" /><polyline points="12 6 12 12 16 14" />
                </svg>
                执行计划
              </div>

              <label className="sidebar-label">快捷预设</label>
              <div className="cron-presets">
                {CRON_PRESETS.map(preset => (
                  <button
                    key={preset.value}
                    type="button"
                    className={`cron-preset-chip${form.cron_expr === preset.value ? ' active' : ''}`}
                    onClick={() => setForm({ ...form, cron_expr: preset.value })}
                  >
                    {preset.label}
                  </button>
                ))}
              </div>

              <label className="sidebar-label">Cron 表达式</label>
              <input
                className="sidebar-input mono"
                required
                value={form.cron_expr}
                onChange={e => setForm({ ...form, cron_expr: e.target.value })}
                placeholder="分 时 日 月 周"
              />

              {form.cron_expr && (
                <div className="cron-hint">
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                    <circle cx="12" cy="12" r="10" /><line x1="12" y1="16" x2="12" y2="12" /><line x1="12" y1="8" x2="12.01" y2="8" />
                  </svg>
                  {parseCronToHuman(form.cron_expr)}
                </div>
              )}
            </div>

            {/* Section 3: Target Scope */}
            <div className="sidebar-section">
              <div className="sidebar-section-title">
                <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <circle cx="12" cy="12" r="10" /><circle cx="12" cy="12" r="6" /><circle cx="12" cy="12" r="2" />
                </svg>
                目标范围
              </div>

              <div className="target-segments">
                {TARGET_MODES.map(mode => (
                  <button
                    key={mode.key}
                    type="button"
                    className={`target-segment${form.target_mode === mode.key ? ' active' : ''}`}
                    onClick={() => setForm({ ...form, target_mode: mode.key, target_values: [] })}
                  >
                    {mode.label}
                  </button>
                ))}
              </div>

              <div className="target-summary">{getTargetSummary(form.target_mode)}</div>

              {/* Sub-options based on target mode */}
              {form.target_mode === 'service_group' && (
                <div style={{ marginTop: '0.75rem' }}>
                  <label className="sidebar-label">服务组名称</label>
                  <input
                    className="sidebar-input"
                    placeholder="例如：Backend, Frontend（多个用逗号分隔）"
                    value={(form.target_values || []).join(', ')}
                    onChange={(e) => setForm({
                      ...form,
                      target_values: e.target.value.split(',').map(s => s.trim()).filter(s => s)
                    })}
                  />
                </div>
              )}

              {form.target_mode === 'team' && (
                <div style={{ marginTop: '0.75rem', display: 'flex', flexWrap: 'wrap', gap: '0.6rem' }}>
                  {teams.length === 0 ? (
                    <span style={{ fontSize: '0.8rem', color: '#94a3b8' }}>暂无团队数据</span>
                  ) : teams.map(team => (
                    <label key={team.id} style={{ display: 'flex', alignItems: 'center', gap: '0.35rem', fontSize: '0.85rem', cursor: 'pointer', padding: '0.3rem 0.6rem', borderRadius: '6px', background: (form.target_values || []).includes(team.id) ? 'rgba(37,99,235,0.08)' : 'transparent', border: `1px solid ${(form.target_values || []).includes(team.id) ? 'var(--primary-color)' : 'var(--border-color)'}`, transition: 'all 0.15s' }}>
                      <input
                        type="checkbox"
                        checked={(form.target_values || []).includes(team.id)}
                        onChange={(e) => {
                          const newVals = e.target.checked
                            ? [...(form.target_values || []), team.id]
                            : (form.target_values || []).filter((id: any) => id !== team.id);
                          setForm({ ...form, target_values: newVals });
                        }}
                        style={{ display: 'none' }}
                      />
                      <span style={{ width: 16, height: 16, borderRadius: 4, border: `2px solid ${(form.target_values || []).includes(team.id) ? 'var(--primary-color)' : '#cbd5e1'}`, display: 'flex', alignItems: 'center', justifyContent: 'center', background: (form.target_values || []).includes(team.id) ? 'var(--primary-color)' : 'transparent', transition: 'all 0.15s' }}>
                        {(form.target_values || []).includes(team.id) && (
                          <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="white" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round"><polyline points="20 6 9 17 4 12" /></svg>
                        )}
                      </span>
                      {team.name}
                    </label>
                  ))}
                </div>
              )}

              {form.target_mode === 'specific' && (
                <div style={{ marginTop: '0.75rem', display: 'flex', flexWrap: 'wrap', gap: '0.5rem', maxHeight: '180px', overflowY: 'auto' }}>
                  {repos.length === 0 ? (
                    <span style={{ fontSize: '0.8rem', color: '#94a3b8' }}>暂无仓库数据</span>
                  ) : repos.map(repo => (
                    <label key={repo.id} style={{ display: 'flex', alignItems: 'center', gap: '0.35rem', fontSize: '0.825rem', cursor: 'pointer', padding: '0.3rem 0.6rem', borderRadius: '6px', background: (form.target_values || []).includes(repo.id) ? 'rgba(37,99,235,0.08)' : 'transparent', border: `1px solid ${(form.target_values || []).includes(repo.id) ? 'var(--primary-color)' : 'var(--border-color)'}`, transition: 'all 0.15s', maxWidth: '100%' }}>
                      <input
                        type="checkbox"
                        checked={(form.target_values || []).includes(repo.id)}
                        onChange={(e) => {
                          const newVals = e.target.checked
                            ? [...(form.target_values || []), repo.id]
                            : (form.target_values || []).filter((id: any) => id !== repo.id);
                          setForm({ ...form, target_values: newVals });
                        }}
                        style={{ display: 'none' }}
                      />
                      <span style={{ width: 16, height: 16, borderRadius: 4, border: `2px solid ${(form.target_values || []).includes(repo.id) ? 'var(--primary-color)' : '#cbd5e1'}`, display: 'flex', alignItems: 'center', justifyContent: 'center', background: (form.target_values || []).includes(repo.id) ? 'var(--primary-color)' : 'transparent', transition: 'all 0.15s', flexShrink: 0 }}>
                        {(form.target_values || []).includes(repo.id) && (
                          <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="white" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round"><polyline points="20 6 9 17 4 12" /></svg>
                        )}
                      </span>
                      <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{repo.name}</span>
                    </label>
                  ))}
                </div>
              )}
            </div>
          </div>

          {/* Footer */}
          <div className="sidebar-footer">
            <button type="button" className="sidebar-btn-cancel" onClick={handleClose}>取消</button>
            <button type="submit" className="sidebar-btn-submit">{isEditing ? '保存修改' : '创建策略'}</button>
          </div>
        </form>
      </div>
    </>
  );

  return ReactDOM.createPortal(sidebarContent, document.body);
}
