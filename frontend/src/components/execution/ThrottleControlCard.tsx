import React, { useState, useEffect } from 'react';

export interface ThrottleControlCardProps {
  sysConfig: any;
  isAdmin: boolean;
  onApplyScale: (scale: number, durationHours: number) => Promise<void>;
  onToggleQueue: (paused: boolean) => Promise<void>;
  applyingConfig: boolean;
  togglingQueue: boolean;
}

export const ThrottleControlCard: React.FC<ThrottleControlCardProps> = ({
  sysConfig,
  isAdmin,
  onApplyScale,
  onToggleQueue,
  applyingConfig,
  togglingQueue,
}) => {
  const [selectedScale, setSelectedScale] = useState<number>(1.0);
  const [durationHours, setDurationHours] = useState<number>(2);

  useEffect(() => {
    if (sysConfig?.concurrency_scale !== undefined) {
      setSelectedScale(sysConfig.concurrency_scale);
    }
  }, [sysConfig]);

  const isThrottleActive = !!(sysConfig && sysConfig.concurrency_scale !== 1.0);
  const throttleMode = sysConfig?.throttle_mode || (isThrottleActive ? 'manual' : 'normal');
  const workHoursCfg = sysConfig?.work_hours_config;

  return (
    <div
      style={{
        background: 'var(--color-bg-surface, var(--card-bg, #ffffff))',
        border: '1px solid var(--color-border-primary, var(--border-color, #e2e8f0))',
        borderRadius: '12px',
        padding: '1.25rem',
        marginBottom: '1.5rem',
        boxShadow: 'var(--shadow-sm, 0 1px 3px rgba(0,0,0,0.05))',
        display: 'flex',
        flexWrap: 'wrap',
        justifyContent: 'space-between',
        alignItems: 'center',
        gap: '1rem',
      }}
    >
      {/* 左侧状态显示 */}
      <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', minWidth: '300px', flex: '1 1 auto' }}>
        <div
          style={{
            width: '40px',
            height: '40px',
            borderRadius: '8px',
            background: isThrottleActive
              ? sysConfig.concurrency_scale === 0
                ? 'rgba(239, 68, 68, 0.1)'
                : sysConfig.concurrency_scale < 1
                ? 'rgba(245, 158, 11, 0.1)'
                : 'rgba(16, 185, 129, 0.1)'
              : 'rgba(59, 130, 246, 0.1)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            color: isThrottleActive
              ? sysConfig.concurrency_scale === 0
                ? 'var(--color-danger, #ef4444)'
                : sysConfig.concurrency_scale < 1
                ? 'var(--color-warning, #f59e0b)'
                : 'var(--color-success, #10b981)'
              : 'var(--color-primary, #2563eb)',
            flexShrink: 0,
          }}
        >
          <svg width="20" height="20" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
            <path strokeLinecap="round" strokeLinejoin="round" d="M13 10V3L4 14h7v7l9-11h-7z" />
          </svg>
        </div>
        <div>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.2rem', flexWrap: 'wrap' }}>
            <h4 style={{ margin: 0, fontSize: '0.95rem', fontWeight: 600, color: 'var(--color-text-primary, var(--text-color, #0f172a))' }}>
              AI 扫描并发流控
            </h4>
            {throttleMode === 'work_hours' ? (
              sysConfig.concurrency_scale === 0 ? (
                <span style={{ fontSize: '0.75rem', padding: '0.15rem 0.4rem', borderRadius: '4px', background: 'rgba(239, 68, 68, 0.12)', color: 'var(--color-danger, #ef4444)', fontWeight: 600 }}>
                  工作时间暂停中
                </span>
              ) : (
                <span style={{ fontSize: '0.75rem', padding: '0.15rem 0.4rem', borderRadius: '4px', background: 'rgba(245, 158, 11, 0.12)', color: 'var(--color-warning, #d97706)', fontWeight: 600 }}>
                  工作时间限速中 {Math.round(sysConfig.concurrency_scale * 100)}%
                </span>
              )
            ) : throttleMode === 'manual' ? (
              sysConfig.concurrency_scale === 0 ? (
                <span style={{ fontSize: '0.75rem', padding: '0.15rem 0.4rem', borderRadius: '4px', background: 'rgba(239, 68, 68, 0.12)', color: 'var(--color-danger, #ef4444)', fontWeight: 600 }}>
                  临时暂停中 (手动)
                </span>
              ) : sysConfig.concurrency_scale < 1 ? (
                <span style={{ fontSize: '0.75rem', padding: '0.15rem 0.4rem', borderRadius: '4px', background: 'rgba(245, 158, 11, 0.12)', color: 'var(--color-warning, #d97706)', fontWeight: 600 }}>
                  手动限速中 {Math.round(sysConfig.concurrency_scale * 100)}%
                </span>
              ) : (
                <span style={{ fontSize: '0.75rem', padding: '0.15rem 0.4rem', borderRadius: '4px', background: 'rgba(16, 185, 129, 0.12)', color: 'var(--color-success, #059669)', fontWeight: 600 }}>
                  手动加速中 {Math.round(sysConfig.concurrency_scale * 100)}%
                </span>
              )
            ) : (
              <span style={{ fontSize: '0.75rem', padding: '0.15rem 0.4rem', borderRadius: '4px', background: 'rgba(37, 99, 235, 0.12)', color: 'var(--color-primary, #2563eb)', fontWeight: 600 }}>
                正常速度运行 (100%)
              </span>
            )}

            {/* 队列调度状态与控制 (紧跟在并发流控后方) */}
            <div
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: '0.5rem',
                marginLeft: '0.75rem',
                paddingLeft: '0.75rem',
                borderLeft: '1px solid var(--color-border-primary, var(--border-color, #e2e8f0))',
              }}
            >
              <h4 style={{ margin: 0, fontSize: '0.95rem', fontWeight: 600, color: 'var(--color-text-primary, var(--text-color, #0f172a))' }}>
                队列调度
              </h4>
              {sysConfig == null ? (
                <span style={{ fontSize: '0.75rem', padding: '0.15rem 0.4rem', borderRadius: '4px', background: 'var(--color-bg-muted, #f1f5f9)', color: 'var(--color-text-muted, #64748b)', fontWeight: 600 }}>
                  加载中…
                </span>
              ) : sysConfig.queue_paused ? (
                <span style={{ fontSize: '0.75rem', padding: '0.15rem 0.4rem', borderRadius: '4px', background: 'rgba(239, 68, 68, 0.12)', color: 'var(--color-danger, #ef4444)', fontWeight: 600 }}>
                  已暂停 (排空模式)
                </span>
              ) : (
                <span style={{ fontSize: '0.75rem', padding: '0.15rem 0.4rem', borderRadius: '4px', background: 'rgba(37, 99, 235, 0.12)', color: 'var(--color-primary, #2563eb)', fontWeight: 600 }}>
                  正常分发中
                </span>
              )}
              {isAdmin && (
                <button
                  className="btn"
                  disabled={togglingQueue || !sysConfig}
                  onClick={() => sysConfig && onToggleQueue(!sysConfig.queue_paused)}
                  style={{
                    height: '24px',
                    padding: '0 0.5rem',
                    fontSize: '0.75rem',
                    display: 'inline-flex',
                    alignItems: 'center',
                    background: sysConfig?.queue_paused ? 'var(--color-primary, #3b82f6)' : 'transparent',
                    color: sysConfig?.queue_paused ? '#ffffff' : 'var(--color-text-primary, #1e293b)',
                    border: sysConfig?.queue_paused ? '1px solid transparent' : '1px solid var(--color-border-primary, var(--border-color, #e2e8f0))',
                  }}
                >
                  {togglingQueue ? '切换中...' : sysConfig?.queue_paused ? '恢复派发' : '暂停派发'}
                </button>
              )}
            </div>
          </div>
          <p style={{ margin: 0, fontSize: '0.825rem', color: 'var(--color-text-secondary, #64748b)', lineHeight: '1.3' }}>
            {throttleMode === 'work_hours' ? (
              <>
                当前处于工作时间窗口（{workHoursCfg?.start_time || '09:00'} ~ {workHoursCfg?.end_time || '22:00'}），已自动限制并发为 <strong>{Math.round(sysConfig.concurrency_scale * 100)}%</strong>，夜间将自动恢复满速。
              </>
            ) : throttleMode === 'manual' ? (
              sysConfig.scale_expires_at ? (
                <>
                  管理员临时调整并发为 <strong>{Math.round(sysConfig.concurrency_scale * 100)}%</strong>，预计于 <strong>{new Date(sysConfig.scale_expires_at).toLocaleTimeString()}</strong> 自动恢复计划模式。
                </>
              ) : (
                <>
                  管理员手动常驻调整并发为 <strong>{Math.round(sysConfig.concurrency_scale * 100)}%</strong>。
                </>
              )
            ) : (
              '非工作时间/夜间满速运行，使用系统配置文件中预设的并发配额处理扫描任务。'
            )}
          </p>
        </div>
      </div>

      {/* 右侧控制栏 */}
      <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', flexWrap: 'wrap' }}>
        {isAdmin ? (
          <>
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.35rem' }}>
              <span style={{ fontSize: '0.825rem', fontWeight: 600, color: 'var(--color-text-secondary, #64748b)' }}>并发速度：</span>
              <select
                value={selectedScale}
                onChange={e => setSelectedScale(parseFloat(e.target.value))}
                style={{
                  padding: '0.35rem 0.5rem',
                  borderRadius: '6px',
                  border: '1px solid var(--color-border-primary, var(--border-color, #e2e8f0))',
                  outline: 'none',
                  fontSize: '0.875rem',
                  background: 'var(--color-bg-input, var(--bg-color, #ffffff))',
                  color: 'var(--color-text-primary, var(--text-color, #0f172a))',
                  cursor: 'pointer',
                  height: '32px',
                }}
              >
                <option value={0}>0% (临时暂停所有扫描)</option>
                <option value={0.25}>25% (极度限速)</option>
                <option value={0.5}>50% (半速运行)</option>
                <option value={0.75}>75% (微调限速)</option>
                <option value={1.0}>100% (恢复默认速度)</option>
                <option value={1.25}>125% (微调加速)</option>
                <option value={1.5}>150% (快速运行)</option>
                <option value={2.0}>200% (双倍加速 - 高物理负载)</option>
              </select>
            </div>

            {selectedScale !== 1.0 && (
              <div style={{ display: 'flex', alignItems: 'center', gap: '0.35rem' }}>
                <span style={{ fontSize: '0.825rem', fontWeight: 600, color: 'var(--color-text-secondary, #64748b)' }}>持续时间：</span>
                <select
                  value={durationHours}
                  onChange={e => setDurationHours(parseFloat(e.target.value))}
                  style={{
                    padding: '0.35rem 0.5rem',
                    borderRadius: '6px',
                    border: '1px solid var(--color-border-primary, var(--border-color, #e2e8f0))',
                    outline: 'none',
                    fontSize: '0.875rem',
                    background: 'var(--color-bg-input, var(--bg-color, #ffffff))',
                    color: 'var(--color-text-primary, var(--text-color, #0f172a))',
                    cursor: 'pointer',
                    height: '32px',
                  }}
                >
                  <option value={0.5}>0.5 小时 (30分钟)</option>
                  <option value={1}>1 小时</option>
                  <option value={2}>2 小时</option>
                  <option value={4}>4 小时</option>
                  <option value={8}>8 小时</option>
                  <option value={12}>12 小时</option>
                  <option value={24}>24 小时</option>
                </select>
              </div>
            )}

            <button
              className="btn"
              disabled={applyingConfig}
              onClick={() => onApplyScale(selectedScale, selectedScale === 1.0 ? 0 : durationHours)}
              style={{
                height: '32px',
                padding: '0 0.75rem',
                fontSize: '0.825rem',
                display: 'flex',
                alignItems: 'center',
                gap: '0.25rem',
                background: selectedScale === 1.0 ? 'transparent' : 'var(--color-primary, #2563eb)',
                color: selectedScale === 1.0 ? 'var(--color-text-primary, var(--text-color, #0f172a))' : '#ffffff',
                border: selectedScale === 1.0 ? '1px solid var(--color-border-primary, var(--border-color, #e2e8f0))' : '1px solid transparent',
              }}
            >
              {applyingConfig ? '应用中...' : selectedScale === 1.0 ? '重置速度' : '应用调节'}
            </button>
          </>
        ) : (
          <span style={{ fontSize: '0.825rem', color: 'var(--color-text-muted, #94a3b8)', fontStyle: 'italic' }}>
            * 仅系统管理员拥有流控调节权限。
          </span>
        )}
      </div>
    </div>
  );
};
