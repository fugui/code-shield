import React, { useState } from 'react';

interface DebateVerdictViewProps {
  detail?: string;
  title?: string;
  hunterClaim?: string;
  challengerArg?: string;
  judgeVerdict?: string;
  triggerLine?: string;
  scopeSymbol?: string;
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
  // eslint-disable-next-line no-useless-escape
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
    // 检查是否有小标题，例如 "编译期格式串路径的安全设计（辩护成立部分）：..."
    const titleMatch = it.raw.match(/^([^：:\n]{2,50})[：:]([\s\S]*)$/);
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

  // 6. 事实、源码、代码、成因、分析、认定、调查、假设、架构、定位、规范、裁定、结论
  if (/事实|源码|代码|成因|认定|分析|调查|假设|架构|定位|生命周期|状态|机制|契约|规范|裁定|裁决|结论|判决|定性/i.test(norm)) {
    return {
      type: 'facts',
      title: norm,
      icon: '📜',
      color: 'var(--color-primary, #3b82f6)',
      borderColor: 'var(--color-primary, #3b82f6)',
      bgColor: 'var(--color-primary-subtle, rgba(59, 130, 246, 0.05))',
    };
  }

  // 7. 修复、建议、整改、防护、加固
  if (/修复|建议|整改|防护|加固|改法|优化/i.test(norm)) {
    return {
      type: 'mitigations',
      title: norm,
      icon: '💡',
      color: 'var(--color-success, #10b981)',
      borderColor: 'var(--color-success, #10b981)',
      bgColor: 'var(--color-success-subtle, rgba(16, 185, 129, 0.05))',
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

const NON_SECTION_TAGS = new Set([
  '致命',
  '严重',
  '高危',
  '中等',
  '中危',
  '低危',
  '低',
  '轻微',
  '建议',
  '提示',
  'CONFIRMED',
  'CONDITIONAL',
  'REJECTED',
  'CHALLENGE_FAILED',
  'DEFENSE_SUCCESSFUL',
]);

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
    text.includes('【仲裁法官裁判词】') ||
    text.includes('仲裁法官裁决') ||
    text.includes('仲裁法官裁判') ||
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
    const numbered = parseNumberedItems(text);
    const sections: ParsedDebateSection[] = [];
    let summary = text;
    if (numbered) {
      // eslint-disable-next-line no-useless-escape
      const firstNumIndex = text.search(/(?:^|\n)\s*1[\.、\)]/);
      if (firstNumIndex > 0) {
        summary = text.substring(0, firstNumIndex).trim();
      } else {
        summary = text.split('\n')[0].trim();
      }
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
    }
    return {
      isDebate: false,
      intro: '',
      verdictSummary: summary,
      sections,
      rawText: text,
    };
  }

  // 1. 拆分前导概述与主体内容
  let intro = '';
  let judgeBody = text;

  const judgePrefixRegex = /(?:^|\n)\s*【?(?:仲裁法官裁决词|仲裁法官裁判词|仲裁法官裁决|仲裁法官裁判|法官裁决词|法官裁判词|法官裁决|法官裁判)】?[：:\s]*/;
  const judgeMatch = text.match(judgePrefixRegex);
  if (judgeMatch && judgeMatch.index !== undefined) {
    intro = text.substring(0, judgeMatch.index).trim();
    judgeBody = text.substring(judgeMatch.index + judgeMatch[0].length).trim();
  }

