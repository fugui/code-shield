import React, { useState } from 'react';

interface DebateVerdictViewProps {
  detail?: string;
  title?: string;
  hunterClaim?: string;
  challengerArg?: string;
  judgeVerdict?: string;
  className?: string;
}

export interface ParsedNumberedItem {
  index: number;
  title?: string;
  content: string;
}

export interface ParsedDebateSection {
  type: 'facts' | 'verification' | 'evidence' | 'reference' | 'defense' | 'mitigations' | 'verdict' | 'other';
  title: string;
  icon: string;
  color: string;
  borderColor: string;
  bgColor: string;
  content: string;
  items?: ParsedNumberedItem[];
}

export interface ParsedDebateResult {
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
            background: 'var(--color-bg-muted, rgba(255, 255, 255, 0.05))',
            color: 'var(--color-primary, #3b82f6)',
            padding: '0.12rem 0.38rem',
            borderRadius: 'var(--radius-xs, 4px)',
            fontFamily: "'JetBrains Mono', 'Fira Code', 'Consolas', monospace",
            fontSize: '0.86em',
            border: '1px solid var(--color-border-primary, rgba(148, 163, 184, 0.2))',
            wordBreak: 'break-all',
          }}
        >
          {part.slice(1, -1)}
        </code>
      );
    }

    // 2. 对非反引号部分，智能识别常见代码符号与标识
    const subParts = part.split(
      /(\b[\w./\\-]+\.[a-zA-Z0-9]+:\d+(?:-\d+)?\b|\b(?:nullptr|NULL|SIGSEGV|ASan|AddressSanitizer|DEADLYSIGNAL|SEGV|EOF|putc_unlocked|_IO_write_ptr|_IO_buf_base|ferror|exit \d+|std::\w+|fmt::\w+|cstring_type)\b|0x[0-9a-fA-F]+)/g
    );

    return (
      <React.Fragment key={index}>
        {subParts.map((sub, sIdx) => {
          if (
            /^\b[\w./\\-]+\.[a-zA-Z0-9]+:\d+(?:-\d+)?\b$/.test(sub) ||
            /^\b(?:nullptr|NULL|SIGSEGV|ASan|AddressSanitizer|DEADLYSIGNAL|SEGV|EOF|putc_unlocked|_IO_write_ptr|_IO_buf_base|ferror|exit \d+|std::\w+|fmt::\w+|cstring_type)\b$/.test(sub) ||
            /^0x[0-9a-fA-F]+$/.test(sub)
          ) {
            return (
              <code
                key={sIdx}
                style={{
                  background: 'var(--color-bg-muted, rgba(255, 255, 255, 0.05))',
                  color: 'var(--color-primary, #3b82f6)',
                  padding: '0.1rem 0.35rem',
                  borderRadius: 'var(--radius-xs, 4px)',
                  fontFamily: "'JetBrains Mono', 'Fira Code', 'Consolas', monospace",
                  fontSize: '0.86em',
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
 * 将长文本中形如 "1. ... \n2. ... " 或 "1、... \n2、..." 的有序列表解析为结构化条目
 */
function parseNumberedItems(text: string): ParsedNumberedItem[] | null {
  if (!text || !text.trim()) return null;

  // 正则匹配每条编号项
  const itemRegex = /(?:^|\n)\s*(\d+)[\.、\)]\s*([^\n]+(?:\n(?!\s*\d+[\.、\)]).*)*)/g;
  const matches: { num: number; raw: string }[] = [];
  let m: RegExpExecArray | null;

  while ((m = itemRegex.exec(text)) !== null) {
    matches.push({
      num: parseInt(m[1], 10),
      raw: m[2].trim(),
    });
  }

  // 至少包含 2 项才视为结构化编号列表
  if (matches.length < 2) {
    return null;
  }

  return matches.map((it) => {
    // 检查是否有小标题，例如 "语法与语义核实：控方指控准确..."
    const titleMatch = it.raw.match(/^([^：:\n]{2,25})[：:]([\s\S]*)$/);
    if (titleMatch) {
      return {
        index: it.num,
        title: titleMatch[1].trim(),
        content: titleMatch[2].trim(),
      };
    }
    return {
      index: it.num,
      content: it.raw,
    };
  });
}

/**
 * 根据标签标题智能推断类型、图标、颜色与左边框高亮
 */
function classifySectionTag(rawTitle: string): {
  type: ParsedDebateSection['type'];
  title: string;
  icon: string;
  color: string;
  borderColor: string;
  bgColor: string;
} {
  const norm = rawTitle.trim();

  // 1. 验证、实测、复现、崩溃、实证、危害
  if (/实测|验证|复现|实证|poc|崩溃|危害|破坏|异常|违例|越界|下溢|溢出|segv|asan/i.test(norm)) {
    return {
      type: 'verification',
      title: norm,
      icon: '🧪',
      color: 'var(--color-danger, #ef4444)',
      borderColor: 'var(--color-danger, #ef4444)',
      bgColor: 'var(--color-danger-subtle, rgba(239, 68, 68, 0.05))',
    };
  }

  // 2. 证据、决定性、证据链、标准、ISO、判例
  if (/证据|决定性|核心|链条|标准|iso|判例|一致性/i.test(norm)) {
    return {
      type: 'evidence',
      title: norm,
      icon: '🎯',
      color: 'var(--color-purple, #a855f7)',
      borderColor: 'var(--color-purple, #a855f7)',
      bgColor: 'var(--color-purple-subtle, rgba(168, 85, 247, 0.05))',
    };
  }

  // 3. 参考、对照、对比、替代
  if (/参考|对照|对比|替代|实现/i.test(norm)) {
    return {
      type: 'reference',
      title: norm,
      icon: '⚖️',
      color: 'var(--color-info, #0ea5e9)',
      borderColor: 'var(--color-info, #0ea5e9)',
      bgColor: 'var(--color-info-subtle, rgba(14, 165, 233, 0.05))',
    };
  }

  // 4. 抗辩、辩护、响应、审理、争鸣
  if (/抗辩|辩护|响应|审理|争鸣|反驳/i.test(norm)) {
    return {
      type: 'defense',
      title: norm,
      icon: '🛡️',
      color: 'var(--color-warning, #f59e0b)',
      borderColor: 'var(--color-warning, #f59e0b)',
      bgColor: 'var(--color-warning-subtle, rgba(245, 158, 11, 0.05))',
    };
  }

  // 5. 缓和、缓解、约束、触发前提、前置条件
  if (/缓和|缓解|约束|触发前提|前提|前置/i.test(norm)) {
    return {
      type: 'mitigations',
      title: norm,
      icon: '🌿',
      color: 'var(--color-success, #10b981)',
      borderColor: 'var(--color-success, #10b981)',
      bgColor: 'var(--color-success-subtle, rgba(16, 185, 129, 0.05))',
    };
  }

  // 6. 事实、源码、代码、成因、分析、认定、调查、假设、架构、定位、规范
  if (/事实|源码|代码|成因|认定|分析|调查|假设|架构|定位|生命周期|状态|机制|契约|规范/i.test(norm)) {
    return {
      type: 'facts',
      title: norm,
      icon: '📌',
      color: 'var(--color-primary, #3b82f6)',
      borderColor: 'var(--color-primary, #3b82f6)',
      bgColor: 'var(--color-primary-subtle, rgba(59, 130, 246, 0.05))',
    };
  }

  // 7. 通用降级
  return {
    type: 'other',
    title: norm,
    icon: '📋',
    color: 'var(--color-text-secondary, #94a3b8)',
    borderColor: 'var(--color-border-primary, #334155)',
    bgColor: 'var(--color-bg-muted, rgba(255, 255, 255, 0.03))',
  };
}

/**
 * 智能通配解析法官裁决与辩论长文本
 */
function parseDebateContent(rawText: string): ParsedDebateResult {
  if (!rawText || !rawText.trim()) {
    return { isDebate: false, intro: '', sections: [], rawText: '' };
  }

  const text = rawText.trim();

  // 判断是否属于结构化法官裁决或辩论流
  const hasJudgeKeywords =
    text.includes('【仲裁法官裁决词】') ||
    text.includes('仲裁法官裁决') ||
    text.includes('【综合裁决】') ||
    text.includes('综合裁决') ||
    text.includes('【事实认定') ||
    text.includes('【源码事实】') ||
    text.includes('【成因与') ||
    text.includes('【实测验证】') ||
    text.includes('【决定性证据】') ||
    text.includes('【抗辩响应】') ||
    /CONFIRMED|CONDITIONAL|CHALLENGE_FAILED|DEFENSE_SUCCESSFUL/i.test(text);

  if (!hasJudgeKeywords) {
    return {
      isDebate: false,
      intro: text,
      sections: [],
      rawText: text,
    };
  }

  // 1. 拆分前导概述与主体内容
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

  // 2. 通用正则匹配所有形如 【XXX】 的小标题标记
  const tagRegex = /【([^】]+)】[：:]?/g;
  interface TagMatch {
    rawTag: string;
    tagName: string;
    startIndex: number;
    endIndex: number;
  }

  const tagMatches: TagMatch[] = [];
  let m: RegExpExecArray | null;

  while ((m = tagRegex.exec(judgeBody)) !== null) {
    tagMatches.push({
      rawTag: m[0],
      tagName: m[1].trim(),
      startIndex: m.index,
      endIndex: m.index + m[0].length,
    });
  }

  let verdictBadge: ParsedDebateResult['verdictBadge'] = undefined;
  let severityLevel: string | undefined = undefined;
  let verdictSummary = '';
  const sections: ParsedDebateSection[] = [];

  // 单字严重度标签白名单（如 【致命】、【严重】、【高危】、【轻微】、【建议】），这些不作为卡片标题，而是定级
  const severityTags = new Set(['致命', '严重', '高危', '中等', '中危', '低危', '低', '轻微', '建议', '提示']);

  if (tagMatches.length === 0) {
    // 没有标签，检查是否可直接从首句或编号列表切分
    const numbered = parseNumberedItems(judgeBody);
    if (numbered) {
      for (const item of numbered) {
        sections.push({
          type: 'facts',
          title: item.title || `分析要点 ${item.index}`,
          icon: '📌',
          color: 'var(--color-primary, #3b82f6)',
          borderColor: 'var(--color-primary, #3b82f6)',
          bgColor: 'var(--color-primary-subtle, rgba(59, 130, 246, 0.05))',
          content: item.content,
        });
      }
    } else {
      verdictSummary = judgeBody;
    }
  } else {
    // 如果首个标签前有文本且无 intro，归入 intro
    if (tagMatches[0].startIndex > 0 && !intro) {
      intro = judgeBody.substring(0, tagMatches[0].startIndex).trim();
    }

    for (let i = 0; i < tagMatches.length; i++) {
      const cur = tagMatches[i];
      const nextStart = i + 1 < tagMatches.length ? tagMatches[i + 1].startIndex : judgeBody.length;
      const content = judgeBody.substring(cur.endIndex, nextStart).trim();

      // 1. 如果是单独出现的严重度标签（如【严重】、【致命】），记录定级
      if (severityTags.has(cur.tagName)) {
        severityLevel = cur.tagName;
        continue;
      }

      // 2. 如果是综合裁决/法官裁决
      if (/综合裁决|终审裁决|裁决结论|法官裁决|裁决结果/i.test(cur.tagName)) {
        // 提取定级
        const sevMatch = content.match(/(?:严重度定级为|初步严重度定级为|初步定级为|严重程度[：:]|定级[：:])([高危中低提示建议轻微严重致命]+)/);
        if (sevMatch) {
          severityLevel = sevMatch[1];
        }

        // 提取裁决徽章状态
        if (/CONFIRMED|缺陷事实成立|成立/i.test(content)) {
          verdictBadge = {
            status: 'CONFIRMED',
            label: '缺陷事实成立 (CONFIRMED)',
            color: 'var(--color-success, #10b981)',
            bg: 'var(--color-success-subtle, rgba(16, 185, 129, 0.1))',
            border: 'var(--color-success-border, rgba(16, 185, 129, 0.3))',
          };
        } else if (/CONDITIONAL|条件触发|条件成立/i.test(content)) {
          verdictBadge = {
            status: 'CONDITIONAL',
            label: '条件触发 (CONDITIONAL)',
            color: 'var(--color-warning, #f59e0b)',
            bg: 'var(--color-warning-subtle, rgba(245, 158, 11, 0.1))',
            border: 'var(--color-warning-border, rgba(245, 158, 11, 0.3))',
          };
        } else if (/REJECTED|误报|驳回|CHALLENGE_FAILED/i.test(content)) {
          verdictBadge = {
            status: 'REJECTED',
            label: '误报驳回 (REJECTED)',
            color: 'var(--color-danger, #ef4444)',
            bg: 'var(--color-danger-subtle, rgba(239, 68, 68, 0.1))',
            border: 'var(--color-danger-border, rgba(239, 68, 68, 0.3))',
          };
        }

        // 检查裁决内容后是否直接紧跟编号列表（如 Item 0: 没有二级标签，全写在综合裁决里）
        const numbered = parseNumberedItems(content);
        if (numbered) {
          // 将编号列表前的一句话作为裁决 summary
          const firstNumIndex = content.search(/(?:^|\n)\s*1[\.、\)]/);
          if (firstNumIndex > 0) {
            verdictSummary = content.substring(0, firstNumIndex).trim();
          } else {
            verdictSummary = content.split('\n')[0].trim();
          }

          // 将编号列表提取为独立的分析卡片
          for (const item of numbered) {
            const classInfo = classifySectionTag(item.title || `分析要点 ${item.index}`);
            sections.push({
              type: classInfo.type,
              title: item.title ? `${item.index}. ${item.title}` : `要点 ${item.index}`,
              icon: classInfo.icon,
              color: classInfo.color,
              borderColor: classInfo.borderColor,
              bgColor: classInfo.bgColor,
              content: item.content,
            });
          }
        } else {
          verdictSummary = content;
        }
      } else {
        // 3. 其他所有业务标签（【事实认定与分析】、【源码事实】、【实测验证】等），全量解析为独立卡片
        const classInfo = classifySectionTag(cur.tagName);
        const numbered = parseNumberedItems(content);

        sections.push({
          type: classInfo.type,
          title: classInfo.title,
          icon: classInfo.icon,
          color: classInfo.color,
          borderColor: classInfo.borderColor,
          bgColor: classInfo.bgColor,
          content,
          items: numbered || undefined,
        });
      }
    }
  }

  // 默认 verdict badge 兜底
  if (!verdictBadge) {
    if (/CONFIRMED/i.test(judgeBody)) {
      verdictBadge = {
        status: 'CONFIRMED',
        label: '缺陷事实成立 (CONFIRMED)',
        color: 'var(--color-success, #10b981)',
        bg: 'var(--color-success-subtle, rgba(16, 185, 129, 0.1))',
        border: 'var(--color-success-border, rgba(16, 185, 129, 0.3))',
      };
    } else if (/CONDITIONAL/i.test(judgeBody)) {
      verdictBadge = {
        status: 'CONDITIONAL',
        label: '条件触发 (CONDITIONAL)',
        color: 'var(--color-warning, #f59e0b)',
        bg: 'var(--color-warning-subtle, rgba(245, 158, 11, 0.1))',
        border: 'var(--color-warning-border, rgba(245, 158, 11, 0.3))',
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
            color: 'var(--color-text-primary, #f8fafc)',
            textAlign: 'left',
            lineHeight: 1.6,
            background: 'var(--color-bg-surface, #1e293b)',
            border: '1px solid var(--color-border-primary, #334155)',
            padding: '1rem',
            borderRadius: 'var(--radius-md, 8px)',
            whiteSpace: 'pre-wrap',
          }}
        >
          {renderInlineFormattedText(detail || judgeVerdict || '')}
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
      {/* 1. 顶部初筛概述 (若存在且与标题不同) */}
      {parsed.intro && parsed.intro !== title && (
        <div
          style={{
            fontSize: '0.85rem',
            color: 'var(--color-text-secondary, #94a3b8)',
            lineHeight: 1.55,
            padding: '0.65rem 0.9rem',
            background: 'var(--color-bg-muted, rgba(255, 255, 255, 0.03))',
            borderRadius: 'var(--radius-sm, 6px)',
            border: '1px solid var(--color-border-primary, #334155)',
          }}
        >
          <span style={{ fontWeight: 600, color: 'var(--color-text-primary, #f8fafc)', marginRight: '0.4rem' }}>
            📋 问题概述:
          </span>
          {renderInlineFormattedText(parsed.intro)}
        </div>
      )}

      {/* 2. 仲裁法官终审裁决 Banner */}
      <div
        style={{
          background: parsed.verdictBadge?.bg || 'var(--color-success-subtle, rgba(16, 185, 129, 0.06))',
          border: `1.5px solid ${parsed.verdictBadge?.border || 'var(--color-success-border, rgba(16, 185, 129, 0.3))'}`,
          borderRadius: 'var(--radius-md, 8px)',
          padding: '0.85rem 1rem',
          display: 'flex',
          flexDirection: 'column',
          gap: '0.45rem',
          boxShadow: 'var(--shadow-sm, 0 1px 3px rgba(0, 0, 0, 0.1))',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: '0.5rem' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <span
              style={{
                fontSize: '0.8rem',
                fontWeight: 700,
                color: parsed.verdictBadge?.color || 'var(--color-success, #10b981)',
                background: 'var(--color-bg-surface, #1e293b)',
                border: `1px solid ${parsed.verdictBadge?.border || 'var(--color-success, #10b981)'}`,
                padding: '0.2rem 0.65rem',
                borderRadius: 'var(--radius-xs, 4px)',
                display: 'inline-flex',
                alignItems: 'center',
                gap: '0.3rem',
              }}
            >
              ⚖️ {parsed.verdictBadge?.label || '法官终审裁决'}
            </span>
            {parsed.severityLevel && (
              <span
                style={{
                  fontSize: '0.75rem',
                  fontWeight: 600,
                  color: 'var(--color-warning, #f59e0b)',
                  background: 'var(--color-warning-subtle, rgba(245, 158, 11, 0.12))',
                  border: '1px solid var(--color-warning-border, rgba(245, 158, 11, 0.3))',
                  padding: '0.15rem 0.5rem',
                  borderRadius: 'var(--radius-xs, 4px)',
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
              fontSize: '0.75rem',
              color: 'var(--color-text-muted, #64748b)',
              background: 'transparent',
              border: 'none',
              cursor: 'pointer',
              padding: '2px 6px',
              borderRadius: '4px',
              transition: 'all 0.2s',
            }}
          >
            📄 查看纯文本
          </button>
        </div>

        {parsed.verdictSummary && (
          <div
            style={{
              fontSize: '0.88rem',
              fontWeight: 500,
              color: 'var(--color-text-primary, #f8fafc)',
              lineHeight: 1.6,
              whiteSpace: 'pre-wrap',
            }}
          >
            {renderInlineFormattedText(parsed.verdictSummary)}
          </div>
        )}
      </div>

      {/* 3. 结构化事实与证据链条卡片流 (Sections Stream) */}
      {parsed.sections.length > 0 && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.65rem' }}>
          {parsed.sections.map((section, idx) => {
            return (
              <div
                key={idx}
                style={{
                  padding: '0.75rem 1rem',
                  background: 'var(--color-bg-surface, #1e293b)',
                  border: '1px solid var(--color-border-primary, #334155)',
                  borderLeft: `4px solid ${section.borderColor}`,
                  borderRadius: 'var(--radius-md, 8px)',
                  fontSize: '0.85rem',
                  lineHeight: 1.6,
                  color: 'var(--color-text-primary, #f8fafc)',
                  boxShadow: 'var(--shadow-sm, 0 1px 3px rgba(0, 0, 0, 0.1))',
                }}
              >
                {/* 卡片头部标题 */}
                <div
                  style={{
                    fontWeight: 700,
                    color: section.color,
                    marginBottom: '0.45rem',
                    display: 'flex',
                    alignItems: 'center',
                    gap: '0.4rem',
                    fontSize: '0.84rem',
                  }}
                >
                  <span style={{ fontSize: '0.95rem' }}>{section.icon}</span>
                  <span>{section.title}</span>
                </div>

                {/* 卡片正文：若含有编号条目，以结构化列表呈现 */}
                {section.items && section.items.length > 0 ? (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem', marginTop: '0.35rem' }}>
                    {section.items.map((item, iIdx) => (
                      <div
                        key={iIdx}
                        style={{
                          display: 'flex',
                          alignItems: 'flex-start',
                          gap: '0.55rem',
                          background: 'var(--color-bg-muted, rgba(255, 255, 255, 0.02))',
                          padding: '0.45rem 0.65rem',
                          borderRadius: 'var(--radius-sm, 6px)',
                          border: '1px solid var(--color-border-subtle, rgba(255, 255, 255, 0.04))',
                        }}
                      >
                        {/* 序号徽章 */}
                        <span
                          style={{
                            flexShrink: 0,
                            minWidth: '20px',
                            height: '20px',
                            borderRadius: '4px',
                            background: section.bgColor,
                            color: section.color,
                            border: `1px solid ${section.borderColor}`,
                            fontSize: '0.72rem',
                            fontWeight: 700,
                            display: 'inline-flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                            marginTop: '0.15rem',
                          }}
                        >
                          {item.index}
                        </span>

                        {/* 条目正文 */}
                        <div style={{ flex: 1, fontSize: '0.84rem', lineHeight: 1.55 }}>
                          {item.title && (
                            <strong style={{ color: section.color, marginRight: '0.4rem' }}>
                              {item.title}：
                            </strong>
                          )}
                          <span style={{ color: 'var(--color-text-primary, #f8fafc)', whiteSpace: 'pre-wrap' }}>
                            {renderInlineFormattedText(item.content)}
                          </span>
                        </div>
                      </div>
                    ))}
                  </div>
                ) : (
                  <div
                    style={{
                      whiteSpace: 'pre-wrap',
                      color: 'var(--color-text-primary, #f8fafc)',
                      fontSize: '0.84rem',
                      lineHeight: 1.6,
                    }}
                  >
                    {renderInlineFormattedText(section.content)}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}

      {/* 4. 原生三方对抗独立数据补充 (若存在单独字段) */}
      {(hunterClaim || challengerArg) && (
        <div
          style={{
            marginTop: '0.25rem',
            padding: '0.65rem 0.9rem',
            background: 'var(--color-bg-muted, rgba(255, 255, 255, 0.02))',
            border: '1px dashed var(--color-border-primary, #334155)',
            borderRadius: 'var(--radius-md, 8px)',
            fontSize: '0.82rem',
            display: 'flex',
            flexDirection: 'column',
            gap: '0.45rem',
          }}
        >
          {hunterClaim && (
            <div>
              <strong style={{ color: 'var(--color-danger, #ef4444)', marginRight: '0.35rem' }}>
                🎯 初筛猎手主张:
              </strong>
              <span style={{ color: 'var(--color-text-secondary, #94a3b8)', whiteSpace: 'pre-wrap' }}>
                {renderInlineFormattedText(hunterClaim)}
              </span>
            </div>
          )}
          {challengerArg && (
            <div>
              <strong style={{ color: 'var(--color-primary, #3b82f6)', marginRight: '0.35rem' }}>
                ⚖️ 辩护对抗证据:
              </strong>
              <span style={{ color: 'var(--color-text-secondary, #94a3b8)', whiteSpace: 'pre-wrap' }}>
                {renderInlineFormattedText(challengerArg)}
              </span>
            </div>
          )}
        </div>
      )}
    </div>
  );
};

export default DebateVerdictView;
