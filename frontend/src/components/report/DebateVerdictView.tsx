import React, { useState } from 'react';

interface DebateVerdictViewProps {
  detail?: string;
  title?: string;
  hunterClaim?: string;
  challengerArg?: string;
  judgeVerdict?: string;
  className?: string;
}

interface ParsedDebateSection {
  type: 'facts' | 'verification' | 'evidence' | 'reference' | 'defense' | 'mitigations' | 'verdict' | 'other';
  title: string;
  icon: string;
  color: string;
  borderColor: string;
  bgColor: string;
  content: string;
}

interface ParsedDebateResult {
  isDebate: boolean;
  intro: string;
  verdictBadge?: {
    status: 'CONFIRMED' | 'CONDITIONAL' | 'REJECTED' | 'UNKNOWN';
    label: string;
    color: string;
    bg: string;
    border: string;
  };
  severityLevel?: string;
  verdictSummary?: string;
  sections: ParsedDebateSection[];
  rawText: string;
}

/**
 * 智能解析行内代码（反引号、代码文件定位、C++符号、系统错误码等）
 */
function renderInlineFormattedText(text: string): React.ReactNode {
  if (!text) return null;

  // 1. 如果已有反引号 `code`，优先按反引号拆分
  const parts = text.split(/(`[^`]+`)/g);

  return parts.map((part, index) => {
    if (part.startsWith('`') && part.endsWith('`') && part.length > 2) {
      return (
        <code
          key={index}
          style={{
            background: 'var(--color-bg-muted, rgba(15, 23, 42, 0.06))',
            color: 'var(--color-primary, #2563eb)',
            padding: '0.12rem 0.35rem',
            borderRadius: '4px',
            fontFamily: "'JetBrains Mono', 'Fira Code', 'Consolas', monospace",
            fontSize: '0.85em',
            border: '1px solid var(--color-border-primary, rgba(148, 163, 184, 0.2))',
            wordBreak: 'break-all',
          }}
        >
          {part.slice(1, -1)}
        </code>
      );
    }

    // 2. 对非反引号部分，智能识别 文件名:行号、std::、nullptr 等常见标识
    const subParts = part.split(/(\b[\w./\\-]+\.[a-zA-Z0-9]+:\d+(?:-\d+)?\b|\b(?:nullptr|NULL|SIGSEGV|ASan|exit \d+|std::\w+|fmt::\w+|cstring_type)\b)/g);

    return (
      <React.Fragment key={index}>
        {subParts.map((sub, sIdx) => {
          if (
            /^\b[\w./\\-]+\.[a-zA-Z0-9]+:\d+(?:-\d+)?\b$/.test(sub) ||
            /^\b(?:nullptr|NULL|SIGSEGV|ASan|exit \d+|std::\w+|fmt::\w+|cstring_type)\b$/.test(sub)
          ) {
            return (
              <code
                key={sIdx}
                style={{
                  background: 'var(--color-bg-muted, rgba(15, 23, 42, 0.06))',
                  color: 'var(--color-primary, #2563eb)',
                  padding: '0.1rem 0.3rem',
                  borderRadius: '3px',
                  fontFamily: "'JetBrains Mono', 'Fira Code', 'Consolas', monospace",
                  fontSize: '0.85em',
                  fontWeight: 500,
                  border: '1px solid var(--color-border-primary, rgba(148, 163, 184, 0.15))',
                }}
              >
                {sub}
              </code>
            );
          }
          return sub;
        })}
      </React.Fragment>
    );
  });
}

/**
 * 智能解析法官裁决与辩论长文本
 */
function parseDebateContent(rawText: string): ParsedDebateResult {
  if (!rawText || !rawText.trim()) {
    return { isDebate: false, intro: '', sections: [], rawText: '' };
  }

  const text = rawText.trim();

  // 判断是否具备法官裁决或结构化语义标记
  const hasJudgeKeywords =
    text.includes('【仲裁法官裁决词】') ||
    text.includes('仲裁法官裁决') ||
    text.includes('综合裁决') ||
    text.includes('源码事实') ||
    text.includes('实测验证') ||
    text.includes('决定性证据') ||
    text.includes('对照参考实现') ||
    text.includes('缓和因素');

  if (!hasJudgeKeywords) {
    return {
      isDebate: false,
      intro: text,
      sections: [],
      rawText: text,
    };
  }

  // 1. 拆分前导概述与裁决正文
  let intro = '';
  let judgeBody = text;

  const judgeIndex = text.indexOf('【仲裁法官裁决词】');
  if (judgeIndex !== -1) {
    intro = text.substring(0, judgeIndex).trim();
    judgeBody = text.substring(judgeIndex + '【仲裁法官裁决词】'.length).replace(/^[：:\s]+/, '');
  } else {
    const altIndex = text.indexOf('仲裁法官裁决词');
    if (altIndex !== -1 && altIndex < 100) {
      intro = text.substring(0, altIndex).trim();
      judgeBody = text.substring(altIndex + '仲裁法官裁决词'.length).replace(/^[：:\s]+/, '');
    }
  }

  // 2. 定义识别锚点
  const anchors: {
    type: ParsedDebateSection['type'];
    title: string;
    icon: string;
    color: string;
    borderColor: string;
    bgColor: string;
    pattern: RegExp;
  }[] = [
    {
      type: 'facts',
      title: '源码事实',
      icon: '📌',
      color: '#2563eb',
      borderColor: '#3b82f6',
      bgColor: 'rgba(59, 130, 246, 0.04)',
      pattern: /(?:【?(?:源码事实|代码事实|事实依据|事实调查)】?[：:]|源码事实[：:])/g,
    },
    {
      type: 'verification',
      title: '实测验证',
      icon: '🧪',
      color: '#dc2626',
      borderColor: '#ef4444',
      bgColor: 'rgba(239, 68, 68, 0.04)',
      pattern: /(?:【?(?:实测验证|复现验证|实测分析|PoC验证)】?[：:]|实测验证[：:])/g,
    },
    {
      type: 'evidence',
      title: '决定性证据',
      icon: '🎯',
      color: '#7c3aed',
      borderColor: '#8b5cf6',
      bgColor: 'rgba(139, 92, 246, 0.04)',
      pattern: /(?:【?(?:决定性证据|关键证据|核心证据|证据链条?)】?[：:]|决定性证据[：:])/g,
    },
    {
      type: 'reference',
      title: '对照参考实现',
      icon: '⚖️',
      color: '#0284c7',
      borderColor: '#0ea5e9',
      bgColor: 'rgba(14, 165, 233, 0.04)',
      pattern: /(?:【?(?:对照参考实现|对照参考|参考实现|对照实现)】?[：:]|对照参考实现[：:])/g,
    },
    {
      type: 'defense',
      title: '抗辩审理',
      icon: '🛡️',
      color: '#d97706',
      borderColor: '#f59e0b',
      bgColor: 'rgba(245, 158, 11, 0.04)',
      pattern: /(?:【?(?:抗辩分析|抗辩响应|辩护抗辩|抗辩判定)】?[：:]|抗辩分析[：:]|抗辩响应[：:]|抗辩中)/g,
    },
    {
      type: 'mitigations',
      title: '缓和与触发约束',
      icon: '🌿',
      color: '#059669',
      borderColor: '#10b981',
      bgColor: 'rgba(16, 185, 129, 0.04)',
      pattern: /(?:【?(?:缓和因素|缓和因素成立|缓解因素|触发前提|约束条件)】?[：:]|缓和因素[：:]|缓和因素成立[：:])/g,
    },
    {
      type: 'verdict',
      title: '综合裁决',
      icon: '🏛️',
      color: '#0f766e',
      borderColor: '#14b8a6',
      bgColor: 'rgba(20, 184, 166, 0.04)',
      pattern: /(?:【?(?:综合裁决|终审裁决|裁决结论|法官裁决|裁决结果)】?[：:]|综合裁决[：:])/g,
    },
  ];

  // 3. 收集所有匹配标记点的位置
  interface MatchItem {
    index: number;
    length: number;
    anchor: (typeof anchors)[0];
  }

  const matches: MatchItem[] = [];

  for (const anchor of anchors) {
    anchor.pattern.lastIndex = 0;
    let m: RegExpExecArray | null;
    while ((m = anchor.pattern.exec(judgeBody)) !== null) {
      matches.push({
        index: m.index,
        length: m[0].length,
        anchor,
      });
    }
  }

  // 按出现顺序升序排序
  matches.sort((a, b) => a.index - b.index);

  if (matches.length === 0) {
    return {
      isDebate: true,
      intro,
      verdictSummary: judgeBody,
      sections: [],
      rawText: text,
    };
  }

  const sections: ParsedDebateSection[] = [];
  let verdictSummary = '';
  let verdictBadge: ParsedDebateResult['verdictBadge'] = undefined;
  let severityLevel: string | undefined = undefined;

  // 如果在第一个匹配项前还有前置内容且 intro 为空，归入 intro
  if (matches[0].index > 0 && !intro) {
    intro = judgeBody.substring(0, matches[0].index).trim();
  }

  for (let i = 0; i < matches.length; i++) {
    const cur = matches[i];
    const nextIndex = i + 1 < matches.length ? matches[i + 1].index : judgeBody.length;
    const content = judgeBody.substring(cur.index + cur.length, nextIndex).trim();

    if (cur.anchor.type === 'verdict') {
      verdictSummary = content;

      // 提取 verdict 状态
      if (/CONFIRMED|缺陷事实成立|成立/i.test(content)) {
        verdictBadge = {
          status: 'CONFIRMED',
          label: '缺陷事实成立 (CONFIRMED)',
          color: '#059669',
          bg: 'rgba(16, 185, 129, 0.1)',
          border: 'rgba(16, 185, 129, 0.3)',
        };
      } else if (/CONDITIONAL|条件触发/i.test(content)) {
        verdictBadge = {
          status: 'CONDITIONAL',
          label: '条件触发 (CONDITIONAL)',
          color: '#d97706',
          bg: 'rgba(245, 158, 11, 0.1)',
          border: 'rgba(245, 158, 11, 0.3)',
        };
      } else if (/REJECTED|驳回|误报/i.test(content)) {
        verdictBadge = {
          status: 'REJECTED',
          label: '误报驳回 (REJECTED)',
          color: '#dc2626',
          bg: 'rgba(239, 68, 68, 0.1)',
          border: 'rgba(239, 68, 68, 0.3)',
        };
      }

      // 提取定级
      const sevMatch = content.match(/(?:严重度定级为|初步严重度定级为|严重程度[：:]|定级[：:])([高危中低提示]+)/);
      if (sevMatch) {
        severityLevel = sevMatch[1];
      }
    } else {
      sections.push({
        type: cur.anchor.type,
        title: cur.anchor.title,
        icon: cur.anchor.icon,
        color: cur.anchor.color,
        borderColor: cur.anchor.borderColor,
        bgColor: cur.anchor.bgColor,
        content,
      });
    }
  }

  // 默认 verdict badge 兜底检测
  if (!verdictBadge) {
    if (/CONFIRMED/i.test(judgeBody)) {
      verdictBadge = {
        status: 'CONFIRMED',
        label: '裁决成立 (CONFIRMED)',
        color: '#059669',
        bg: 'rgba(16, 185, 129, 0.1)',
        border: 'rgba(16, 185, 129, 0.3)',
      };
    } else if (/CONDITIONAL/i.test(judgeBody)) {
      verdictBadge = {
        status: 'CONDITIONAL',
        label: '条件触发 (CONDITIONAL)',
        color: '#d97706',
        bg: 'rgba(245, 158, 11, 0.1)',
        border: 'rgba(245, 158, 11, 0.3)',
      };
    }
  }

  return {
    isDebate: true,
    intro,
    verdictBadge,
    severityLevel,
    verdictSummary,
    sections,
    rawText: text,
  };
}

export const DebateVerdictView: React.FC<DebateVerdictViewProps> = ({
  detail = '',
  title,
  hunterClaim,
  challengerArg,
  judgeVerdict,
  className,
}) => {
  const [showRaw, setShowRaw] = useState(false);
  const parsed = parseDebateContent(detail || judgeVerdict || '');

  if (!detail && !judgeVerdict) {
    return null;
  }

  // 如果不是法官辩论格式，或者用户点击了切换纯文本
  if (!parsed.isDebate || showRaw) {
    return (
      <div className={className} style={{ position: 'relative' }}>
        <div
          style={{
            margin: 0,
            fontSize: '0.85rem',
            color: 'var(--color-text-primary, var(--text-color, #1e293b))',
            textAlign: 'left',
            lineHeight: 1.6,
            background: 'var(--color-bg-surface, var(--card-bg, #ffffff))',
            border: '1px solid var(--color-border-primary, var(--border-color, #e2e8f0))',
            padding: '1rem',
            borderRadius: '8px',
            whiteSpace: 'pre-wrap',
          }}
        >
          {renderInlineFormattedText(detail)}
        </div>
        {parsed.isDebate && (
          <button
            type="button"
            onClick={() => setShowRaw(false)}
            style={{
              marginTop: '0.5rem',
              fontSize: '0.75rem',
              color: 'var(--color-primary, #3b82f6)',
              background: 'transparent',
              border: 'none',
              cursor: 'pointer',
              padding: 0,
              display: 'inline-flex',
              alignItems: 'center',
              gap: '4px',
            }}
          >
            ✨ 切换回结构化卡片视图
          </button>
        )}
      </div>
    );
  }

  return (
    <div
      className={className}
      style={{
        display: 'flex',
        flexDirection: 'column',
        gap: '0.75rem',
        textAlign: 'left',
      }}
    >
      {/* 1. 顶部初筛概述 (若存在且与裁决不同) */}
      {parsed.intro && parsed.intro !== title && (
        <div
          style={{
            fontSize: '0.85rem',
            color: 'var(--color-text-secondary, #475569)',
            lineHeight: 1.5,
            padding: '0.6rem 0.85rem',
            background: 'var(--color-bg-muted, rgba(241, 245, 249, 0.6))',
            borderRadius: '6px',
            border: '1px solid var(--color-border-primary, rgba(226, 232, 240, 0.8))',
          }}
        >
          <span style={{ fontWeight: 600, color: 'var(--color-text-primary, #0f172a)', marginRight: '0.4rem' }}>
            📋 问题概述:
          </span>
          {renderInlineFormattedText(parsed.intro)}
        </div>
      )}

      {/* 2. 仲裁法官终审裁决 Banner */}
      <div
        style={{
          background: parsed.verdictBadge?.bg || 'rgba(16, 185, 129, 0.06)',
          border: `1.5px solid ${parsed.verdictBadge?.border || 'rgba(16, 185, 129, 0.25)'}`,
          borderRadius: '8px',
          padding: '0.75rem 1rem',
          display: 'flex',
          flexDirection: 'column',
          gap: '0.45rem',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: '0.5rem' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <span
              style={{
                fontSize: '0.78rem',
                fontWeight: 700,
                color: parsed.verdictBadge?.color || '#059669',
                background: 'var(--color-bg-surface, #ffffff)',
                border: `1px solid ${parsed.verdictBadge?.border || '#10b981'}`,
                padding: '0.2rem 0.6rem',
                borderRadius: '4px',
                display: 'inline-flex',
                alignItems: 'center',
                gap: '0.25rem',
              }}
            >
              ⚖️ {parsed.verdictBadge?.label || '法官终审裁决'}
            </span>
            {parsed.severityLevel && (
              <span
                style={{
                  fontSize: '0.75rem',
                  fontWeight: 600,
                  color: '#d97706',
                  background: 'rgba(245, 158, 11, 0.12)',
                  border: '1px solid rgba(245, 158, 11, 0.3)',
                  padding: '0.15rem 0.5rem',
                  borderRadius: '4px',
                }}
              >
                定级：{parsed.severityLevel}
              </span>
            )}
          </div>

          <button
            type="button"
            onClick={() => setShowRaw(true)}
            title="查看纯文本格式"
            style={{
              fontSize: '0.72rem',
              color: 'var(--color-text-muted, #94a3b8)',
              background: 'transparent',
              border: 'none',
              cursor: 'pointer',
              padding: '2px 4px',
            }}
          >
            📄 查看纯文本
          </button>
        </div>

        {parsed.verdictSummary && (
          <div
            style={{
              fontSize: '0.86rem',
              fontWeight: 500,
              color: 'var(--color-text-primary, #1e293b)',
              lineHeight: 1.55,
            }}
          >
            {renderInlineFormattedText(parsed.verdictSummary)}
          </div>
        )}
      </div>

      {/* 3. 事实与证据链条 (Facts & Evidences Stream) */}
      {parsed.sections.length > 0 && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.55rem' }}>
          {parsed.sections.map((section, idx) => {
            const isMitigation = section.type === 'mitigations' || section.type === 'defense';
            return (
              <div
                key={idx}
                style={{
                  padding: '0.65rem 0.9rem',
                  background: isMitigation ? 'var(--color-bg-muted, rgba(248, 250, 252, 0.8))' : 'var(--color-bg-surface, #ffffff)',
                  border: `1px solid var(--color-border-primary, rgba(226, 232, 240, 0.8))`,
                  borderLeft: `4px solid ${section.borderColor}`,
                  borderRadius: '6px',
                  fontSize: '0.84rem',
                  lineHeight: 1.55,
                  color: 'var(--color-text-primary, #334155)',
                }}
              >
                <div
                  style={{
                    fontWeight: 700,
                    color: section.color,
                    marginBottom: '0.25rem',
                    display: 'flex',
                    alignItems: 'center',
                    gap: '0.35rem',
                    fontSize: '0.82rem',
                  }}
                >
                  <span>{section.icon}</span>
                  <span>{section.title}</span>
                </div>
                <div>{renderInlineFormattedText(section.content)}</div>
              </div>
            );
          })}
        </div>
      )}

      {/* 4. 原生三方对抗独立数据补充 (若存在单独字段) */}
      {(hunterClaim || challengerArg) && (
        <div
          style={{
            marginTop: '0.2rem',
            padding: '0.6rem 0.85rem',
            background: 'var(--color-bg-muted, #f8fafc)',
            border: '1px dashed var(--color-border-primary, #cbd5e1)',
            borderRadius: '6px',
            fontSize: '0.8rem',
            display: 'flex',
            flexDirection: 'column',
            gap: '0.4rem',
          }}
        >
          {hunterClaim && (
            <div>
              <strong style={{ color: '#dc2626' }}>🎯 初筛猎手主张:</strong> {renderInlineFormattedText(hunterClaim)}
            </div>
          )}
          {challengerArg && (
            <div>
              <strong style={{ color: '#2563eb' }}>⚖️ 辩护对抗证据:</strong> {renderInlineFormattedText(challengerArg)}
            </div>
          )}
        </div>
      )}
    </div>
  );
};

export default DebateVerdictView;
