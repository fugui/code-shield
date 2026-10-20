import React, { useState, useEffect } from 'react';

export interface ThrottleControlCardProps {
  sysConfig: any;
  isAdmin: boolean;
  onApplyScale: (scale: number | string, durationHours: number) => Promise<void>;
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
  const [selectedScale, setSelectedScale] = useState<number | 'adaptive'>('adaptive');
  const [durationHours, setDurationHours] = useState<number>(2);

  useEffect(() => {
    if (sysConfig) {
      if (sysConfig.is_manual) {
        setSelectedScale(sysConfig.concurrency_scale ?? 1.0);
      } else {
        setSelectedScale('adaptive');
      }
    }
  }, [sysConfig]);

  const isThrottleActive = !!(sysConfig && sysConfig.concurrency_scale !== 1.0);
  const throttleMode: string = sysConfig?.throttle_mode || (isThrottleActive ? 'manual' : 'normal');
  const workHoursCfg = sysConfig?.work_hours_config;
  const adaptiveStatus = sysConfig?.adaptive_status;
  const currentScalePct = Math.round((sysConfig?.concurrency_scale ?? 1.0) * 100);

  // 状态图标背景与前景色定义
  const getIconTheme = () => {
    if (throttleMode === 'adaptive') {
      if (adaptiveStatus?.is_fallback) {
        return { bg: 'rgba(245, 158, 11, 0.12)', color: 'var(--color-warning, #f59e0b)' };
      }
      if (adaptiveStatus?.state === 'overload') {
        return { bg: 'rgba(239, 68, 68, 0.12)', color: 'var(--color-danger, #ef4444)' };
      }
      if (adaptiveStatus?.state === 'warning') {
        return { bg: 'rgba(245, 158, 11, 0.12)', color: 'var(--color-warning, #f59e0b)' };
      }
      if (adaptiveStatus?.state === 'boost') {
        return { bg: 'rgba(16, 185, 129, 0.12)', color: 'var(--color-success, #10b981)' };
      }
      return { bg: 'rgba(59, 130, 246, 0.12)', color: 'var(--color-primary, #2563eb)' };
    }
    if (throttleMode === 'manual') {
      if (sysConfig?.concurrency_scale === 0) {
        return { bg: 'rgba(239, 68, 68, 0.12)', color: 'var(--color-danger, #ef4444)' };
      }
      if (sysConfig?.concurrency_scale < 1) {
        return { bg: 'rgba(245, 158, 11, 0.12)', color: 'var(--color-warning, #f59e0b)' };
      }
      return { bg: 'rgba(16, 185, 129, 0.12)', color: 'var(--color-success, #10b981)' };
    }
    if (throttleMode === 'work_hours') {
      return { bg: 'rgba(245, 158, 11, 0.12)', color: 'var(--color-warning, #f59e0b)' };
    }
    return { bg: 'rgba(59, 130, 246, 0.12)', color: 'var(--color-primary, #2563eb)' };
  };

  const iconTheme = getIconTheme();

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
            background: iconTheme.bg,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            color: iconTheme.color,
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

