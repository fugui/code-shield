import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import AuditingWorkspace from './AuditingWorkspace';
import { ToastProvider } from './Toast';
import { resetAIFixURLCache } from '../utils/reportUtils';
import { Finding } from './AuditingWorkspace';

const template = 'https://starmind-dev.sicarrier.com/shield-fix/fixdefect?id={defect_id}';

const baseFinding: Finding = {
  id: 43935,
  repo_url: 'https://code.example.com/group/repo.git',
  repo_branch: 'release/1.2',
  title: 'Potential null pointer dereference',
  category: '内存安全',
  file_path: 'src/example.cpp',
  line_number: '42',
  severity: '严重',
  status: 'open',
  detail: 'The pointer may be null.',
  hunter_claim: 'The pointer can be null on the concurrent cleanup path.',
  challenger_arg: 'The caller performs a null guard before invoking this function.',
  judge_verdict: 'Judge confirms the concurrent cleanup path lacks protection.',
  trigger_line: 'diskLoop->stop()',
  scope_symbol: 'ControlZone::stop',
};

function jsonResponse(body: unknown, ok = true) {
  return {
    ok,
    json: async () => body,
  };
}

function renderWorkspace(governanceMode: 'full_ledger' | 'entity_assessment' = 'full_ledger', isOpen = true) {
  return render(
    <ToastProvider>
      <MemoryRouter
        initialEntries={[
          {
            pathname: '/analysis/coredump',
            search: '?repoId=32&findingId=43935',
          },
        ]}
      >
        <AuditingWorkspace
          isOpen={isOpen}
          onClose={() => {}}
          repoId={32}
          repoName="example-repo"
          apiPrefix="/api/analysis/coredump"
          workspaceType="coredump"
          governanceMode={governanceMode}
        />
      </MemoryRouter>
    </ToastProvider>
  );
}

function mockWorkspaceFetch(finding: Finding, includeFixURL = true) {
  let configRequested = false;
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const url = input.toString();
    if (url.includes('/api/me')) {
      return jsonResponse({ id: 1, name: 'Tester' });
    }
    if (url.includes('/api/tasks?')) {
      return jsonResponse({ items: [] });
    }
    if (url.includes('/api/config/frontend')) {
      configRequested = true;
      return jsonResponse({
        ai_fix: includeFixURL ? { fix_url: template } : undefined,
      });
    }
    if (url.includes('/api/analysis/coredump/findings?')) {
      return jsonResponse({
        items: [finding],
        total: 1,
        page: 1,
        page_size: 10,
      });
    }
    return jsonResponse({ error: 'not found' }, false);
  });
  vi.stubGlobal('fetch', fetchMock);
  return { fetchMock, isConfigRequested: () => configRequested };
}

describe('AuditingWorkspace AI fix action', () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
    localStorage.clear();
    resetAIFixURLCache();
  });

  it('opens the configured single-defect URL for a fixable finding', async () => {
    mockWorkspaceFetch(baseFinding);
    renderWorkspace();

    const action = await screen.findByTitle('使用 AI 修复该缺陷；不会改变当前流转状态');
    await waitFor(() => expect(action.getAttribute('href')).toBe(
      'https://starmind-dev.sicarrier.com/shield-fix/fixdefect?id=43935'
    ));
    expect(action.getAttribute('target')).toBe('_blank');
    expect(action.getAttribute('rel')).toBe('noopener noreferrer');
  });

  it('disables the action for a non-fixable finding', async () => {
    mockWorkspaceFetch({ ...baseFinding, status: 'resolved' });
    renderWorkspace();

    const action = await screen.findByTitle('当前缺陷状态不可修复');
    await waitFor(() => expect(screen.getByText('❌ Potential null pointer dereference')).toBeTruthy());
    expect(action.getAttribute('aria-disabled')).toBe('true');
    expect(action.getAttribute('href')).toBeNull();
  });

  it('hides the action in entity assessment mode', async () => {
    const { isConfigRequested } = mockWorkspaceFetch(baseFinding);
    renderWorkspace('entity_assessment');

    await waitFor(() => expect(screen.getByText('❌ Potential null pointer dereference')).toBeTruthy());
    expect(screen.queryByText('🤖 AI修复')).toBeNull();
    expect(isConfigRequested()).toBe(false);
  });
});

