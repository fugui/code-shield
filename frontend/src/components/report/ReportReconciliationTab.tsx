import React, { useEffect, useMemo, useState } from 'react';
import { Pagination } from '@code/common';
import { apiUrl } from '../../config';
import DefectDetailPanel from '../defects/DefectDetailPanel';
import {
  DefectCandidate,
  DefectDetailData,
  ObservationProjection,
  ReconciliationSummary,
  ReconciliationUnmatchedDefect,
  TaskFindingItem,
  TaskReportMeta,
  ObservationSourceFinding,
} from '../../types/report';

interface ReportReconciliationTabProps {
  meta?: TaskReportMeta;
  reconciliation: ReconciliationSummary | null;
  findingsPage: { items: TaskFindingItem[] } | null;
  loading: boolean;
  error?: string | null;
  onRefresh: () => void;
  readonly?: boolean;
}

interface FileIssueGroup {
  key: string;
  observationPath?: string;
  defectPath?: string;
  observations: ObservationProjection[];
  unmatched: ReconciliationUnmatchedDefect[];
  issues: FileIssueItem[];
}

interface FileIssueItem {
  key: string;
  origin: 'EXISTING' | 'NEW' | 'ARCHIVED';
  lineNumber?: number | null;
  lineEnd?: number | null;
  observation?: ObservationProjection;
  finding?: TaskFindingItem | ObservationSourceFinding | null;
  defect?: ReconciliationUnmatchedDefect;
}

const verdictMeta: Record<string, { label: string; className: string }> = {
  NEW: { label: '本轮新增', className: 'verdict-new' },
  EXISTED: { label: '本轮复现', className: 'verdict-existed' },
  REOPENED: { label: '修复后复发', className: 'verdict-reopened' },
  PROBABLE: { label: '疑似存量，待确认', className: 'verdict-probable' },
  RESOLVED: { label: '本轮判定修复', className: 'verdict-resolved' },
  COVERAGE_GAP: { label: '覆盖缺口，不判修复', className: 'verdict-coverage-gap' },
};

const matchTierLabels: Record<string, string> = {
  STRONG: '强身份匹配',
  STRUCT: '结构匹配',
  BUCKET: '弱候选匹配',
  ASSIGN: '全局指派',
  AI: 'AI 仲裁',
  AI_FALLBACK: 'AI 兜底裁决',
  HUMAN: '人工确认',
  UNCLAIMED: '未匹配',
};

const reasonLabels: Record<string, string> = {
  GRAY_BAND_CANDIDATE: '结果落在灰区，需要人工裁决',
  CANDIDATE_BUDGET_EXCEEDED: '候选数量超限，结果已降级',
  AI_UNAVAILABLE: 'AI 仲裁不可用，结果已降级',
};

const unmatchedStatusLabels: Record<string, string> = {
  ACTIVE: '未命中',
  COVERAGE_GAP: '覆盖缺口',
  VERIFIED_PENDING: '待复核',
  DORMANT: '休眠',
};

const scoreFactorLabels: Record<string, string> = {
  context_key_equal: '上下文键一致',
  stmt_shape_equal: '语句形态一致',
  clean_token_similarity: '稳定令牌相似度',
  shape_tokens_similarity: '形态令牌相似度',
  context_similarity: '上下文相似度',
  defect_class_major_equal: '大类一致',
  line_proximity: '行号接近度',
  scope_key_equal: '作用域键一致',
  blob_change_consistency: '代码块一致性',
  severity_compatibility: '严重级兼容性',
};

function candidateStatusLabel(status: string) {
  return unmatchedStatusLabels[status] || status;
}

function fileBaseName(path?: string | null) {
  const normalized = (path || '').trim().replace(/\\/g, '/');
  const segments = normalized.split('/').filter(Boolean);
  return segments[segments.length - 1] || normalized;
}

function formatShortLocation(path: string | undefined, lineNumber?: string) {
  if (!path) return '';
  return lineNumber ? `${fileBaseName(path)}:${lineNumber}` : fileBaseName(path);
}