            {/* 状态徽章 (Status Badge) */}
            {throttleMode === 'adaptive' ? (
              adaptiveStatus?.is_fallback ? (
                <span style={{ fontSize: '0.75rem', padding: '0.15rem 0.4rem', borderRadius: '4px', background: 'rgba(245, 158, 11, 0.12)', color: 'var(--color-warning, #d97706)', fontWeight: 600 }}>
                  ⚠️ 探针离线 (静态保底) {currentScalePct}%
                </span>
              ) : adaptiveStatus?.state === 'boost' ? (
                <span style={{ fontSize: '0.75rem', padding: '0.15rem 0.4rem', borderRadius: '4px', background: 'rgba(16, 185, 129, 0.12)', color: 'var(--color-success, #059669)', fontWeight: 600 }}>
                  🤖 自动增压中 {currentScalePct}%
                </span>
              ) : adaptiveStatus?.state === 'warning' ? (
                <span style={{ fontSize: '0.75rem', padding: '0.15rem 0.4rem', borderRadius: '4px', background: 'rgba(245, 158, 11, 0.12)', color: 'var(--color-warning, #d97706)', fontWeight: 600 }}>
                  ⚠️ 自动避让中 {currentScalePct}%
                </span>
              ) : adaptiveStatus?.state === 'overload' ? (
                <span style={{ fontSize: '0.75rem', padding: '0.15rem 0.4rem', borderRadius: '4px', background: 'rgba(239, 68, 68, 0.12)', color: 'var(--color-danger, #ef4444)', fontWeight: 600 }}>
                  🚨 紧急避让中 {currentScalePct}%
                </span>
              ) : (
                <span style={{ fontSize: '0.75rem', padding: '0.15rem 0.4rem', borderRadius: '4px', background: 'rgba(37, 99, 235, 0.12)', color: 'var(--color-primary, #2563eb)', fontWeight: 600 }}>
                  🤖 自动伸缩中 {currentScalePct}%
                </span>
              )
            ) : throttleMode === 'work_hours' ? (
              sysConfig.concurrency_scale === 0 ? (
                <span style={{ fontSize: '0.75rem', padding: '0.15rem 0.4rem', borderRadius: '4px', background: 'rgba(239, 68, 68, 0.12)', color: 'var(--color-danger, #ef4444)', fontWeight: 600 }}>
                  工作时间暂停中
                </span>
              ) : (
                <span style={{ fontSize: '0.75rem', padding: '0.15rem 0.4rem', borderRadius: '4px', background: 'rgba(245, 158, 11, 0.12)', color: 'var(--color-warning, #d97706)', fontWeight: 600 }}>
                  工作时间限速中 {currentScalePct}%
                </span>
              )
            ) : throttleMode === 'manual' ? (
              sysConfig.concurrency_scale === 0 ? (
                <span style={{ fontSize: '0.75rem', padding: '0.15rem 0.4rem', borderRadius: '4px', background: 'rgba(239, 68, 68, 0.12)', color: 'var(--color-danger, #ef4444)', fontWeight: 600 }}>
                  临时暂停中 (手动)
                </span>
              ) : sysConfig.concurrency_scale < 1 ? (
                <span style={{ fontSize: '0.75rem', padding: '0.15rem 0.4rem', borderRadius: '4px', background: 'rgba(245, 158, 11, 0.12)', color: 'var(--color-warning, #d97706)', fontWeight: 600 }}>
                  手动限速中 {currentScalePct}%
                </span>
              ) : (
                <span style={{ fontSize: '0.75rem', padding: '0.15rem 0.4rem', borderRadius: '4px', background: 'rgba(16, 185, 129, 0.12)', color: 'var(--color-success, #059669)', fontWeight: 600 }}>
                  手动加速中 {currentScalePct}%
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

          {/* 水情透视与运行文案 (Status Description) */}
          <p style={{ margin: 0, fontSize: '0.825rem', color: 'var(--color-text-secondary, #64748b)', lineHeight: '1.3' }}>
            {throttleMode === 'adaptive' ? (
              adaptiveStatus ? (
                adaptiveStatus.is_fallback ? (
                  <>自适应探针服务连续多次无法连通（网络超时或哨兵节点离线），已自动平滑降级为静态兜底模式（100% 并发），确保主扫描流程不受干扰。</>
                ) : (
                  <>
                    当前处于流量自适应模式：哨兵节点（活跃 <strong>{adaptiveStatus.active_requests}</strong>，排队 <strong>{adaptiveStatus.waiting_requests}</strong>），
                    {adaptiveStatus.state === 'overload' ? (
                      <>检测到严重拥塞风险，已紧急断崖避让至 <strong>{currentScalePct}%</strong> 并发速度，抢在 429 前削峰。</>
                    ) : adaptiveStatus.state === 'warning' ? (
                      <>出现轻度排队积压，已温和退让至 <strong>{currentScalePct}%</strong> 并发速度防微杜渐。</>
                    ) : adaptiveStatus.state === 'boost' ? (
                      <>后端算力充沛且平稳，已自动增压至 <strong>{currentScalePct}%</strong> 并发速度高效消化扫描队列。</>
                    ) : (
                      <>推理集群处于黄金平衡水位，当前自动保持为 <strong>{currentScalePct}%</strong> 稳态吞吐。</>
                    )}
                  </>
                )
              ) : (
                '当前处于流量自适应模式，调度器正根据哨兵节点水力负荷实时调节并发槽位。'
              )
            ) : throttleMode === 'work_hours' ? (
              <>
                当前处于工作时间窗口（{workHoursCfg?.start_time || '09:00'} ~ {workHoursCfg?.end_time || '22:00'}），已自动限制并发为 <strong>{currentScalePct}%</strong>，夜间将自动恢复自适应模式。
              </>
            ) : throttleMode === 'manual' ? (
              sysConfig.scale_expires_at ? (
                <>
                  管理员临时调整并发为 <strong>{currentScalePct}%</strong>，预计于 <strong>{new Date(sysConfig.scale_expires_at).toLocaleTimeString()}</strong> 自动恢复自适应模式。
                </>
              ) : (
                <>
                  管理员手动常驻调整并发为 <strong>{currentScalePct}%</strong>。
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
                onChange={e => {
                  const val = e.target.value;
                  setSelectedScale(val === 'adaptive' ? 'adaptive' : parseFloat(val));
                }}
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
                <option value="adaptive">🤖 按流量压力自动伸缩 (默认推荐)</option>
                <option value={0}>0% (临时暂停所有扫描)</option>
                <option value={0.25}>25% (人工强制极度限速)</option>
                <option value={0.5}>50% (人工强制半速运行)</option>
                <option value={0.75}>75% (人工强制微调限速)</option>
                <option value={1.0}>100% (人工强制锁定基准速度)</option>
                <option value={1.5}>150% (人工强制快速运行)</option>
                <option value={2.0}>200% (人工强制双倍加速)</option>
              </select>
            </div>

            {selectedScale !== 'adaptive' && (
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
              disabled={applyingConfig || (!sysConfig?.is_manual && selectedScale === 'adaptive')}
              onClick={() => onApplyScale(selectedScale, selectedScale === 'adaptive' ? 0 : durationHours)}
              style={{
                height: '32px',
                padding: '0 0.75rem',
                fontSize: '0.825rem',
                display: 'flex',
                alignItems: 'center',
                gap: '0.25rem',
                background:
                  selectedScale === 'adaptive' && !sysConfig?.is_manual
                    ? 'var(--color-bg-muted, #f1f5f9)'
                    : 'var(--color-primary, #2563eb)',
                color:
                  selectedScale === 'adaptive' && !sysConfig?.is_manual
                    ? 'var(--color-text-muted, #94a3b8)'
                    : '#ffffff',
                border: '1px solid transparent',
                cursor: applyingConfig || (!sysConfig?.is_manual && selectedScale === 'adaptive') ? 'not-allowed' : 'pointer',
              }}
            >
              {applyingConfig
                ? '应用中...'
                : selectedScale === 'adaptive'
                ? sysConfig?.is_manual
                  ? '恢复自适应'
                  : '自适应运行中'
                : '应用调节'}
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