describe('AuditingWorkspace filter reset', () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
    localStorage.clear();
    resetAIFixURLCache();
  });

  it('resets all dropdown filters when reopened', async () => {
    mockWorkspaceFetch(baseFinding);
    const firstRender = renderWorkspace();
    await firstRender.findByTitle('使用 AI 修复该缺陷；不会改变当前流转状态');
    const [severitySelect, statusSelect, categorySelect] =
      (await screen.findAllByRole('combobox')) as HTMLSelectElement[];

    fireEvent.change(severitySelect, { target: { value: '严重' } });
    fireEvent.change(statusSelect, { target: { value: 'open' } });
    fireEvent.change(categorySelect, { target: { value: '内存安全' } });

    await waitFor(() => {
      expect(severitySelect.value).toBe('严重');
      expect(statusSelect.value).toBe('open');
      expect(categorySelect.value).toBe('内存安全');
    });

    await act(async () => {
      firstRender.rerender(
        <ToastProvider>
          <MemoryRouter initialEntries={[{ pathname: '/analysis/coredump', search: '?repoId=32&findingId=43935' }]}>
            <AuditingWorkspace
              isOpen={false}
              onClose={() => {}}
              repoId={32}
              repoName="example-repo"
              apiPrefix="/api/analysis/coredump"
              workspaceType="coredump"
              governanceMode="full_ledger"
            />
          </MemoryRouter>
        </ToastProvider>
      );
    });
    await act(async () => {
      firstRender.rerender(
        <ToastProvider>
          <MemoryRouter initialEntries={[{ pathname: '/analysis/coredump', search: '?repoId=32&findingId=43935' }]}>
            <AuditingWorkspace
              isOpen
              onClose={() => {}}
              repoId={32}
              repoName="example-repo"
              apiPrefix="/api/analysis/coredump"
              workspaceType="coredump"
              governanceMode="full_ledger"
            />
          </MemoryRouter>
        </ToastProvider>
      );
    });

    await waitFor(() => {
      const reopenedSelects = screen.getAllByRole('combobox') as HTMLSelectElement[];
      expect(reopenedSelects[0].value).toBe('');
      expect(reopenedSelects[1].value).toBe('');
      expect(reopenedSelects[2].value).toBe('');
    });
  });
});

describe('AuditingWorkspace debate evidence', () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
    localStorage.clear();
    resetAIFixURLCache();
  });

  it('shows Hunter first and keeps Challenger evidence visible', async () => {
    mockWorkspaceFetch(baseFinding);
    renderWorkspace();
    const hunterClaim = baseFinding.hunter_claim ?? '';
    const challengerArg = baseFinding.challenger_arg ?? '';

    expect(await screen.findByText('📌 缺陷机理与成因剖析')).toBeTruthy();
    expect(screen.getByText(hunterClaim)).toBeTruthy();
    fireEvent.click(screen.getByText('🛡️ AI 智能体仲裁与对抗事实链'));
    expect(screen.getByText('⚖️ Challenger (对抗辩护论证与抗辩证据):')).toBeTruthy();
    expect(screen.getByText(challengerArg)).toBeTruthy();
  });
});

describe('AuditingWorkspace source link', () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
    localStorage.clear();
    resetAIFixURLCache();
  });

  it('opens the finding source using repo metadata', async () => {
    mockWorkspaceFetch(baseFinding);
    renderWorkspace();

    const sourceLink = await screen.findByTitle('在代码仓中查看源码');
    expect(sourceLink.getAttribute('href')).toBe(
      'https://code.example.com/group/repo.git/files?ref=release%2F1.2&filePath=src%2Fexample.cpp&isFile=true#L42'
    );
    expect(sourceLink.getAttribute('target')).toBe('_blank');
    expect(sourceLink.getAttribute('rel')).toBe('noopener noreferrer');
  });

  it('does not render a source link without repository metadata', async () => {
    const findingWithoutRepo: Finding = {
      ...baseFinding,
      repo_url: undefined,
      repo_branch: undefined,
    };
    mockWorkspaceFetch(findingWithoutRepo);
    renderWorkspace();

    await screen.findByText('📌 缺陷机理与成因剖析');
    expect(screen.queryByTitle('在代码仓中查看源码')).toBeNull();
  });
});
