import React from 'react';

export interface TaskReportSummaryInfo {
  status?: string;
  total_chunks?: number;
  processed_chunks?: number;
}

export interface TaskLogSummaryInfo {
  status: string;
  engine_mode?: string;
}

export interface DiagnosticsPhaseInfo {
  name?: string;
  duration_seconds?: number;
  status?: string;
}

export interface DiagnosticsSummaryData {
  is_running?: boolean;
  analysis_duration?: number;
  phase_timers?: Record<string, DiagnosticsPhaseInfo>;
}

export interface PipelineStepperProps {
  log: TaskLogSummaryInfo;
  report?: TaskReportSummaryInfo | null;
  summary?: DiagnosticsSummaryData | null;
  liveSeconds?: number;
}

const formatStepDuration = (seconds: number | null | undefined): string => {
  if (seconds == null || seconds <= 0) return '';
  const s = Math.round(seconds);
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`;
};

export const PipelineStepper: React.FC<PipelineStepperProps> = ({ log, report, summary, liveSeconds = 0 }) => {
  const activeStatus =
    report?.status && report.status !== 'queued' && report.status !== 'pending'
      ? report.status
      : log.status;
  const isDebate = log.engine_mode === 'debate_full';
  const totalChunks = report?.total_chunks ?? 0;
  const processedChunks = report?.processed_chunks ?? 0;

  type StepStatus = 'completed' | 'current' | 'upcoming' | 'failed';

  let step1: StepStatus = 'upcoming'; // 代码准备
  let step2: StepStatus = 'upcoming'; // 语义分片与辩论
  let step3: StepStatus = 'upcoming'; // 规则校准与增量
  let step4: StepStatus = 'upcoming'; // 全仓报告综合
  let step5: StepStatus = 'upcoming'; // 缺陷同步与归档

  if (activeStatus === 'failed') {
    if (log.status === 'cloning' || log.status === 'pre_processing') {
      step1 = 'failed';
    } else if (log.status === 'analyzing') {
      step1 = 'completed';
      step2 = 'failed';
    } else if (log.status === 'synthesis') {
      step1 = 'completed';
      step2 = 'completed';
      step3 = 'completed';
      step4 = 'failed';
    } else {
      step1 = 'completed';
      step2 = 'completed';
      step3 = 'completed';
      step4 = 'completed';
      step5 = 'failed';
    }
  } else if (activeStatus === 'success') {
    step1 = 'completed';
    step2 = 'completed';
    step3 = 'completed';
    step4 = 'completed';
    step5 = 'completed';
  } else if (activeStatus === 'cloning' || activeStatus === 'pre_processing') {
    step1 = 'current';
  } else if (activeStatus === 'analyzing') {
    step1 = 'completed';
    if (totalChunks > 0 && processedChunks >= totalChunks) {
      step2 = 'completed';
      step3 = 'current';
    } else {
      step2 = 'current';
    }
  } else if (activeStatus === 'synthesis') {
    step1 = 'completed';
    step2 = 'completed';
    step3 = 'completed';
    step4 = 'current';
  } else if (activeStatus === 'post_processing' || activeStatus === 'merging') {
    step1 = 'completed';
    step2 = 'completed';
    step3 = 'completed';
    step4 = 'completed';
    step5 = 'current';
  }

  // 提取各阶段实际或实时耗时
  const timers = summary?.phase_timers || {};
  const prepDur = timers.prepare?.duration_seconds ?? 0;
  const anaDur = timers.analysis?.duration_seconds ?? summary?.analysis_duration ?? 0;
  const calDur = timers.calibration?.duration_seconds ?? 0;
  const synDur = timers.synthesis?.duration_seconds ?? 0;
  const arcDur = timers.archive?.duration_seconds ?? 0;

  const getDurationBadge = (status: StepStatus, fixedDur: number) => {
    if (status === 'current') {
      const dur = fixedDur > 0 ? fixedDur : liveSeconds;
      return dur > 0 ? `⚡ ${formatStepDuration(dur)}` : '⚡ 运行中';
    }
    if (status === 'completed' && fixedDur > 0) {
      return `⏱️ ${formatStepDuration(fixedDur)}`;
    }
    if (status === 'failed') {
      return '✕ 异常';
    }
    return '';
  };

  const steps = [
    {
      title: '代码准备',
      desc: '拉取与环境预检',
      status: step1,
      durationBadge: getDurationBadge(step1, prepDur),
    },
    {
      title: isDebate ? '分片对抗辩论' : '分片检视',
      desc: totalChunks > 0 ? `${processedChunks}/${totalChunks} 分片完成` : isDebate ? '三方对抗与仲裁' : '并发静态扫描',
      status: step2,
      durationBadge: getDurationBadge(step2, anaDur),
    },
    {
      title: '校准与增量比对',
      desc: '确定性严重度校准',
      status: step3,
      durationBadge: getDurationBadge(step3, calDur),
    },
    {
      title: 'AI 全仓报告综合',
      desc: isDebate ? 'Tier 4 报告排版生成' : '全仓报告排版生成',
      status: step4,
      durationBadge: getDurationBadge(step4, synDur),
    },
    {
      title: '缺陷同步与归档',
      desc: '状态闭环与通知推送',
      status: step5,
      durationBadge: getDurationBadge(step5, arcDur),
    },
  ];

  return (
    <div
      style={{
        width: '100%',
        marginBottom: '1rem',
        padding: '0.85rem 1.1rem',
        background: 'var(--color-bg-surface, #ffffff)',
        border: '1px solid var(--color-border-primary, #e2e8f0)',
        borderRadius: '8px',
        boxShadow: 'var(--shadow-sm, 0 1px 2px rgba(0,0,0,0.04))',
      }}
    >
      <div
        style={{
          fontSize: '0.8rem',
          fontWeight: 600,
          color: 'var(--color-text-secondary, #64748b)',
          marginBottom: '0.65rem',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
        }}
      >
        <span style={{ display: 'flex', alignItems: 'center', gap: '0.4rem' }}>
          <span>🚦 任务流水线执行进度</span>
          {summary?.is_running && (
            <span
              style={{
                fontSize: '0.7rem',
                color: 'var(--color-primary, #2563eb)',
                background: 'rgba(37, 99, 235, 0.1)',
                padding: '0.1rem 0.45rem',
                borderRadius: '10px',
                fontWeight: 600,
              }}
            >
              实时追踪中
            </span>
          )}
        </span>
        <span style={{ fontSize: '0.75rem', fontWeight: 500, color: 'var(--color-text-muted, #94a3b8)' }}>
          {isDebate ? '多智能体三方对抗架构' : '语义感知分片架构'}
        </span>
      </div>
      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fit, minmax(170px, 1fr))',
          gap: '0.65rem',
        }}
      >
        {steps.map((step, idx) => {
          const isDone = step.status === 'completed';
          const isCurr = step.status === 'current';
          const isFail = step.status === 'failed';

          let iconBg = 'var(--color-bg-muted, #f1f5f9)';
          let iconColor = 'var(--color-text-muted, #94a3b8)';
          let borderColor = 'var(--color-border-primary, #e2e8f0)';
          let titleColor = 'var(--color-text-muted, #94a3b8)';

          if (isDone) {
            iconBg = 'rgba(16, 185, 129, 0.12)';
            iconColor = 'var(--color-success, #10b981)';
            borderColor = 'rgba(16, 185, 129, 0.35)';
            titleColor = 'var(--color-text-primary, #0f172a)';
          } else if (isCurr) {
            iconBg = 'rgba(37, 99, 235, 0.12)';
            iconColor = 'var(--color-primary, #2563eb)';
            borderColor = 'var(--color-primary, #2563eb)';
            titleColor = 'var(--color-primary, #2563eb)';
          } else if (isFail) {
            iconBg = 'rgba(239, 68, 68, 0.12)';
            iconColor = 'var(--color-danger, #ef4444)';
            borderColor = 'rgba(239, 68, 68, 0.35)';
            titleColor = 'var(--color-danger, #ef4444)';
          }

          return (
            <div
              key={idx}
              style={{
                display: 'flex',
                alignItems: 'flex-start',
                gap: '0.55rem',
                padding: '0.5rem 0.65rem',
                borderRadius: '6px',
                background: isCurr ? 'var(--color-bg-muted, rgba(248, 250, 252, 0.8))' : 'transparent',
                border: `1px solid ${borderColor}`,
                boxShadow: isCurr ? '0 0 0 1px var(--color-primary, #2563eb)' : 'none',
                transition: 'all 0.2s ease',
              }}
            >
              <div
                style={{
                  width: '22px',
                  height: '22px',
                  borderRadius: '50%',
                  background: iconBg,
                  color: iconColor,
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  fontSize: '0.72rem',
                  fontWeight: 700,
                  flexShrink: 0,
                  marginTop: '1px',
                }}
              >
                {isDone ? '✓' : isFail ? '✕' : isCurr ? '●' : idx + 1}
              </div>
              <div style={{ display: 'flex', flexDirection: 'column', minWidth: 0, flex: 1 }}>
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '0.2rem' }}>
                  <span style={{ fontSize: '0.78rem', fontWeight: isCurr ? 700 : 600, color: titleColor, lineHeight: 1.3 }}>
                    {step.title}
                  </span>
                  {step.durationBadge && (
                    <span
                      style={{
                        fontSize: '0.68rem',
                        fontWeight: 600,
                        padding: '0.05rem 0.3rem',
                        borderRadius: '3px',
                        background: isCurr ? 'rgba(37, 99, 235, 0.15)' : isDone ? 'rgba(16, 185, 129, 0.12)' : 'transparent',
                        color: isCurr ? 'var(--color-primary, #2563eb)' : isDone ? 'var(--color-success, #10b981)' : 'var(--color-text-muted, #94a3b8)',
                        whiteSpace: 'nowrap',
                      }}
                    >
                      {step.durationBadge}
                    </span>
                  )}
                </div>
                <span style={{ fontSize: '0.7rem', color: 'var(--color-text-muted, #64748b)', marginTop: '2px', lineHeight: 1.2 }}>
                  {step.desc}
                </span>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
};