function formatCandidateLocation(candidate: Pick<DefectCandidate, 'norm_path' | 'line_start' | 'line_end'>) {
  if (!candidate.line_start) return candidate.norm_path || '—';
  if (candidate.line_end && candidate.line_end !== candidate.line_start) {
    return `${candidate.norm_path}:${candidate.line_start}-${candidate.line_end}`;
  }
  return `${candidate.norm_path}:${candidate.line_start}`;
}

function formatConfidence(value?: number) {
  if (typeof value !== 'number' || Number.isNaN(value)) return '—';
  return Math.round(value * 100) / 100 === 0 ? '0' : value.toFixed(2);
}

function formatScoreFactors(scoreDetail?: Record<string, number>) {
  return Object.entries(scoreDetail || {}).map(([name, value]) => {
    const label = scoreFactorLabels[name] || name;
    return `${label} ${value}`;
  });
}

function formatReason(reason?: string): string {
  if (!reason) return '系统无法给出唯一稳定归并结论';
  const separatorIndex = reason.indexOf(':');
  if (separatorIndex > 0) {
    const code = reason.slice(0, separatorIndex);
    const detail = reason.slice(separatorIndex + 1).trim();
    if (code === 'AUTO_MERGE' && detail) return `自动合并（灰区分数达标）：${formatReason(detail)}`;
    if (code === 'AUTO_NEW' && detail) return `自动新增（灰区证据不足）：${formatReason(detail)}`;
    if (code === 'AI_REVIEW' && detail) return `AI 要求人工复核：${detail}`;
    if (code === 'AI_DIFFERENT' && detail) return `AI 判定为不同：${detail}`;
    if (code === 'AI_SAME' && detail) return `AI 判定为相同：${detail}`;
  }
  return reasonLabels[reason] || reason;
}

function formatUnmatchedLocation(defect?: ReconciliationUnmatchedDefect) {
  if (!defect?.line_start) return '行号未加载';
  if (defect.line_end && defect.line_end !== defect.line_start) {
    return `行 ${defect.line_start}-${defect.line_end}`;
  }
  return `行 ${defect.line_start}`;
}

function formatIssueLine(lineNumber?: number | string | null) {
  return lineNumber ? `行 ${lineNumber}` : '行号未加载';
}

function formatIssueSeverity(severity?: string | null) {
  return severity ? `等级 ${severity}` : '等级 未标记';
}

function issueOriginMeta(origin: FileIssueItem['origin']) {
  if (origin === 'NEW') return { label: '本轮新增', className: 'origin-new' };
  if (origin === 'ARCHIVED') return { label: '历史归集', className: 'origin-archived' };
  return { label: '本轮复现', className: 'origin-existing' };
}

