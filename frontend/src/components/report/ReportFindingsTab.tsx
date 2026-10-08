import React, { useState, useEffect, useRef, useMemo } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Pagination } from '@code/common';
import {
  FindingsPageResponse,
  ObservationProjection,
  ReconciliationSummary,
  TaskReportMeta,
} from '../../types/report';
import FindingCard from './FindingCard';
import ReportEmptyState from './ReportEmptyState';

const ENTITY_MODE_SEVS = ['pass', 'unpass'];
const DEFECT_MODE_SEVS = ['fatal', 'critical', 'major', 'suggestion'];

interface ReportFindingsTabProps {
  meta?: TaskReportMeta;
  findingsPage: FindingsPageResponse | null;
  reconciliation: ReconciliationSummary | null;
  loading: boolean;
  onFilterChange: (filters: Record<string, any>) => void;
}

interface SeverityCardItem {
  key: string;
  name: string;
  count: number;
  color: string;
}

export default function ReportFindingsTab({
  meta,
  findingsPage,
  reconciliation,
  loading,
  onFilterChange,
}: ReportFindingsTabProps) {
  const [searchParams] = useSearchParams();
  const findingIdParam = searchParams.get('findingId');
  const isEntityMode = meta?.governance_mode === 'entity_assessment';
  const modeKeys = isEntityMode ? ENTITY_MODE_SEVS : DEFECT_MODE_SEVS;

  const [selectedSevs, setSelectedSevs] = useState<string[]>(modeKeys);
  const [selectedStatus, setSelectedStatus] = useState<string>('');
  const [keyword, setKeyword] = useState<string>('');
  const [debouncedKeyword, setDebouncedKeyword] = useState<string>('');
  const [page, setPage] = useState<number>(1);
  const [pageSize, setPageSize] = useState<number>(25);
  // meta 异步到达后治理模式可能从默认值翻转，这里在模式变化时校正默认选中项
  const prevModeRef = useRef(isEntityMode);
  useEffect(() => {
    if (prevModeRef.current !== isEntityMode) {
      prevModeRef.current = isEntityMode;
      setSelectedSevs(prev => {
        const valid = prev.filter(k => modeKeys.includes(k));
        return valid.length > 0 ? valid : modeKeys;
      });
    }
  }, [isEntityMode, modeKeys]);

  const metrics = findingsPage?.metrics;
  const items = useMemo(() => findingsPage?.items || [], [findingsPage]);
  const total = findingsPage?.total || 0;
  const observationByFindingID = useMemo(() => {
    const index = new Map<number, ObservationProjection>();
    reconciliation?.observations.forEach(observation => {
      observation.source_finding_ids.forEach(findingID => {
        if (!index.has(findingID)) index.set(findingID, observation);
      });
    });
    return index;
  }, [reconciliation]);

  // 搜索输入防抖
  useEffect(() => {
    const timer = setTimeout(() => {
      setDebouncedKeyword(keyword.trim());
    }, 250);
    return () => clearTimeout(timer);
  }, [keyword]);

  // 当问题列表加载完毕，根据 URL 参数 findingId 或 hash 自动定位并呼吸高亮
  useEffect(() => {
    if (!loading && items.length > 0) {
      const hashId = window.location.hash ? window.location.hash.replace('#finding-', '') : null;
      const targetId = findingIdParam || hashId;
      if (targetId) {
        let cleanTimer: NodeJS.Timeout;
        const timer = setTimeout(() => {
          const el = document.getElementById(`finding-${targetId}`);
          if (el) {
            el.scrollIntoView({ behavior: 'smooth', block: 'center' });
            el.classList.add('finding-card-highlighted');
            cleanTimer = setTimeout(() => {
              el.classList.remove('finding-card-highlighted');
            }, 3500);
          }
        }, 200);
        return () => {
          clearTimeout(timer);
          if (cleanTimer) clearTimeout(cleanTimer);
        };
      }
    }
  }, [loading, items, findingIdParam]);

  // 构建指标卡片列表
  const cards: SeverityCardItem[] = [];
  if (isEntityMode) {
    cards.push({
      key: 'pass',
      name: '合格项',
      count: metrics?.pass_count ?? 0,
      color: '#10b981',
    });
    cards.push({
      key: 'unpass',
      name: '不合格 / 风险',
      count: (metrics?.total_findings ?? 0) - (metrics?.pass_count ?? 0),
      color: '#ef4444',
    });
  } else {
    cards.push({
      key: 'fatal',
      name: '致命',
      count: metrics?.fatal_count ?? 0,
      color: '#dc2626',
    });
    cards.push({
      key: 'critical',
      name: '严重',
      count: metrics?.critical_count ?? 0,
      color: '#ea580c',
    });
    cards.push({
      key: 'major',
      name: '一般',
      count: metrics?.major_count ?? 0,
      color: '#d97706',
    });
    cards.push({
      key: 'suggestion',
      name: '建议',
      count: metrics?.suggestion_count ?? 0,
      color: '#64748b',
    });
  }

  const allAvailableKeys = cards.map(c => c.key);
  const isAllSelected = allAvailableKeys.length > 0 && allAvailableKeys.every(k => selectedSevs.includes(k));
  const severityParam = selectedSevs.length === 0
    ? '__none__'
    : isAllSelected
      ? ''
      : selectedSevs.map(k => (k === 'unpass' ? 'fatal,critical,major,minor,suggestion' : k)).join(',');

  useEffect(() => {
    onFilterChange({
      severity: severityParam,
      status: selectedStatus,
      keyword: debouncedKeyword,
      page,
      pageSize,
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps -- onFilterChange 由父组件传入，加入依赖可能导致每次渲染重复触发筛选
  }, [severityParam, selectedStatus, debouncedKeyword, page, pageSize]);

  // 标准 Checkbox 常识交互：点击卡片即切换当前级别的选中/取消状态
  const handleCardClick = (key: string) => {
    const isSelected = selectedSevs.includes(key);
    const nextSevs = isSelected && selectedSevs.length === 1
      ? [key]
      : isSelected
        ? selectedSevs.filter(selectedKey => selectedKey !== key)
        : [...selectedSevs, key];
    setSelectedSevs(nextSevs);
    setPage(1);
  };

  const handleResetAllSevs = () => {
    setSelectedSevs(allAvailableKeys);
    setPage(1);
  };

  return (
    <div>
      {!isEntityMode && reconciliation && (
        <div className={`reconciliation-strip ${reconciliation.coverage_state !== 'COMPLETE' ? 'degraded' : ''}`}>
          <div className="reconciliation-strip-main">
            <strong>跨轮对账</strong>
            <span>新增 {reconciliation.stats?.NEW || 0}</span>
            <span>存量 {reconciliation.stats?.EXISTED || 0}</span>
            <span>复发 {reconciliation.stats?.REOPENED || 0}</span>
            <span>疑似 {reconciliation.stats?.PROBABLE || 0}</span>
            <span>修复 {reconciliation.stats?.RESOLVED || 0}</span>
            <span>覆盖缺口 {reconciliation.stats?.COVERAGE_GAP || 0}</span>
          </div>
          <div className="reconciliation-strip-meta">
            <span>{reconciliation.ledger_ready ? 'Ledger 已提交' : 'Ledger 未提交'}</span>
            <span>覆盖 {reconciliation.coverage_state}</span>
            <span>算法 {reconciliation.algorithm_version || '—'}</span>
          </div>
        </div>
      )}

      {/* 1. 严重性指标卡片看板 (默认全选中高亮，支持智能单选/多选组合) */}
      <div className="severity-cards-grid no-print">
        {cards.map((c) => {
          const isSelected = selectedSevs.includes(c.key);
          return (
            <div
              key={c.key}
              onClick={() => handleCardClick(c.key)}
              className={`severity-metric-card ${isSelected ? 'selected' : ''}`}
              style={{ '--sev-color': c.color } as React.CSSProperties}
            >
              <div className="card-top-row">
                <span className="card-sev-name">
                  {c.name}
                </span>
                <div className="filter-checkbox">
                  {isSelected && (
                    <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="white" strokeWidth="4" strokeLinecap="round" strokeLinejoin="round">
                      <polyline points="20 6 9 17 4 12" />
                    </svg>
                  )}
                </div>
              </div>
              <div className="card-bottom-row">
                <span className="card-count-num" style={{ color: isSelected ? c.color : 'var(--color-text-primary)' }}>
                  {c.count}
                </span>
                <span style={{ fontSize: '0.8rem', color: 'var(--color-text-muted)' }}>个</span>
              </div>
            </div>
          );
        })}
      </div>

      {/* 2. 独立搜索与状态过滤工具栏 */}
      <div className="findings-filter-toolbar no-print">
        <div className="filter-summary-info">
          <span>共检索到 <strong style={{ color: 'var(--color-text-primary)', fontSize: '0.95rem' }}>{total}</strong> 项结果</span>
          {!isAllSelected && (
            <button
              type="button"
              className="filter-reset-btn"
              onClick={handleResetAllSevs}
              title="恢复所有级别全选"
            >
              ✕ 恢复全部级别
            </button>
          )}
        </div>

        <div className="filter-search-group">
          <select
            className="filter-status-select"
            value={selectedStatus}
            onChange={(e) => {
              const nextStatus = e.target.value;
              setSelectedStatus(nextStatus);
              setPage(1);
              onFilterChange({
                severity: severityParam,
                status: nextStatus,
                keyword: debouncedKeyword,
                page: 1,
                pageSize,
              });
            }}
          >
            <option value="">全部状态</option>
            {isEntityMode ? (
              <>
                <option value="pass">合格</option>
                <option value="fail">待整改/不合格</option>
                <option value="analyzing">复核中</option>
              </>
            ) : (
              <>
                <option value="open">待处理</option>
                <option value="analyzing">问题分析</option>
                <option value="resolved">已解决</option>
                <option value="closed">已关闭</option>
                <option value="invalid">忽略/误报</option>
              </>
            )}
          </select>

          <div className="filter-search-input-wrapper">
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="#94a3b8" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" style={{ position: 'absolute', left: '12px', pointerEvents: 'none' }}>
              <circle cx="11" cy="11" r="8" />
              <line x1="21" y1="21" x2="16.65" y2="16.65" />
            </svg>
            <input
              type="text"
              className="filter-search-input"
              placeholder="搜索标题、文件路径..."
              value={keyword}
              onChange={(e) => {
                setKeyword(e.target.value);
                setPage(1);
              }}
            />
            {keyword && (
              <button
                type="button"
                className="filter-search-clear"
                title="清空搜索"
              >
                ✕
              </button>
            )}
          </div>
        </div>
      </div>

      {/* 列表内容区 */}
      {loading && items.length === 0 ? (
        <div className="report-loading">
          ⏳ 正在加载问题清单...
        </div>
      ) : items.length === 0 ? (
        <ReportEmptyState
          type="pass"
          title={isEntityMode ? "实体评估合格！" : "代码检视通过！"}
          description={isEntityMode ? "所有评估用例及测试项均达到合格指标要求。" : "本次扫描未发现任何安全隐患或缺陷，状态良好。"}
        />
      ) : (
        <div>
          {items.map((item) => (
            (() => {
              const observation = item.observation_group_uid ? observationByFindingID.get(item.id) : undefined;
              const itemKey = observation?.defect_id
                ? `defect:${observation.defect_id}`
                : item.observation_group_uid
                  ? `observation:${meta?.id || 0}:${item.observation_group_uid}`
                  : `finding:${item.id}`;
              return (
            <FindingCard
                key={itemKey}
                finding={item}
                governanceMode={meta?.governance_mode}
                repoUrl={meta?.repo_url}
                branch={meta?.branch}
                reconciliation={observation}
              />
              );
            })()
          ))}

          {findingsPage && (
            <div className="no-print report-findings-pagination">
              <Pagination
                totalItems={findingsPage.total}
                page={page}
                pageSize={pageSize}
                onPageChange={setPage}
                onPageSizeChange={size => {
                  setPageSize(size);
                  setPage(1);
                }}
              />
            </div>
          )}
        </div>
      )}
    </div>
  );
}