  // 提前从正文中精准提取严重度定级（如 严重度定级为【高危】）
  let severityLevel: string | undefined = undefined;
  const sevMatch = judgeBody.match(/(?:严重度定级为|初步严重度定级为|初步定级为|严重程度[：:]|定级[：:])\s*【?([高危中低提示建议轻微严重致命]+)】?/);
  if (sevMatch) {
    severityLevel = sevMatch[1];
  } else {
    const inlineSev = judgeBody.match(/【(致命|严重|高危|中等|中危|低危|轻微|建议|提示)】/);
    if (inlineSev) {
      severityLevel = inlineSev[1];
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

  const rawTagMatches: TagMatch[] = [];
  let m: RegExpExecArray | null;

  while ((m = tagRegex.exec(judgeBody)) !== null) {
    rawTagMatches.push({
      rawTag: m[0],
      tagName: m[1].trim(),
      startIndex: m.index,
      endIndex: m.index + m[0].length,
    });
  }

  // 关键过滤：剔除行内严重度或判定状态标签（如【高危】、【致命】、【CONFIRMED】），避免切断句子导致后续要点全部丢失
  const tagMatches = rawTagMatches.filter((t) => !NON_SECTION_TAGS.has(t.tagName));

  let verdictBadge: ParsedDebateResult['verdictBadge'] = undefined;
  let verdictSummary = '';
  const sections: ParsedDebateSection[] = [];

  if (tagMatches.length === 0) {
    // 没有有效二级标签，检查是否可直接从首句或编号列表切分
    const numbered = parseNumberedItems(judgeBody);
    if (numbered) {
      // eslint-disable-next-line no-useless-escape
      const firstNumIndex = judgeBody.search(/(?:^|\n)\s*1[\.、\)]/);
      if (firstNumIndex > 0) {
        verdictSummary = judgeBody.substring(0, firstNumIndex).trim();
      } else {
        verdictSummary = judgeBody.split('\n')[0].trim();
      }

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

      // 如果是综合裁决/法官裁决
      if (/综合裁决|终审裁决|裁决结论|法官裁决|裁决结果|仲裁裁决/i.test(cur.tagName)) {
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

        // 检查裁决内容后是否紧跟编号列表（全写在综合裁决段落中）
        const numbered = parseNumberedItems(content);
        if (numbered) {
          // 将编号列表前的一句话作为裁决 summary
          // eslint-disable-next-line no-useless-escape
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
        // 其他所有业务标签（【事实认定与分析】、【源码事实】、【实测验证】等），全量解析为独立卡片
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
    } else if (/REJECTED/i.test(judgeBody)) {
      verdictBadge = {
        status: 'REJECTED',
        label: '误报驳回 (REJECTED)',
        color: 'var(--color-danger, #ef4444)',
        bg: 'var(--color-danger-subtle, rgba(239, 68, 68, 0.1))',
        border: 'var(--color-danger-border, rgba(239, 68, 68, 0.3))',
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

export interface DefectInsights {
  trigger?: string;
  impact?: string;
  reference?: string;
}

/**
 * 从缺陷描述或分析文本中智能提取结构化要点（触发条件、危害后果、参考对照）
 */
export function extractDefectInsights(text: string): DefectInsights {
  if (!text) return {};
  const insights: DefectInsights = {};

  // 1. 触发前置条件：匹配 "触发需依赖"、"前置条件"、"触发条件"、"当以"、"运行时格式串" 等
  const triggerMatch = text.match(
    /(?:(?:触发需依赖|前置条件[：:]|触发条件[：:]|当以|在传入)[^。；;\n]+(?:。|；|;|\n|$))/
  );
  if (triggerMatch) {
    const t = triggerMatch[0].trim().replace(/^[，,。；;]+|[，,。；;]+$/g, '');
    if (t.length > 5 && t.length < 240) {
      insights.trigger = t;
    }
  } else {
    const formatMatch = text.match(/(?:运行时格式串\s*["“][^"”]+["”][^。；;\n]*)/);
    if (formatMatch && formatMatch[0].length < 160) {
      insights.trigger = formatMatch[0].trim();
    }
  }

  // 2. 潜在危害与影响：匹配 ASan、越界读、SIGSEGV、buffer-overflow、真实 UB、崩溃 等
  const impactMatch = text.match(
    /(?:(?:该越界读为真实\s*UB|ASan\s*可检出|可触发\s*SIGSEGV|属于越界读|导致内存破坏|引发拒绝服务|触发崩溃)[^。；;\n]+(?:。|；|;|\n|$))/
  );
  if (impactMatch) {
    const imp = impactMatch[0].trim().replace(/^[，,。；;]+|[，,。；;]+$/g, '');
    if (imp.length > 5 && imp.length < 240) {
      insights.impact = imp;
    }
  } else {
    const ubMatch = text.match(/([^。；;\n]*(?:ASan|SIGSEGV|buffer-overflow|内存越界|未定义行为)[^。；;\n]*)/);
    if (ubMatch && ubMatch[0].trim().length > 10 && ubMatch[0].trim().length < 180) {
      insights.impact = ubMatch[0].trim();
    }
  }

  // 3. 规范对照 / 参考实现：匹配 "对照同一文件"、"同文件中"、"参考实现" 等
  const refMatch = text.match(
    /(?:(?:对照同一文件|同文件中|参考实现)[^。；;\n]+(?:均有|保护|实现|缺陷)[^。；;\n]*(?:。|；|;|\n|$))/
  );
  if (refMatch) {
    const ref = refMatch[0].trim().replace(/^[，,。；;]+|[，,。；;]+$/g, '');
    if (ref.length > 10 && ref.length < 260) {
      insights.reference = ref;
    }
  }

  return insights;
}

export const DebateVerdictView: React.FC<DebateVerdictViewProps> = ({
  detail = '',
  title,
  hunterClaim,
  challengerArg,
  judgeVerdict,
  triggerLine,
  scopeSymbol,
  className,
}) => {
  const [debateExpanded, setDebateExpanded] = useState(false);
  const [showRawJudge, setShowRawJudge] = useState(false);

  // 解析法官裁决结构
  const parsed = parseDebateContent(judgeVerdict || detail || '');

  // 1. 确定缺陷本体核心机理描述（优先 Hunter 客观事实描述，其次 detail 中剥离的成因）
  let primaryMechanism = '';
  if (hunterClaim && hunterClaim.trim()) {
    primaryMechanism = hunterClaim.trim();
  } else if (detail && detail.trim()) {
    const trimmedDetail = detail.trim();
    const judgePrefixRegex = /(?:^|\n)\s*【?(?:仲裁法官裁决词|仲裁法官裁判词|仲裁法官裁决|仲裁法官裁判|法官裁决词|法官裁判词|法官裁决|法官裁判)】?[：:\s]*/;
    const judgeMatch = trimmedDetail.match(judgePrefixRegex);
    if (judgeMatch && judgeMatch.index !== undefined && judgeMatch.index > 0) {
      const preJudge = trimmedDetail.substring(0, judgeMatch.index).trim();
      if (preJudge && preJudge !== title) {
        primaryMechanism = preJudge;
      }
    }
    if (!primaryMechanism) {
      if (!judgeMatch) {
        primaryMechanism = trimmedDetail;
      } else {
        // detail 包含法官裁决，提取判词中事实与成因部分
        const matchIdx = judgeMatch.index ?? 0;
        const judgeBody = trimmedDetail.substring(matchIdx + judgeMatch[0].length).trim();
        primaryMechanism = judgeBody;
      }
    }
  } else if (judgeVerdict && judgeVerdict.trim()) {
    primaryMechanism = judgeVerdict.trim();
  } else if (title) {
    primaryMechanism = title;
  }

  // 2. 提取判词正文（用于审理存证胶囊）
  let judgeBodyText = judgeVerdict?.trim() || '';
  if (!judgeBodyText && detail) {
    const judgePrefixRegex = /(?:^|\n)\s*【?(?:仲裁法官裁决词|仲裁法官裁判词|仲裁法官裁决|仲裁法官裁判|法官裁决词|法官裁判词|法官裁决|法官裁判)】?[：:\s]*/;
    const judgeMatch = detail.match(judgePrefixRegex);
    if (judgeMatch) {
      judgeBodyText = detail.substring((judgeMatch.index ?? 0) + judgeMatch[0].length).trim();
    }
  }

  // 3. 结构化要点提取（触发条件、危害后果、对照参考）
  const insights = extractDefectInsights(`${primaryMechanism} ${judgeBodyText}`);
  const hasInsights = Boolean(insights.trigger || insights.impact || insights.reference);

  // 4. 仲裁事实链存证信息判定
  const hasAuditInfo = Boolean(
    hunterClaim ||
    challengerArg ||
    judgeBodyText ||
    parsed.isDebate ||
    (parsed.verdictBadge && parsed.verdictBadge.status !== 'UNKNOWN')
  );

  const verdictBadge = parsed.verdictBadge || (
    judgeBodyText && /CONFIRMED|属真实缺陷点|缺陷点真实存在|判定成立|成立/i.test(judgeBodyText)
      ? {
          status: 'CONFIRMED' as const,
          label: '缺陷事实成立 (CONFIRMED)',
          color: 'var(--color-success, #10b981)',
          bg: 'var(--color-success-subtle, rgba(16, 185, 129, 0.1))',
          border: 'var(--color-success-border, rgba(16, 185, 129, 0.3))',
        }
      : judgeBodyText && /REJECTED|误报|驳回/i.test(judgeBodyText)
      ? {
          status: 'REJECTED' as const,
          label: '误报驳回 (REJECTED)',
          color: 'var(--color-danger, #ef4444)',
          bg: 'var(--color-danger-subtle, rgba(239, 68, 68, 0.1))',
          border: 'var(--color-danger-border, rgba(239, 68, 68, 0.3))',
        }
      : judgeBodyText && /CONDITIONAL|条件/i.test(judgeBodyText)
      ? {
          status: 'CONDITIONAL' as const,
          label: '条件触发 (CONDITIONAL)',
          color: 'var(--color-warning, #f59e0b)',
          bg: 'var(--color-warning-subtle, rgba(245, 158, 11, 0.1))',
          border: 'var(--color-warning-border, rgba(245, 158, 11, 0.3))',
        }
      : judgeBodyText
      ? {
          status: 'CONFIRMED' as const,
          label: '法官裁决已出 (ADJUDICATED)',
          color: 'var(--color-success, #10b981)',
          bg: 'var(--color-success-subtle, rgba(16, 185, 129, 0.1))',
          border: 'var(--color-success-border, rgba(16, 185, 129, 0.3))',
        }
      : undefined
  );

  if (!primaryMechanism && !hasAuditInfo) {
    return null;
  }

  return (
    <div className={`code-defect-view-container ${className || ''}`} style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem', textAlign: 'left' }}>
      {/* 1. 缺陷机理与成因剖析 (Root Cause & Mechanism - 黄金首屏主角) */}
      {primaryMechanism && (
        <div className="code-defect-mechanism">
          <div className="code-defect-mechanism__header">
            <div className="code-defect-mechanism__title">
              <span>📌 缺陷机理与成因剖析</span>
            </div>
            <div className="code-defect-mechanism__badges">
              {triggerLine && (
                <span className="code-defect-mechanism__pill" title="引发风险的关键单一语句">
                  触发行：<code>{triggerLine}</code>
                </span>
              )}
              {scopeSymbol && (
                <span className="code-defect-mechanism__pill" title="AST 作用域符号">
                  作用域：<code>{scopeSymbol}</code>
                </span>
              )}
            </div>
          </div>

          {/* 客观机理主文本 */}
          <div className="code-defect-mechanism__body">
            {renderInlineFormattedText(primaryMechanism)}
          </div>

          {/* 结构化要点提取 (触发条件、危害、对照) */}
          {hasInsights && (
            <div className="code-defect-insights">
              {insights.trigger && (
                <div className="code-defect-insight-item code-defect-insight-item--trigger">
                  <span className="code-defect-insight-label">⚡ 触发条件：</span>
                  <span className="code-defect-insight-content">
                    {renderInlineFormattedText(insights.trigger)}
                  </span>
                </div>
              )}
              {insights.impact && (
                <div className="code-defect-insight-item code-defect-insight-item--impact">
                  <span className="code-defect-insight-label">💥 潜在危害：</span>
                  <span className="code-defect-insight-content">
                    {renderInlineFormattedText(insights.impact)}
                  </span>
                </div>
              )}
              {insights.reference && (
                <div className="code-defect-insight-item code-defect-insight-item--reference">
                  <span className="code-defect-insight-label">📐 规范对照：</span>
                  <span className="code-defect-insight-content">
                    {renderInlineFormattedText(insights.reference)}
                  </span>
                </div>
              )}
            </div>
          )}
        </div>
      )}

      {/* 2. AI 智能体仲裁与对抗事实链胶囊 (Audit Trail Capsule - 默认轻量收拢) */}
      {hasAuditInfo && (
        <div className="code-audit-capsule">
          <div
            className="code-audit-capsule__bar"
            onClick={() => setDebateExpanded(!debateExpanded)}
            title="点击展开或收起多智能体（Hunter ➜ Challenger ➜ Judge）对抗事实链与仲裁依据"
          >
            <div className="code-audit-capsule__lead">
              {verdictBadge && (
                <span
                  className="code-audit-capsule__badge"
                  style={{
                    background: verdictBadge.bg,
                    color: verdictBadge.color,
                    borderColor: verdictBadge.border,
                  }}
                >
                  ⚖️ {verdictBadge.label}
                </span>
              )}
              <span className="code-audit-capsule__title">
                🛡️ AI 智能体仲裁与对抗事实链
              </span>
              {parsed.severityLevel && (
                <span
                  style={{
                    fontSize: '0.75rem',
                    fontWeight: 600,
                    color: 'var(--color-warning, #f59e0b)',
                    background: 'var(--color-warning-subtle, rgba(245, 158, 11, 0.1))',
                    border: '1px solid var(--color-warning-border, rgba(245, 158, 11, 0.3))',
                    padding: '0.12rem 0.45rem',
                    borderRadius: '4px',
                  }}
                >
                  定级：{parsed.severityLevel}
                </span>
              )}
            </div>

            <div className="code-audit-capsule__toggle">
              <span>{debateExpanded ? '▲ 收起事实链' : '▼ 展开辩论与判词'}</span>
            </div>
          </div>

          {debateExpanded && (
            <div className="code-audit-capsule__body">
              {/* 阶段 1: Hunter 初筛猎手主张 */}
              {hunterClaim && (
                <div className="code-audit-stage code-audit-stage--hunter">
                  <div className="code-audit-stage__title" style={{ color: 'var(--color-danger, #ef4444)' }}>
                    🎯 Hunter (初筛猎手主张与攻击假设):
                  </div>
                  <div className="code-audit-stage__content">
                    {renderInlineFormattedText(hunterClaim)}
                  </div>
                </div>
              )}

              {/* 阶段 2: Challenger 对抗辩护证据 */}
              {challengerArg && (
                <div className="code-audit-stage code-audit-stage--challenger">
                  <div className="code-audit-stage__title" style={{ color: 'var(--color-primary, #3b82f6)' }}>
                    ⚖️ Challenger (对抗辩护论证与抗辩证据):
                  </div>
                  <div className="code-audit-stage__content">
                    {renderInlineFormattedText(challengerArg)}
                  </div>
                </div>
              )}

              {/* 阶段 3: Judge 终审法官裁决书 */}
              {(judgeBodyText || parsed.verdictSummary) && (
                <div className="code-audit-stage code-audit-stage--judge">
                  <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '0.5rem', flexWrap: 'wrap' }}>
                    <div className="code-audit-stage__title" style={{ color: 'var(--color-success, #10b981)' }}>
                      📜 Judge (终审法官裁决书与法理推演):
                    </div>
                    <button
                      type="button"
                      onClick={(e) => {
                        e.stopPropagation();
                        setShowRawJudge(!showRawJudge);
                      }}
                      style={{
                        fontSize: '0.74rem',
                        color: 'var(--color-text-muted, #64748b)',
                        background: 'transparent',
                        border: 'none',
                        cursor: 'pointer',
                        padding: '2px 6px',
                        borderRadius: '4px',
                      }}
                    >
                      {showRawJudge ? '✨ 切换结构化' : '📄 查看纯文本'}
                    </button>
                  </div>

                  {showRawJudge ? (
                    <div className="code-audit-stage__content">
                      {renderInlineFormattedText(judgeBodyText || parsed.rawText || '')}
                    </div>
                  ) : (
                    <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem', marginTop: '0.25rem' }}>
                      {/* 如果有解析出的摘要，或当没有 sections 时展示完整裁判文本 */}
                      {(parsed.verdictSummary || parsed.sections.length === 0) && (
                        <div
                          className="code-audit-stage__content"
                          style={{ fontWeight: parsed.sections.length > 0 ? 500 : 400 }}
                        >
                          {renderInlineFormattedText(parsed.verdictSummary || judgeBodyText || parsed.rawText || '')}
                        </div>
                      )}
                      {parsed.sections.length > 0 && (
                        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.55rem', marginTop: '0.35rem' }}>
                          {parsed.sections.map((section, sIdx) => (
                            <div
                              key={sIdx}
                              style={{
                                padding: '0.65rem 0.85rem',
                                background: 'var(--color-bg-muted, rgba(255, 255, 255, 0.02))',
                                border: '1px solid var(--color-border-primary, #334155)',
                                borderLeft: `3px solid ${section.borderColor}`,
                                borderRadius: 'var(--radius-sm, 6px)',
                                fontSize: '0.84rem',
                                lineHeight: 1.55,
                              }}
                            >
                              <div
                                style={{
                                  fontWeight: 700,
                                  color: section.color,
                                  marginBottom: '0.35rem',
                                  display: 'flex',
                                  alignItems: 'center',
                                  gap: '0.35rem',
                                  fontSize: '0.82rem',
                                }}
                              >
                                <span>{section.icon}</span>
                                <span>{section.title}</span>
                              </div>
                              {section.items && section.items.length > 0 ? (
                                <div style={{ display: 'flex', flexDirection: 'column', gap: '0.45rem' }}>
                                  {section.items.map((it, iIdx) => (
                                    <div key={iIdx} style={{ display: 'flex', alignItems: 'flex-start', gap: '0.5rem', fontSize: '0.82rem' }}>
                                      <span
                                        style={{
                                          flexShrink: 0,
                                          minWidth: '18px',
                                          height: '18px',
                                          borderRadius: '3px',
                                          background: section.bgColor,
                                          color: section.color,
                                          fontSize: '0.7rem',
                                          fontWeight: 700,
                                          display: 'inline-flex',
                                          alignItems: 'center',
                                          justifyContent: 'center',
                                          marginTop: '0.15rem',
                                        }}
                                      >
                                        {it.index}
                                      </span>
                                      <div style={{ flex: 1, lineHeight: 1.5 }}>
                                        {it.title && <strong style={{ color: section.color, marginRight: '0.35rem' }}>{it.title}：</strong>}
                                        <span>{renderInlineFormattedText(it.content)}</span>
                                      </div>
                                    </div>
                                  ))}
                                </div>
                              ) : (
                                <div className="code-audit-stage__content">
                                  {renderInlineFormattedText(section.content)}
                                </div>
                              )}
                            </div>
                          ))}
                        </div>
                      )}
                    </div>
                  )}
                </div>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  );
};

export default DebateVerdictView;