function normalizeFilePath(path?: string | null) {
  return (path || '').trim().replace(/\\/g, '/').replace(/^\.\//, '').toLowerCase();
}

function fileIssueGroupPath(group: FileIssueGroup) {
  return group.observationPath || group.defectPath || '未定位路径';
}

function observationToDetail(
  observation: ObservationProjection,
  finding: TaskFindingItem | ObservationSourceFinding | null,
): DefectDetailData {
  const findingRecord = finding && 'severity_display' in finding ? finding : null;
  const firstLine = Number.parseInt(finding?.line_number || '', 10);
  return {
    id: observation.defect_id || observation.observation_group_uid,
    title: finding?.title || '当轮证据未加载',
    norm_path: finding?.file_path || '',
    line_start: Number.isFinite(firstLine) ? firstLine : null,
    severity: findingRecord?.severity_display || '',
    category: findingRecord?.category || '',
    detail: findingRecord?.detail || '',
    code_snippet: findingRecord?.code_snippet || '',
    suggestion: findingRecord?.suggestion || '',
    status: observation.verdict,
    status_label: verdictMeta[observation.verdict]?.label || observation.verdict,
    status_reason: observation.reason,
  };
}

export default function ReportReconciliationTab({
  meta,
  reconciliation,
  findingsPage,
  loading,
  error,
  onRefresh,
  readonly = false,
}: ReportReconciliationTabProps) {
  const [selectedGroupUID, setSelectedGroupUID] = useState<string | null>(null);
  const [expandedDefectID, setExpandedDefectID] = useState<number | null>(null);
  const [expandedObservationUID, setExpandedObservationUID] = useState<string | null>(null);
  const [fileGroupOpen, setFileGroupOpen] = useState<Record<string, boolean>>({});
  const [candidates, setCandidates] = useState<DefectCandidate[] | null>(null);
  const [selectedCandidateID, setSelectedCandidateID] = useState<number | null>(null);
  const [actionReason, setActionReason] = useState('');
  const [actionError, setActionError] = useState<string | null>(null);
  const [actionBusy, setActionBusy] = useState(false);
  const [fileGroupPage, setFileGroupPage] = useState(1);
  const [fileGroupPageSize, setFileGroupPageSize] = useState(25);

  const findingByID = useMemo(() => {
    const index = new Map<number, TaskFindingItem | ObservationSourceFinding>();
    reconciliation?.observations?.forEach(observation => {
      observation.source_findings?.forEach(item => index.set(item.id, item));
    });
    findingsPage?.items?.forEach(item => index.set(item.id, item));
    return index;
  }, [findingsPage, reconciliation]);

  const probableObservations = useMemo(
    () => reconciliation?.observations.filter(item => item.verdict === 'PROBABLE') || [],
    [reconciliation],
  );
  const fileIssueGroups = useMemo(() => {
    const groups = new Map<string, FileIssueGroup>();
    const ensureGroup = (path?: string | null) => {
      const normalizedPath = normalizeFilePath(path);
      const key = normalizedPath || '__unlocated__';
      let group = groups.get(key);
      if (!group) {
        group = { key, observations: [], unmatched: [], issues: [] };
        groups.set(key, group);
      }
      return group;
    };

    reconciliation?.observations?.forEach(observation => {
      const findingID = observation.source_finding_ids?.[0];
      const finding = findingID ? findingByID.get(findingID) : null;
      const path = finding?.file_path || '';
      const group = ensureGroup(path);
      if (path && !group.observationPath) {
        group.observationPath = path;
      }
      group.observations.push(observation);
    });

    reconciliation?.unmatched_open_defects?.forEach(defect => {
      const path = defect.norm_path || '';
      const group = ensureGroup(path);
      if (path && !group.defectPath) {
        group.defectPath = path;
      }
      group.unmatched.push(defect);
    });

    const groupList = Array.from(groups.values()).map(group => {
      const observationIssues: FileIssueItem[] = group.observations.map(observation => {
        const findingID = observation.source_finding_ids?.[0];
        const finding = findingID ? findingByID.get(findingID) || null : null;
        const lineNumber = Number.parseInt(finding?.line_number || '', 10);
        return {
          key: `observation:${observation.observation_group_uid}`,
          origin: ['NEW', 'PROBABLE'].includes(observation.verdict) ? 'NEW' : 'EXISTING',
          lineNumber: Number.isFinite(lineNumber) ? lineNumber : null,
          observation,
          finding,
        };
      });
      const archivedIssues: FileIssueItem[] = group.unmatched.map(defect => ({
        key: `defect:${defect.id}`,
        origin: 'ARCHIVED' as const,
        lineNumber: defect.line_start,
        lineEnd: defect.line_end,
        defect,
      }));
      const issues = [...observationIssues, ...archivedIssues].sort((left, right) => {
        const leftLine = typeof left.lineNumber === 'number' ? left.lineNumber : Number.MAX_SAFE_INTEGER;
        const rightLine = typeof right.lineNumber === 'number' ? right.lineNumber : Number.MAX_SAFE_INTEGER;
        if (leftLine !== rightLine) return leftLine - rightLine;
        return left.key.localeCompare(right.key);
      });
      return { ...group, issues };
    });

    return groupList.sort((left, right) => {
      const leftProbable = left.observations.filter(item => item.verdict === 'PROBABLE').length;
      const rightProbable = right.observations.filter(item => item.verdict === 'PROBABLE').length;
      const riskScore = (probable: number, group: FileIssueGroup) => {
        if (probable > 0 && group.unmatched.length > 0) return 3;
        if (probable > 0) return 2;
        if (group.unmatched.length > 0) return 1;
        return 0;
      };
      const leftRisk = riskScore(leftProbable, left);
      const rightRisk = riskScore(rightProbable, right);
      if (leftRisk !== rightRisk) return rightRisk - leftRisk;
      const leftTotal = left.observations.length + left.unmatched.length;
      const rightTotal = right.observations.length + right.unmatched.length;
      if (leftTotal !== rightTotal) return rightTotal - leftTotal;
      return fileIssueGroupPath(left).localeCompare(fileIssueGroupPath(right));
    });
  }, [reconciliation, findingByID]);
  const fileGroupTotalPages = Math.max(1, Math.ceil(fileIssueGroups.length / fileGroupPageSize));
  const currentFileGroupPage = Math.min(fileGroupPage, fileGroupTotalPages);
  const visibleFileGroups = useMemo(
    () => fileIssueGroups.slice(
      (currentFileGroupPage - 1) * fileGroupPageSize,
      currentFileGroupPage * fileGroupPageSize,
    ),
    [fileIssueGroups, currentFileGroupPage, fileGroupPageSize],
  );
  useEffect(() => {
    setFileGroupPage(1);
  }, [meta?.id, reconciliation?.report_id]);
  const selectedObservation = probableObservations.find(
    item => item.observation_group_uid === selectedGroupUID,
  ) || null;
  const selectedCandidate = candidates?.find(item => item.defect_id === selectedCandidateID) || null;
  const selectedFinding = useMemo(() => {
    const findingID = selectedObservation?.source_finding_ids?.[0];
    return findingID ? findingByID.get(findingID) || null : null;
  }, [selectedObservation, findingByID]);

  useEffect(() => {
    if (!candidates || candidates.some(candidate => candidate.defect_id === selectedCandidateID)) return;
    setSelectedCandidateID(candidates[0]?.defect_id ?? null);
  }, [candidates, selectedCandidateID]);

  const selectObservation = async (observation: ObservationProjection) => {
    setSelectedGroupUID(observation.observation_group_uid);
    setSelectedCandidateID(null);
    setCandidates(null);
    setActionReason('');
    setActionError(null);
    if (!meta?.id || readonly) return;
    try {
      const res = await fetch(apiUrl(`/api/reports/${meta.id}/observations/${encodeURIComponent(observation.observation_group_uid)}/candidates`));
      if (!res.ok) throw new Error('无法加载候选缺陷');
      const data = await res.json();
      setCandidates(Array.isArray(data?.items) ? data.items : []);
    } catch (err: unknown) {
      setCandidates([]);
      setActionError(err instanceof Error ? err.message : '无法加载候选缺陷');
    }
  };

  const performAction = async (action: 'confirm' | 'bind') => {
    if (!meta?.id || !selectedObservation) return;
    if (action === 'bind' && !selectedCandidateID) {
      setActionError('请先选择要绑定的已有缺陷');
      return;
    }
    setActionBusy(true);
    setActionError(null);
    try {
      const res = await fetch(apiUrl(`/api/reports/${meta.id}/observations/${encodeURIComponent(selectedObservation.observation_group_uid)}/${action}`), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          defect_id: selectedCandidateID,
          reason: actionReason.trim() || undefined,
        }),
      });
      if (!res.ok) {
        const payload = await res.json().catch(() => ({}));
        throw new Error(payload.error || '治理操作失败');
      }
      setSelectedGroupUID(null);
      setSelectedCandidateID(null);
      setActionReason('');
      setCandidates(null);
      onRefresh();
    } catch (err: unknown) {
      setActionError(err instanceof Error ? err.message : '治理操作失败');
    } finally {
      setActionBusy(false);
    }
  };

  const stats = reconciliation?.stats || {};
  const quality = reconciliation?.quality;
  const warnings = [
    quality?.candidate_budget_exceeded ? `候选预算超限 ${quality.candidate_budget_exceeded} 项` : '',
    quality?.ai_unavailable ? `AI 仲裁不可用 ${quality.ai_unavailable} 项` : '',
    quality?.identity_moved ? `身份迁移 ${quality.identity_moved} 次` : '',
  ].filter(Boolean);
  const coverageDegraded = reconciliation && reconciliation.coverage_state !== 'COMPLETE';

  return (
    <div className="reconciliation-tab">
      <div className="reconciliation-summary-card">
        <div className="reconciliation-summary-header">
          <h3>跨轮对账摘要</h3>
          <button className="reconciliation-refresh-btn" onClick={onRefresh} disabled={loading}>
            {loading ? '刷新中…' : '刷新'}
          </button>
        </div>
        {error && <div className="reconciliation-alert">{error}</div>}
        <div className="reconciliation-stat-row">
          {[
            ['NEW', '本轮新增'],
            ['EXISTED', '本轮复现'],
            ['REOPENED', '修复后复发'],
            ['PROBABLE', '疑似重复'],
            ['RESOLVED', '本轮修复'],
            ['COVERAGE_GAP', '覆盖缺口'],
          ].map(([key, label]) => (
            <div key={key} className={`reconciliation-stat ${Number(stats[key] || 0) > 0 ? 'active' : ''}`}>
              <strong>{stats[key] || 0}</strong>
              <span>{label}</span>
            </div>
          ))}
        </div>
        <div className="reconciliation-meta-row">
          <span className={reconciliation?.ledger_ready ? 'status-good' : 'status-warn'}>
            Ledger：{reconciliation?.ledger_ready ? '已提交' : '未提交'}
          </span>
          <span className={coverageDegraded ? 'status-warn' : 'status-good'}>
            覆盖：{reconciliation?.coverage_state || '未知'}
          </span>
          <span>算法：{reconciliation?.algorithm_version || '—'}</span>
          <span>{reconciliation?.worktree_clean === false ? '工作区不干净' : '工作区干净'}</span>
          <span>AI 兜底：{quality?.gray_zone_auto_resolved || 0} 项</span>
        </div>
        {coverageDegraded && (
          <div className="reconciliation-alert warning">
            覆盖不完整，覆盖缺口中的问题不参与修复结论。
          </div>
        )}
        {warnings.length > 0 && (
          <div className="reconciliation-alert warning">
            对账质量提示：{warnings.join('；')}。
          </div>
        )}
        {Number(stats.PROBABLE || 0) > 0 && (
          <div className="reconciliation-alert info">
            当前仍存在灰区候选。AI 会先仲裁；若结论不确定，将按候选分阈值自动兜底。只有关闭灰区自动裁决或出现保留策略时，才会进入人工队列。
          </div>
        )}
      </div>

      {probableObservations.length > 0 && (
        <div className="probable-workspace">
          <div className="probable-list">
            <h4>疑似重复待裁决（{probableObservations.length}）</h4>
            {probableObservations.map(observation => {
              const finding = observation.source_finding_ids?.length
                ? findingByID.get(observation.source_finding_ids[0])
                : null;
              return (
                <button
                  key={observation.observation_group_uid}
                  className={`probable-item ${selectedGroupUID === observation.observation_group_uid ? 'selected' : ''}`}
                  onClick={() => selectObservation(observation)}
                >
                  <span className="probable-title">{finding?.title || '疑似重复问题'}</span>
                  <span className="probable-id">
                    {observation.defect_id ? `缺陷 #${observation.defect_id}` : finding ? `问题 #${finding.id}` : '编号未加载'}
                  </span>
                  <span className="probable-location" title={finding?.file_path}>
                    {formatShortLocation(finding?.file_path, finding?.line_number) || '当轮证据未加载'}
                  </span>
                  <span className="probable-score">
                    候选分 {formatConfidence(observation.confidence)} · {formatReason(observation.reason)}
                  </span>
                </button>
              );
            })}
          </div>

          <div className="probable-detail">
            {!selectedObservation && <p className="reconciliation-muted">选择一个疑似重复问题，查看候选缺陷并裁决。</p>}
            {selectedObservation && (
              <>
                <div className="decision-banner">
                  <strong>候选分落在人工灰区</strong>
                  <p>
                    该候选分 {formatConfidence(selectedObservation.confidence)} 落在人工灰区。AI 结论：{formatReason(selectedObservation.reason)}；请基于下方同一对比视图确认“同一缺陷”或“新缺陷”。
                  </p>
                </div>

                <div className="comparison-grid">
                  <section className="evidence-panel">
                    <header>
                      <span className="evidence-tag current">当轮证据</span>
                      <strong>{selectedFinding?.title || '—'}</strong>
                    </header>
                    <div className="evidence-meta">
                      <span>{selectedFinding?.severity || '未分级'}</span>
                      {selectedFinding?.category && <span>{selectedFinding.category}</span>}
                      <code>{selectedFinding ? `${selectedFinding.file_path}:${selectedFinding.line_number}` : '—'}</code>
                    </div>
                    <div className="evidence-field"><span>作用域</span><code>{selectedFinding?.scope_symbol || '—'}</code></div>
                    <div className="evidence-field"><span>匹配层级</span><strong>{matchTierLabels[selectedObservation.match_tier] || selectedObservation.match_tier}</strong></div>
                    {selectedFinding?.detail && (
                      <details className="evidence-details" open={selectedFinding.detail.length <= 240}>
                        <summary>问题详情</summary>
                        <p>{selectedFinding.detail}</p>
                      </details>
                    )}
                    {selectedFinding?.code_snippet && (
                      <details className="evidence-details">
                        <summary>代码片段</summary>
                        <pre>{selectedFinding.code_snippet}</pre>
                      </details>
                    )}
                    {selectedFinding?.suggestion && (
                      <details className="evidence-details">
                        <summary>修复建议</summary>
                        <p>{selectedFinding.suggestion}</p>
                      </details>
                    )}
                  </section>

                  <section className="evidence-panel">
                    <header>
                      <span className="evidence-tag candidate">候选已有缺陷</span>
                      <strong>{selectedCandidate?.title || (selectedCandidate ? `缺陷 #${selectedCandidate.defect_id}` : '选择候选后对比')}</strong>
                    </header>
                    {selectedCandidate ? (
                      <>
                        <div className="evidence-meta">
                          <span>{selectedCandidate.severity || '未分级'}</span>
                          {selectedCandidate.category && <span>{selectedCandidate.category}</span>}
                          <code>{formatCandidateLocation(selectedCandidate)}</code>
                        </div>
                        <div className="evidence-field"><span>作用域</span><code>{selectedCandidate.symbol_path || '—'}</code></div>
                        <div className="evidence-field">
                          <span>历史轨迹</span>
                          <strong>#{selectedCandidate.first_report_id || '—'} 首现 · #{selectedCandidate.last_seen_report_id || '—'} 出现 · #{selectedCandidate.last_matched_report_id || '—'} 命中</strong>
                        </div>
                        {selectedCandidate.detail && (
                          <details className="evidence-details" open={selectedCandidate.detail.length <= 240}>
                            <summary>问题详情</summary>
                            <p>{selectedCandidate.detail}</p>
                          </details>
                        )}
                        {selectedCandidate.code_snippet && (
                          <details className="evidence-details">
                            <summary>代码片段</summary>
                            <pre>{selectedCandidate.code_snippet}</pre>
                          </details>
                        )}
                        {selectedCandidate.suggestion && (
                          <details className="evidence-details">
                            <summary>修复建议</summary>
                            <p>{selectedCandidate.suggestion}</p>
                          </details>
                        )}
                      </>
                    ) : (
                      <p className="reconciliation-muted">使用标题旁的候选点选择一项，查看与当轮证据的可比信息。</p>
                    )}
                  </section>
                </div>

                <div className="score-factors">
                  <strong>评分因子</strong>
                  <div>
                    {formatScoreFactors(selectedObservation.score_detail).map(factor => (
                      <span key={factor}>{factor}</span>
                    ))}
                  </div>
                </div>

                <div className="candidate-selector-header">
                  <h4>候选已有缺陷（{candidates?.length || 0}）</h4>
                  {candidates?.length ? (
                    <div className="candidate-switcher" role="radiogroup" aria-label="候选已有缺陷">
                      {candidates.map((candidate, index) => (
                        <label
                          key={candidate.defect_id}
                          className={selectedCandidateID === candidate.defect_id ? 'selected' : ''}
                          title={`${candidate.title || `缺陷 #${candidate.defect_id}`} · ${formatCandidateLocation(candidate)} · 候选分 ${formatConfidence(candidate.score)}`}
                        >
                          <input
                            type="radio"
                            name="defect-candidate"
                            checked={selectedCandidateID === candidate.defect_id}
                            onChange={() => setSelectedCandidateID(candidate.defect_id)}
                          />
                          <span>{index + 1}</span>
                        </label>
                      ))}
                    </div>
                  ) : null}
                  {!candidates && <span className="reconciliation-muted">候选加载中…</span>}
                  {candidates?.length === 0 && (
                    <span className="reconciliation-muted">没有达到候选阈值的已有缺陷。</span>
                  )}
                </div>
                {selectedCandidate && (
                  <div className="candidate-selector-meta">
                    <strong title={selectedCandidate.title}>{selectedCandidate.title || `缺陷 #${selectedCandidate.defect_id}`}</strong>
                    <span>
                      #{selectedCandidate.defect_id} · {candidateStatusLabel(selectedCandidate.status)} ·{' '}
                      {matchTierLabels[selectedCandidate.match_tier] || selectedCandidate.match_tier} · 候选分{' '}
                      {formatConfidence(selectedCandidate.score)}
                    </span>
                  </div>
                )}

                {!readonly && (
                  <div className="probable-actions">
                    <textarea
                      value={actionReason}
                      onChange={event => setActionReason(event.target.value)}
                      placeholder="填写裁决理由，便于后续追溯（可选）"
                      rows={2}
                    />
                    <button
                      className="probable-primary"
                      onClick={() => performAction('bind')}
                      disabled={actionBusy || !selectedCandidateID}
                    >
                      绑定已有缺陷
                    </button>
                    <button
                      className="probable-secondary"
                      onClick={() => performAction('confirm')}
                      disabled={actionBusy}
                    >
                      确认为新缺陷
                    </button>
                  </div>
                )}
                {actionError && <div className="reconciliation-alert">{actionError}</div>}
              </>
            )}
          </div>
        </div>
      )}

      <div className="observation-section">
        <h4>按文件查看问题</h4>
        <p className="reconciliation-muted">
          本轮扫描结果与未命中历史缺陷按规范化路径合并展示，并按行号排序。同一文件同时出现灰区和未命中时会提示重复风险，但不会自动判定为重复。
        </p>
        <div className="file-group-list">
          {visibleFileGroups.map(group => {
            const observationCounts = group.observations.reduce<Record<string, number>>((counts, observation) => {
              counts[observation.verdict] = (counts[observation.verdict] || 0) + 1;
              return counts;
            }, {});
            const probableCount = observationCounts.PROBABLE || 0;
            const duplicateRisk = probableCount > 0 && group.unmatched.length > 0;
            const isOpen = fileGroupOpen[group.key] ?? (probableCount > 0 || group.unmatched.length > 0);
            const toggleFileGroup = () => setFileGroupOpen(prev => ({ ...prev, [group.key]: !isOpen }));

            return (
              <section key={group.key} className="file-group">
                <button
                  type="button"
                  className={`file-group-header ${isOpen ? 'expanded' : ''}`}
                  onClick={toggleFileGroup}
                  aria-expanded={isOpen}
                >
                  <div className="file-group-title">
                    <code>{fileIssueGroupPath(group)}</code>
                    <div className="file-group-badges">
                      {group.observations.length > 0 && (
                        <span className="file-group-tag neutral">本轮 {group.observations.length}</span>
                      )}
                      {(['NEW', 'EXISTED', 'REOPENED', 'PROBABLE', 'RESOLVED', 'COVERAGE_GAP'] as const).map(verdict => {
                        const count = observationCounts[verdict] || 0;
                        if (!count) return null;
                        return (
                          <span
                            key={verdict}
                            className={`file-group-tag ${verdict === 'PROBABLE' ? 'warn' : 'neutral'}`}
                          >
                            {verdictMeta[verdict]?.label || verdict} {count}
                          </span>
                        );
                      })}
                      {group.unmatched.length > 0 && (
                        <span className="file-group-tag neutral">历史归集 {group.unmatched.length}</span>
                      )}
                      {duplicateRisk && <span className="file-group-tag risk">重复风险</span>}
                    </div>
                  </div>
                  <span className="unmatched-expand-indicator">{isOpen ? '收起' : '展开'}</span>
                </button>

                {isOpen && (
                  <div className="file-group-content">
                    <div className="observation-list">
                      {group.issues.map(issue => {
                        const observation = issue.observation;
                        const defect = issue.defect;
                        const finding = issue.finding;
                        const origin = issueOriginMeta(issue.origin);
                        const verdict = observation
                          ? verdictMeta[observation.verdict] || { label: observation.verdict, className: 'verdict-probable' }
                          : null;
                        const findingSeverity = finding && 'severity_display' in finding
                          ? finding.severity_display
                          : finding?.severity;
                        const issueID = observation
                          ? observation.defect_id
                            ? `缺陷 #${observation.defect_id}`
                            : finding
                              ? `问题 #${finding.id}`
                              : '问题编号未加载'
                          : `缺陷 #${defect?.id}`;
                        const verdictSuffix = verdict && verdict.label !== origin.label
                          ? verdict.label
                          : '';
                        const verdictClassName = verdict?.className || '';
                        const expanded = observation
                          ? expandedObservationUID === observation.observation_group_uid
                          : expandedDefectID === defect?.id;
                        const metaText = observation
                          ? [
                              issueID,
                              formatIssueLine(finding?.line_number),
                              formatIssueSeverity(findingSeverity),
                              `${matchTierLabels[observation.match_tier] || observation.match_tier}`,
                              ...(observation.verdict === 'NEW' ? [] : [`置信度 ${formatConfidence(observation.confidence)}`]),
                            ].join(' · ')
                          : [
                              issueID,
                              formatUnmatchedLocation(defect),
                              formatIssueSeverity(defect?.severity),
                              unmatchedStatusLabels[defect?.status || ''] || defect?.status,
                              `未命中 ${defect?.missed_count || 0} 次`,
                            ].join(' · ');

                        return (
                          <React.Fragment key={issue.key}>
                            <button
                              type="button"
                              className={`observation-row unmatched-row ${expanded ? 'expanded' : ''}`}
                              onClick={() => {
                                if (observation) {
                                  setExpandedObservationUID(expanded ? null : observation.observation_group_uid);
                                } else if (defect) {
                                  setExpandedDefectID(expanded ? null : defect.id);
                                }
                              }}
                              aria-expanded={expanded}
                            >
                              <span className={`issue-origin-badge ${origin.className}`}>{origin.label}</span>
                              {verdictSuffix && (
                                <span className={`reconciliation-badge ${verdictClassName}`}>{verdictSuffix}</span>
                              )}
                              <div className="observation-main">
                                <strong>{observation ? finding?.title || '当轮证据未加载' : defect?.title || `缺陷 #${defect?.id}`}</strong>
                                <span>{metaText}</span>
                              </div>
                              <span className="unmatched-expand-indicator">{expanded ? '收起' : '展开'}</span>
                            </button>
                            {expanded && observation && (
                              <DefectDetailPanel
                                defect={observationToDetail(observation, finding ?? null)}
                                events={observation.defect_id
                                  ? reconciliation?.events.filter(event => event.defect_id === observation.defect_id)
                                  : []}
                                compact
                                hidePath
                              />
                            )}
                            {expanded && defect && (
                              <DefectDetailPanel
                                defect={defect}
                                events={reconciliation?.events.filter(event => event.defect_id === defect.id)}
                                compact
                                hidePath
                              />
                            )}
                          </React.Fragment>
                        );
                      })}
                    </div>
                  </div>
                )}
              </section>
            );
          })}
        </div>
        {fileIssueGroups.length === 0 && (
          <p className="reconciliation-muted">暂无可分组的问题。</p>
        )}
        {fileIssueGroups.length > 0 && (
          <div className="file-group-pagination">
            <Pagination
              totalItems={fileIssueGroups.length}
              page={currentFileGroupPage}
              pageSize={fileGroupPageSize}
              onPageChange={setFileGroupPage}
              onPageSizeChange={pageSize => {
                setFileGroupPageSize(pageSize);
                setFileGroupPage(1);
              }}
              className="file-group-standard-pagination"
            />
          </div>
        )}
      </div>
    </div>
  );
}
