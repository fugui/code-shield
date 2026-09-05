import React, { useState, useMemo } from 'react';
import { TaskDebateLog, TaskReportMeta } from '../../types/report';
import { copyToClipboardWithFallback } from '../../utils/reportUtils';
import { useToast } from '../Toast';

interface ReportDebateTabProps {
  meta?: TaskReportMeta;
  debateLogs: TaskDebateLog[] | null;
  loading: boolean;
}

interface HunterCandidateData {
  candidate_id?: string;
  file_path?: string;
  line_range?: string;
  trigger_line?: string;
  scope_symbol?: string;
  code_snippet?: string;
  cwe_category?: string;
  category?: string;
  trigger_condition?: string;
  attack_hypothesis?: string;
  suspected_trigger?: string;
}

interface DefenseArgument {
  dimension?: string;
  finding?: string;
}

interface ChallengerDefenseData {
  candidate_id?: string;
  defense_verdict?: string;
  defense_arguments?: DefenseArgument[];
  mitigating_factors?: string;
  counter_evidence_snippet?: string;
  summary?: string;
}

interface JudgeVerdictData {
  candidate_id?: string;
  verdict?: 'CONFIRMED' | 'REJECTED' | 'CONDITIONAL';
  severity_preliminary?: string;
  category?: string;
  file_path?: string;
  line_number?: string;
  trigger_line?: string;
  scope_symbol?: string;
  title?: string;
  judgement_rationale?: string;
  code_snippet?: string;
  suggestion?: string;
}

