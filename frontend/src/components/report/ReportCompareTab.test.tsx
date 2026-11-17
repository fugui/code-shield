import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import ReportCompareTab from './ReportCompareTab';
import { matchTierLabels, verdictLabels } from './reconciliationLabels';
import { ReportCompareResult, TaskReportMeta } from '../../types/report';

const sampleMeta: TaskReportMeta = {
  id: 20,
  repo_id: 5,
  repo_name: 'demo-repo',
  repo_url: 'https://example.com/demo.git',
  branch: 'main',
  task_type_id: 2,
  task_type_name: 'coredump-risk',
  task_type_display: 'Core Dump 风险分析',
  engine_mode: 'debate_full',
  governance_mode: 'full_ledger',
  status: 'success',
  score: 80,
  rating: 'B',
  total_chunks: 1,
  processed_chunks: 1,
  success_chunks: 1,
  head_commit: 'abc123456789',
  created_at: '2026-11-17T10:00:00Z',
};

const sampleComparePayload: ReportCompareResult = {
  base_report: {
    id: 10,
    repo_id: 5,
    task_type_id: 2,
    coverage_state: 'COMPLETE',
    coverage_complete: true,
    governance_mode: 'full_ledger',
    branch: 'main',
    head_commit: '000aaa111',
    created_at: '2026-11-16T10:00:00Z',
  },
  target_report: {
    id: 20,
    repo_id: 5,
    task_type_id: 2,
    coverage_state: 'COMPLETE',
    coverage_complete: true,
    governance_mode: 'full_ledger',
    branch: 'main',
    head_commit: 'abc123456789',
    created_at: '2026-11-17T10:00:00Z',
  },
  data_source: 'defect_observations',
  summary: {
    common_persisted: 1,
    only_a_code_changed: 1,
    only_a_code_unchanged: 1,
    only_a_not_covered: 0,
    only_b_new: 1,
    total_a: 3,
    total_b: 2,
  },
  common_persisted: [
    {
      bucket: 'COMMON_PERSISTED',
      defect_id: 101,
      norm_path: 'src/keep.cpp',
      line_start: 12,
      line_end: 12,
      severity: '严重',
      category: 'NPD',
      title: 'Keep pointer deref',
      scope_symbol: 'keepFn',
      defect_status: 'ACTIVE',
      verdict_a: 'EXISTED',
      verdict_b: 'EXISTED',
    },
  ],
  only_a_code_changed: [
    {
      bucket: 'ONLY_A_CODE_CHANGED',
      defect_id: 102,
      norm_path: 'src/changed.cpp',
      line_start: 20,
      line_end: 20,
      severity: '严重',
      category: 'MEM_LEAK',
      title: 'Changed leak',
      scope_symbol: 'changedFn',
      base_blob_hash: 'hash-old',
      target_blob_hash: 'hash-new',
      scope_outcome_b: 'SCANNED',
    },
  ],
  only_a_code_unchanged: [
    {
      bucket: 'ONLY_A_CODE_UNCHANGED',
      defect_id: 103,
      norm_path: 'src/unchanged.cpp',
      line_start: 33,
      line_end: 33,
      severity: '一般',
      category: 'UAF',
      title: 'Unchanged uaf silent miss',
      scope_symbol: 'unchangedFn',
      base_blob_hash: 'same-hash',
      target_blob_hash: 'same-hash',
      scope_outcome_b: 'SCANNED',
    },
  ],
  only_a_not_covered: [],
  only_b_new: [
    {
      bucket: 'ONLY_B_NEW',
      defect_id: 104,
      norm_path: 'src/newfile.cpp',
      line_start: 8,
      line_end: 8,
      severity: '致命',
      category: 'OOB',
      title: 'New oob in B',
      scope_symbol: 'newFn',
      verdict_b: 'NEW',
    },
  ],
};

describe('reconciliationLabels SSOT & ReportCompareTab', () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
  });

  it('unifies EXISTED, PROBABLE, and PASS labels cleanly', () => {
    expect(verdictLabels.EXISTED.label).toBe('本轮复现');
    expect(verdictLabels.PROBABLE.label).toBe('疑似存量，待确认');
    expect(verdictLabels.PASS.label).toBe('显式通过清理');
    expect(matchTierLabels.PASS).toBe('显式通过清理');
  });

  it('auto-selects baseline report, renders 5 buckets, and filters by bucket', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes('/api/tasks?')) {
        return {
          ok: true,
          json: async () => ({
            items: [
              { id: 20, repo_id: 5, task_type_id: 2, status: 'success', branch: 'main', head_commit: 'abc123456', created_at: '2026-11-17T10:00:00Z' },
              { id: 10, repo_id: 5, task_type_id: 2, status: 'success', branch: 'main', head_commit: '000aaa111', created_at: '2026-11-16T10:00:00Z' },
            ],
          }),
        } as Response;
      }
      if (url.includes('/api/reports/compare?base_report_id=10&target_report_id=20')) {
        return {
          ok: true,
          json: async () => sampleComparePayload,
        } as Response;
      }
      throw new Error(`Unexpected fetch URL: ${url}`);
    });
    vi.stubGlobal('fetch', fetchMock);

    render(
      <MemoryRouter initialEntries={['/reports/20?tab=compare']}>
        <ReportCompareTab targetReportId={20} meta={sampleMeta} />
      </MemoryRouter>,
    );

    await waitFor(() => {
      expect(screen.getByText('Keep pointer deref')).toBeTruthy();
      expect(screen.getByText('Unchanged uaf silent miss')).toBeTruthy();
      expect(screen.getByText('New oob in B')).toBeTruthy();
    });

    const unchangedBucketBtn = screen.getByRole('button', { name: /仅 A：代码未变未复现/ });
    fireEvent.click(unchangedBucketBtn);

    await waitFor(() => {
      expect(screen.getByText('Unchanged uaf silent miss')).toBeTruthy();
      expect(screen.queryByText('New oob in B')).toBeNull();
      expect(screen.queryByText('Keep pointer deref')).toBeNull();
    });
  });
});
