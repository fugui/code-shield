import { render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { ToastProvider } from '../Toast';
import FindingCard from './FindingCard';
import { resetAIFixURLCache } from '../../utils/reportUtils';
import { TaskFindingItem } from '../../types/report';

const template = 'https://starmind-dev.sicarrier.com/shield-fix/fixdefect?id={defect_id}';

const baseFinding: TaskFindingItem = {
  id: 1,
  title: 'Example defect',
  detail: 'Example detail',
  severity: 'critical',
  category: 'memory_leak',
  file_path: 'src/example.go',
  line_number: '12',
  status: 'open',
  status_display: '待处理',
  code_snippet: 'old := value',
} as TaskFindingItem;

function renderFindingCard(finding: TaskFindingItem, governanceMode: 'full_ledger' | 'entity_assessment' = 'full_ledger') {
  return render(
    <ToastProvider>
      <FindingCard finding={finding} governanceMode={governanceMode} />
    </ToastProvider>
  );
}

function mockFrontendConfig(fixURL?: string) {
  vi.stubGlobal('fetch', vi.fn(async () => ({
    ok: true,
    json: async () => ({ ai_fix: fixURL ? { fix_url: fixURL } : undefined }),
  })));
}

describe('FindingCard AI fix action', () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
    localStorage.clear();
    resetAIFixURLCache();
  });

  it('enables a fixable finding and opens the configured single-defect URL', async () => {
    mockFrontendConfig(template);
    renderFindingCard({ ...baseFinding, defect_id: 9182 });

    const action = await screen.findByTitle('使用 AI 修复该缺陷');
    await waitFor(() => expect(action.getAttribute('href')).toBe(
      'https://starmind-dev.sicarrier.com/shield-fix/fixdefect?id=9182'
    ));
    expect(action.getAttribute('target')).toBe('_blank');
    expect(action.getAttribute('rel')).toBe('noopener noreferrer');
    expect(fetch).toHaveBeenCalledWith('/api/config/frontend', expect.anything());
  });

  it('disables the action when defect id is missing', async () => {
    mockFrontendConfig(template);
    renderFindingCard(baseFinding);

    const action = await screen.findByTitle('尚未关联缺陷台账，暂不支持 AI 修复');
    expect(action.getAttribute('aria-disabled')).toBe('true');
    expect(action.getAttribute('href')).toBeNull();
  });

  it.each(['pass', 'fail', 'resolved', 'closed', 'invalid'])('disables the action for %s findings', async (status) => {
    mockFrontendConfig(template);
    renderFindingCard({ ...baseFinding, defect_id: 1, status });

    const action = await screen.findByTitle('当前缺陷状态不可修复');
    expect(action.getAttribute('aria-disabled')).toBe('true');
    expect(action.getAttribute('href')).toBeNull();
  });

  it('disables the action when fix URL is missing', async () => {
    mockFrontendConfig(undefined);
    renderFindingCard({ ...baseFinding, defect_id: 1 });

    const action = await screen.findByTitle('AI 修复配置无效');
    expect(action.getAttribute('aria-disabled')).toBe('true');
    expect(action.getAttribute('href')).toBeNull();
  });

  it('hides the action in entity assessment mode', () => {
    mockFrontendConfig(template);
    renderFindingCard({ ...baseFinding, defect_id: 1 }, 'entity_assessment');
    expect(screen.queryByText('🤖 AI修复')).toBeNull();
    expect(fetch).not.toHaveBeenCalled();
  });
});
