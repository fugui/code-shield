import { CompareBucket } from '../../types/report';

export interface VerdictLabelMeta {
  label: string;
  className: string;
}

// 对账判决与层级展示标签单一真实源（SSOT），在卡片与跨轮对账页间共享
export const verdictLabels: Record<string, VerdictLabelMeta> = {
  NEW: { label: '本轮新增', className: 'verdict-new' },
  EXISTED: { label: '本轮复现', className: 'verdict-existed' },
  REOPENED: { label: '修复后复发', className: 'verdict-reopened' },
  PROBABLE: { label: '疑似存量，待确认', className: 'verdict-probable' },
  RESOLVED: { label: '本轮判定修复', className: 'verdict-resolved' },
  CLEARED: { label: '显式通过清理', className: 'verdict-cleared' },
  PASS: { label: '显式通过清理', className: 'verdict-cleared' },
  COVERAGE_GAP: { label: '覆盖缺口，不判修复', className: 'verdict-coverage-gap' },
};

export const matchTierLabels: Record<string, string> = {
  STRONG: '强身份匹配',
  STRUCT: '结构匹配',
  BUCKET: '弱候选匹配',
  ASSIGN: '全局指派',
  AI: 'AI 仲裁',
  AI_FALLBACK: 'AI 兜底裁决',
  HUMAN: '人工确认',
  PASS: '显式通过清理',
  UNCLAIMED: '未匹配',
};

export interface CompareBucketMeta {
  key: CompareBucket;
  label: string;
  description: string;
  tone: 'neutral' | 'success' | 'warning' | 'muted' | 'danger';
}

export const compareBucketList: CompareBucketMeta[] = [
  {
    key: 'COMMON_PERSISTED',
    label: '两轮共现未消减',
    description: 'A 与 B 中均归属于同一规范 DefectID 的持续缺陷',
    tone: 'neutral',
  },
  {
    key: 'ONLY_A_CODE_CHANGED',
    label: '仅 A：代码已变更',
    description: 'A 中存在而 B 中未报出，且目标文件在 B 中发生真实修改（疑似已修复）',
    tone: 'success',
  },
  {
    key: 'ONLY_A_CODE_UNCHANGED',
    label: '仅 A：代码未变未复现',
    description: '文件已完整覆盖且源码 BlobHash 一致却未报出，需关注模型漏报波动',
    tone: 'warning',
  },
  {
    key: 'ONLY_A_NOT_COVERED',
    label: '仅 A：B 未覆盖该文件',
    description: '目标报告因排除、分片失败或局部作用域未扫描到该文件，不可下修复结论',
    tone: 'muted',
  },
  {
    key: 'ONLY_B_NEW',
    label: '仅 B：目标报告新增',
    description: '相对基线报告 A 新进入的规范缺陷或待评估条目',
    tone: 'danger',
  },
];

export const compareBucketMap: Record<CompareBucket, CompareBucketMeta> = compareBucketList.reduce(
  (acc, item) => {
    acc[item.key] = item;
    return acc;
  },
  {} as Record<CompareBucket, CompareBucketMeta>,
);