export default function ReportDebateTab({
  meta,
  debateLogs,
  loading,
}: ReportDebateTabProps) {
  const { showToast } = useToast();
  const [verdictFilter, setVerdictFilter] = useState<'ALL' | 'CONFIRMED' | 'REJECTED' | 'CONDITIONAL'>('ALL');
  const [chunkFilter, setChunkFilter] = useState<string>('ALL');
  const [searchKeyword, setSearchKeyword] = useState<string>('');
  const [expandedSnippets, setExpandedSnippets] = useState<Record<number, boolean>>({});

  const toggleSnippet = (logId: number) => {
    setExpandedSnippets(prev => ({ ...prev, [logId]: !prev[logId] }));
  };

  // 提取唯一分片列表
  const chunkOptions = useMemo(() => {
    if (!debateLogs || debateLogs.length === 0) return [];
    const set = new Set<string>();
    debateLogs.forEach(l => {
      if (l.chunk_name) set.add(l.chunk_name);
    });
    return Array.from(set).sort();
  }, [debateLogs]);

  // KPI 数据统计
  const stats = useMemo(() => {
    if (!debateLogs) {
      return {
        totalRounds: 0,
        rejectedCount: 0,
        confirmedCount: 0,
        conditionalCount: 0,
        tier1Tokens: 0,
        tier2Tokens: 0,
      };
    }

    let rejected = 0;
    let confirmed = 0;
    let conditional = 0;
    let t1Tokens = 0;
    let t2Tokens = 0;

    debateLogs.forEach(log => {
      if (log.verdict === 'REJECTED') rejected++;
      else if (log.verdict === 'CONFIRMED') confirmed++;
      else if (log.verdict === 'CONDITIONAL') conditional++;

      if (log.token_usage) {
        t1Tokens += (log.token_usage.hunter_tokens || 0);
        t2Tokens += ((log.token_usage.challenger_tokens || 0) + (log.token_usage.judge_tokens || 0));
      }
    });

    return {
      totalRounds: debateLogs.length,
      rejectedCount: rejected,
      confirmedCount: confirmed,
      conditionalCount: conditional,
      tier1Tokens: t1Tokens,
      tier2Tokens: t2Tokens,
    };
  }, [debateLogs]);

  // 计算全局流水号索引映射 (全局自增 #1, #2...)
  const indexMap = useMemo(() => {
    const map = new Map<number, number>();
    (debateLogs || []).forEach((l, idx) => {
      map.set(l.id, idx + 1);
    });
    return map;
  }, [debateLogs]);

  // 过滤后的辩论记录
  const filteredLogs = useMemo(() => {
    if (!debateLogs) return [];

    return debateLogs.filter(log => {
      // 1. 结论过滤
      if (verdictFilter !== 'ALL' && log.verdict !== verdictFilter) {
        return false;
      }

      // 2. 分片过滤
      if (chunkFilter !== 'ALL' && log.chunk_name !== chunkFilter) {
        return false;
      }

      // 3. 关键字搜索
      if (searchKeyword.trim()) {
        const kw = searchKeyword.trim().toLowerCase();
        const hunter = (log.hunter_output || {}) as HunterCandidateData;
        const judge = (log.judge_output || {}) as JudgeVerdictData;
        const seq = indexMap.get(log.id) || 0;

        const matchSeq = seq.toString() === kw || `#${seq}` === kw;
        const matchChunk = (log.chunk_name || '').toLowerCase().includes(kw);
        const matchFile = (hunter.file_path || judge.file_path || '').toLowerCase().includes(kw);
        const matchSymbol = (hunter.scope_symbol || judge.scope_symbol || '').toLowerCase().includes(kw);
        const matchHypothesis = (hunter.trigger_condition || hunter.attack_hypothesis || '').toLowerCase().includes(kw);
        const matchRationale = (judge.judgement_rationale || '').toLowerCase().includes(kw);
        const matchTitle = (judge.title || '').toLowerCase().includes(kw);

        if (!matchSeq && !matchChunk && !matchFile && !matchSymbol && !matchHypothesis && !matchRationale && !matchTitle) {
          return false;
        }
      }

      return true;
    });
  }, [debateLogs, verdictFilter, chunkFilter, searchKeyword, indexMap]);

  // 复制单条辩论记录为 Markdown 格式
  const handleCopyDebateMarkdown = async (log: TaskDebateLog, seq: number) => {
    const hunter = (log.hunter_output || {}) as HunterCandidateData;
    const chall = (log.challenger_output || {}) as ChallengerDefenseData;
    const judge = (log.judge_output || {}) as JudgeVerdictData;

    const md = [
      `### ⚔️ 多智能体辩论轨迹 [#${seq}] - ${judge.verdict || log.verdict}`,
      `- **目标分片**: \`${log.chunk_name}\``,
      `- **代码定位**: \`${hunter.file_path || judge.file_path || ''}:${hunter.line_range || judge.line_number || ''}\``,
      `- **作用域符号**: \`${hunter.scope_symbol || judge.scope_symbol || '-'}\``,
      `- **触发语句**: \`${log.trigger_line || hunter.trigger_line || '-'}\``,
      '',
      `#### 🔴 Hunter (初筛诱因假设)`,
      `- **缺陷类型**: ${hunter.category || hunter.cwe_category || '未知'}`,
      `- **触发机理与缺陷成因**: ${hunter.trigger_condition || hunter.attack_hypothesis || '无'}`,
      `- **疑似触发诱因**: ${hunter.suspected_trigger || '无'}`,
      '',
      `#### 🔵 Challenger (守方·代码抗辩)`,
      `- **抗辩结论**: ${chall.defense_verdict || '无'}`,
      `- **缓解事实与证据**: ${chall.mitigating_factors || '无'}`,
      ...(chall.defense_arguments && chall.defense_arguments.length > 0
        ? chall.defense_arguments.map(arg => `  * [${arg.dimension}] ${arg.finding}`)
        : []),
      '',
      `#### ⚖️ Judge (终审·裁决判词)`,
      `- **裁决结论**: ${judge.verdict || log.verdict}`,
      `- **裁决理由**: ${judge.judgement_rationale || '无'}`,
      judge.suggestion ? `- **修复建议**: ${judge.suggestion}` : '',
    ].filter(Boolean).join('\n');

    const ok = await copyToClipboardWithFallback(md);
    if (ok) {
      showToast(`已复制第 #${seq} 项辩论轨迹 Markdown`, 'success');
    } else {
      showToast('复制失败', 'error');
    }
  };

  if (loading && (!debateLogs || debateLogs.length === 0)) {
    return (
      <div className="report-loading">
        ⏳ 正在获取多智能体三方对抗辩论轨迹...
      </div>
    );
  }

  if (!debateLogs || debateLogs.length === 0) {
    return (
      <div className="report-debate-empty">
        <div className="report-debate-empty-icon">⚖️</div>
        <div className="report-debate-empty-title">当前任务暂无多智能体辩论轨迹</div>
        <div className="report-debate-empty-desc">
          {meta?.engine_mode === 'single'
            ? '该任务使用的是单仓全量扫描模式 (Single)，未启用多智能体对抗辩论流水线。'
            : '本次扫描分片中未发现需要发起三方对抗博弈的候选疑点，或辩论轨迹已按生命周期策略归档。'}
        </div>
      </div>
    );
  }

  const rejectedRate = stats.totalRounds > 0 ? ((stats.rejectedCount / stats.totalRounds) * 100).toFixed(1) : '0.0';
  const confirmedRate = stats.totalRounds > 0 ? ((stats.confirmedCount / stats.totalRounds) * 100).toFixed(1) : '0.0';
  const conditionalRate = stats.totalRounds > 0 ? ((stats.conditionalCount / stats.totalRounds) * 100).toFixed(1) : '0.0';

  return (
    <div className="report-debate-container">
      {/* 1. 顶层 KPI 统计看板 */}
      <div className="report-debate-kpi-grid">
        <div className="report-debate-kpi-card">
          <div className="report-debate-kpi-header">
            <span className="report-debate-kpi-icon">🎯</span>
            <span className="report-debate-kpi-title">对抗总疑点</span>
          </div>
          <div className="report-debate-kpi-value">{stats.totalRounds} <span className="report-debate-kpi-unit">轮</span></div>
          <div className="report-debate-kpi-sub">
            覆盖 {chunkOptions.length} 个语义代码分片
          </div>
        </div>

        <div className="report-debate-kpi-card kpi-card-rejected">
          <div className="report-debate-kpi-header">
            <span className="report-debate-kpi-icon">🛡️</span>
            <span className="report-debate-kpi-title">拦截误报疑点</span>
          </div>
          <div className="report-debate-kpi-value text-success">{stats.rejectedCount} <span className="report-debate-kpi-unit">个</span></div>
          <div className="report-debate-kpi-sub">
            成功拦截率 <strong className="text-success">{rejectedRate}%</strong> (误报消除)
          </div>
        </div>

        <div className="report-debate-kpi-card kpi-card-confirmed">
          <div className="report-debate-kpi-header">
            <span className="report-debate-kpi-icon">🚨</span>
            <span className="report-debate-kpi-title">确认有效缺陷</span>
          </div>
          <div className="report-debate-kpi-value text-danger">{stats.confirmedCount} <span className="report-debate-kpi-unit">个</span></div>
          <div className="report-debate-kpi-sub">
            确认率 <strong>{confirmedRate}%</strong> (进入最终报告)
          </div>
        </div>

        <div className="report-debate-kpi-card kpi-card-conditional">
          <div className="report-debate-kpi-header">
            <span className="report-debate-kpi-icon">⚠️</span>
            <span className="report-debate-kpi-title">条件触发疑点</span>
          </div>
          <div className="report-debate-kpi-value text-warning">{stats.conditionalCount} <span className="report-debate-kpi-unit">个</span></div>
          <div className="report-debate-kpi-sub">
            占比 <strong>{conditionalRate}%</strong> (附带触发约束)
          </div>
        </div>

        <div className="report-debate-kpi-card">
          <div className="report-debate-kpi-header">
            <span className="report-debate-kpi-icon">🪙</span>
            <span className="report-debate-kpi-title">智能体 Token 开销</span>
          </div>
          <div className="report-debate-kpi-value text-primary">
            {((stats.tier1Tokens + stats.tier2Tokens) / 1000).toFixed(1)}k
          </div>
          <div className="report-debate-kpi-sub">
            Hunter: {(stats.tier1Tokens / 1000).toFixed(1)}k | Tier2: {(stats.tier2Tokens / 1000).toFixed(1)}k
          </div>
        </div>
      </div>

      {/* 2. 工具栏 Filter & 搜索区域 */}
      <div className="report-debate-toolbar">
        <div className="report-debate-filter-group">
          {/* 状态过滤切换 */}
          <div className="report-debate-segmented">
            <button
              type="button"
              className={`report-debate-seg-btn ${verdictFilter === 'ALL' ? 'active' : ''}`}
              onClick={() => setVerdictFilter('ALL')}
            >
              全部 ({stats.totalRounds})
            </button>
            <button
              type="button"
              className={`report-debate-seg-btn ${verdictFilter === 'REJECTED' ? 'active active-rejected' : ''}`}
              onClick={() => setVerdictFilter('REJECTED')}
            >
              🛡️ 已拦截误报 ({stats.rejectedCount})
            </button>
            <button
              type="button"
              className={`report-debate-seg-btn ${verdictFilter === 'CONFIRMED' ? 'active active-confirmed' : ''}`}
              onClick={() => setVerdictFilter('CONFIRMED')}
            >
              🚨 确认缺陷 ({stats.confirmedCount})
            </button>
            <button
              type="button"
              className={`report-debate-seg-btn ${verdictFilter === 'CONDITIONAL' ? 'active active-conditional' : ''}`}
              onClick={() => setVerdictFilter('CONDITIONAL')}
            >
              ⚠️ 条件触发 ({stats.conditionalCount})
            </button>
          </div>

          {/* 分片筛选下拉框 */}
          {chunkOptions.length > 1 && (
            <select
              className="report-debate-select"
              value={chunkFilter}
              onChange={e => setChunkFilter(e.target.value)}
            >
              <option value="ALL">全部分片 ({chunkOptions.length} 个)</option>
              {chunkOptions.map(chunk => (
                <option key={chunk} value={chunk}>
                  分片: {chunk}
                </option>
              ))}
            </select>
          )}
        </div>

        {/* 关键字搜索框 */}
        <div className="report-debate-search-box">
          <span className="report-debate-search-icon">🔍</span>
          <input
            type="text"
            className="report-debate-search-input"
            placeholder="搜索序号 (#1) / 文件路径 / 函数名 / 触发诱因 / 裁决词..."
            value={searchKeyword}
            onChange={e => setSearchKeyword(e.target.value)}
          />
          {searchKeyword && (
            <button
              type="button"
              className="report-debate-search-clear"
              onClick={() => setSearchKeyword('')}
            >
              ✕
            </button>
          )}
        </div>
      </div>

      {/* 3. 对抗辩论卡片列表 */}
      <div className="report-debate-list">
        {filteredLogs.length === 0 ? (
          <div className="report-debate-no-match">
            未找到匹配筛选条件的对抗辩论记录
          </div>
        ) : (
          filteredLogs.map(log => {
            const hunter = (log.hunter_output || {}) as HunterCandidateData;
            const chall = (log.challenger_output || {}) as ChallengerDefenseData;
            const judge = (log.judge_output || {}) as JudgeVerdictData;
            const isSnippetOpen = expandedSnippets[log.id];

            const filePath = hunter.file_path || judge.file_path || '';
            const lineRange = hunter.line_range || judge.line_number || '';
            const scopeSymbol = hunter.scope_symbol || judge.scope_symbol || '';
            const triggerLine = log.trigger_line || hunter.trigger_line || judge.trigger_line || '';
            const codeSnippet = hunter.code_snippet || judge.code_snippet || '';

            const seq = indexMap.get(log.id) || 1;

            return (
              <div
                key={log.id}
                className={`report-debate-card report-debate-card--${log.verdict.toLowerCase()}`}
              >
                {/* 卡片头部 */}
                <div className="report-debate-card-header">
                  <div className="report-debate-header-left">
                    <span className="report-debate-candidate-id">
                      #{seq}
                    </span>
                    <span className="report-debate-chunk-badge">
                      📦 {log.chunk_name}
                    </span>
                    {filePath && (
                      <span className="report-debate-location">
                        📄 <code>{filePath}{lineRange ? `:${lineRange}` : ''}</code>
                      </span>
                    )}
                    {scopeSymbol && (
                      <span className="report-debate-scope">
                        ƒ <code>{scopeSymbol}</code>
                      </span>
                    )}
                  </div>

                  <div className="report-debate-header-right">
                    {log.duration_ms > 0 && (
                      <span className="report-debate-meta-pill">
                        ⏱️ {(log.duration_ms / 1000).toFixed(2)}s
                      </span>
                    )}
                    {log.token_usage && (
                      <span className="report-debate-meta-pill">
                        🪙 {(( (log.token_usage.hunter_tokens || 0) + (log.token_usage.challenger_tokens || 0) + (log.token_usage.judge_tokens || 0) ) / 1000).toFixed(1)}k tokens
                      </span>
                    )}
                    <span className={`report-debate-verdict-badge badge-${log.verdict.toLowerCase()}`}>
                      {log.verdict === 'REJECTED' && '🛡️ 辩护成立 · 驳回误报'}
                      {log.verdict === 'CONFIRMED' && '🚨 终审判定 · 缺陷成立'}
                      {log.verdict === 'CONDITIONAL' && '⚠️ 终审判定 · 条件成立'}
                    </span>
                    <button
                      type="button"
                      className="report-debate-action-btn"
                      title="复制本条辩论 Markdown"
                      onClick={() => handleCopyDebateMarkdown(log, seq)}
                    >
                      📋 复制
                    </button>
                  </div>
                </div>

                {/* 触发行警示 */}
                {triggerLine && (
                  <div className="report-debate-trigger-box">
                    <span className="report-debate-trigger-label">核心触发行:</span>
                    <code className="report-debate-trigger-code">{triggerLine}</code>
                  </div>
                )}

                {/* 三方对抗核心区 */}
                <div className="report-debate-stages-grid">
                  {/* 1. 🔴 Hunter 攻方 */}
                  <div className="report-debate-stage-col stage-hunter">
                    <div className="report-debate-stage-badge">
                      <span className="stage-icon">🔴</span>
                      <span className="stage-role">Hunter 猎手 (初筛审计)</span>
                      {(hunter.category || hunter.cwe_category) && (
                        <span className="cwe-pill">{hunter.category || hunter.cwe_category}</span>
                      )}
                    </div>
                    <div className="report-debate-stage-content">
                      <div className="stage-section">
                        <div className="stage-section-label">🎯 触发机理与缺陷成因:</div>
                        <div className="stage-section-text">
                          {hunter.trigger_condition || hunter.attack_hypothesis || '（未提供触发机理分析）'}
                        </div>
                      </div>
                      {hunter.suspected_trigger && (
                        <div className="stage-section">
                          <div className="stage-section-label">⚡ 疑似触发条件:</div>
                          <div className="stage-section-text">
                            {hunter.suspected_trigger}
                          </div>
                        </div>
                      )}
                    </div>
                  </div>

                  {/* 2. 🔵 Challenger 守方 */}
                  <div className="report-debate-stage-col stage-challenger">
                    <div className="report-debate-stage-badge">
                      <span className="stage-icon">🔵</span>
                      <span className="stage-role">Challenger 辩护人 (守方·抗辩)</span>
                      {chall.defense_verdict && (
                        <span className={`defense-pill pill-${chall.defense_verdict.toLowerCase()}`}>
                          {chall.defense_verdict}
                        </span>
                      )}
                    </div>
                    <div className="report-debate-stage-content">
                      {chall.mitigating_factors && (
                        <div className="stage-section">
                          <div className="stage-section-label">🛡️ 缓解事实与上下文防御:</div>
                          <div className="stage-section-text">
                            {chall.mitigating_factors}
                          </div>
                        </div>
                      )}

                      {chall.defense_arguments && chall.defense_arguments.length > 0 && (
                        <div className="stage-section">
                          <div className="stage-section-label">📑 抗辩论据明细:</div>
                          <ul className="defense-args-list">
                            {chall.defense_arguments.map((arg, aIdx) => (
                              <li key={aIdx}>
                                <span className="arg-dimension">[{arg.dimension}]</span> {arg.finding}
                              </li>
                            ))}
                          </ul>
                        </div>
                      )}

                      {chall.summary && !chall.mitigating_factors && (
                        <div className="stage-section">
                          <div className="stage-section-label">📝 辩护陈述:</div>
                          <div className="stage-section-text">{chall.summary}</div>
                        </div>
                      )}
                    </div>
                  </div>

                  {/* 3. ⚖️ Judge 终审法官 */}
                  <div className="report-debate-stage-col stage-judge">
                    <div className="report-debate-stage-badge">
                      <span className="stage-icon">⚖️</span>
                      <span className="stage-role">Judge 终审法官 (裁判·裁决)</span>
                      <span className={`judge-pill pill-${log.verdict.toLowerCase()}`}>
                        {log.verdict}
                      </span>
                    </div>
                    <div className="report-debate-stage-content">
                      {judge.title && (
                        <div className="stage-section">
                          <div className="stage-section-label">📋 判定标题:</div>
                          <div className="stage-section-title">{judge.title}</div>
                        </div>
                      )}

                      <div className="stage-section">
                        <div className="stage-section-label">🏛️ 仲裁裁决依据与法理:</div>
                        <div className="stage-section-text judge-rationale">
                          {judge.judgement_rationale || '（法官已对攻守双方意见综合审核，判定如上结论）'}
                        </div>
                      </div>

                      {judge.suggestion && (
                        <div className="stage-section">
                          <div className="stage-section-label">💡 修复与防护建议:</div>
                          <div className="stage-section-text stage-suggestion">
                            {judge.suggestion}
                          </div>
                        </div>
                      )}
                    </div>
                  </div>
                </div>

                {/* 可折叠源码片段 */}
                {codeSnippet && (
                  <div className="report-debate-snippet-wrapper">
                    <button
                      type="button"
                      className="report-debate-snippet-toggle"
                      onClick={() => toggleSnippet(log.id)}
                    >
                      <span>{isSnippetOpen ? '▼ 收起源代码片段' : '▶ 展开关联源代码片段'}</span>
                    </button>
                    {isSnippetOpen && (
                      <pre className="report-debate-code-block">
                        <code>{codeSnippet}</code>
                      </pre>
                    )}
                  </div>
                )}
              </div>
            );
          })
        )}
      </div>
    </div>
  );
}
