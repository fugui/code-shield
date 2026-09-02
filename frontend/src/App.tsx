import React, { useEffect, useState } from 'react';
import { BrowserRouter, Routes, Route, Link, Navigate, useLocation } from 'react-router-dom';
import { BASE_PATH, apiUrl, AUTH_TOKEN_KEY, appNavigatePath } from './config';
import ReportsOverview from './pages/ReportsOverview';
import RepoTaskHistory from './pages/RepoReviewHistory';
import PublicReportFindings from './pages/PublicReportFindings';

import ScanManagement from './pages/ScanManagement';
import TaskTypeManagement from './pages/TaskTypeManagement';
import SystemDebug from './pages/SystemDebug';
import UniversalCampaignPage from './pages/UniversalCampaignPage';
import Workbench from './pages/Workbench';

import { buildDynamicMenuGroups, TaskTypeMenuMeta } from './menu';
import { ToastProvider } from './components/Toast';
import { UnifiedLogin, ConfirmProvider, UserMenu, useCurrentUser, setupFetchInterceptor } from '@code/common';

// Setup unified global fetch interceptor
setupFetchInterceptor({ appPrefix: '/shield' });

const PrivateRoute = ({ children }: { children: JSX.Element }) => {
  const token = localStorage.getItem(AUTH_TOKEN_KEY);
  if (!token) return <Navigate to={appNavigatePath("/login")} replace />;
  return children;
};

function Sidebar({ taskTypes }: { taskTypes: TaskTypeMenuMeta[] }) {
  const location = useLocation();
  const [isAdmin, setIsAdmin] = useState(false);

  useEffect(() => {
    fetch('/api/me')
      .then(res => res.ok ? res.json() : null)
      .then(data => {
        if (data) {
          const isShieldAdmin = Array.isArray(data.roles) && (data.roles.includes('super_admin') || data.roles.includes('shield_admin'));
          setIsAdmin(isShieldAdmin);
        }
      })
      .catch(() => {});
  }, []);

  const menuGroups = buildDynamicMenuGroups(taskTypes);

  return (
    <aside style={{ width: '260px', background: 'var(--card-bg)', borderRight: '1px solid var(--border-color)', display: 'flex', flexDirection: 'column', height: '100vh', position: 'sticky', top: 0 }}>
      <div style={{ height: '70px', padding: '0 1.5rem', borderBottom: '1px solid var(--border-color)', display: 'flex', alignItems: 'center', gap: '0.75rem', boxSizing: 'border-box' }}>
        <img src={apiUrl('/assets/madun-logo.png')} alt="码盾" style={{ width: '34px', height: '34px', objectFit: 'contain', flexShrink: 0 }} />
        <div style={{ display: 'flex', flexDirection: 'column', lineHeight: 1.2 }}>
          <h2 style={{ margin: 0, fontSize: '1.1rem', color: 'var(--text-color)', letterSpacing: '0.5px', fontWeight: 700 }}>码盾，守护代码质量</h2>
          <span style={{ fontSize: '0.7rem', color: '#94a3b8', letterSpacing: '0.3px' }}>Code Shield</span>
        </div>
      </div>

      <nav style={{ padding: '1rem 1rem', display: 'flex', flexDirection: 'column', gap: '0.25rem', flex: 1, overflowY: 'auto' }}>
        {menuGroups.map((group, groupIdx) => {
          if (group.adminOnly && !isAdmin) return null;
          return (
            <React.Fragment key={group.title}>
              {groupIdx > 0 && (
                <div style={{ height: '1px', background: 'var(--border-color)', margin: '0.75rem 0.5rem' }} />
              )}
              <div style={{ padding: '0.5rem 0.75rem 0.25rem', fontSize: '0.7rem', fontWeight: 600, color: '#94a3b8', textTransform: 'uppercase' as const, letterSpacing: '0.5px' }}>
                {group.title}
              </div>
              {group.items.map(item => {
                const itemPath = appNavigatePath(item.path);
                const isActive = location.pathname === itemPath || location.pathname.startsWith(itemPath + '/');
                return (
                  <Link key={item.path} to={itemPath} style={{
                    display: 'flex', alignItems: 'center', gap: '0.75rem', padding: '0.6rem 0.75rem',
                    borderRadius: '8px', textDecoration: 'none',
                    color: isActive ? 'var(--primary-color)' : '#64748b',
                    background: isActive ? 'rgba(37, 99, 235, 0.08)' : 'transparent',
                    fontWeight: isActive ? 600 : 500,
                    fontSize: '0.9rem',
                    transition: 'all 0.2s'
                  }}>
                    {item.icon && (
                      <svg width="18" height="18" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                        <path d={item.icon}></path>
                      </svg>
                    )}
                    {item.label}
                  </Link>
                );
              })}
            </React.Fragment>
          );
        })}
      </nav>
    </aside>
  );
}

export const EmbeddedContext = React.createContext(false);

function MainLayout({ children, taskTypes }: { children: React.ReactNode; taskTypes: TaskTypeMenuMeta[] }) {
  const location = useLocation();
  const isEmbedded = React.useContext(EmbeddedContext);
  const { user, logout } = useCurrentUser({ tokenKey: AUTH_TOKEN_KEY });

  // Don't show sidebar on login page, OAuth callback, or public read-only pages
  const isLoginPath = location.pathname.endsWith('/login');
  const isPublicPath = location.pathname.includes('/public/');
  const isOAuthPath = location.pathname.includes('/oauth2/');
  if (isLoginPath || isPublicPath || isOAuthPath) {
    return <>{children}</>;
  }

  return (
    <div className={isEmbedded ? "embedded" : ""} style={{ display: 'flex', minHeight: isEmbedded ? 'auto' : '100vh', background: 'var(--bg-color)', flex: 1 }}>
      {!isEmbedded && <Sidebar taskTypes={taskTypes} />}
      <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
        {!isEmbedded && (
          <header style={{ height: '70px', background: 'var(--card-bg)', borderBottom: '1px solid var(--border-color)', display: 'flex', alignItems: 'center', padding: '0 2rem', justifyContent: 'space-between', zIndex: 10 }}>
            <h1 style={{ fontSize: '1.25rem', margin: 0, fontWeight: 600 }}>
              {(() => {
                const relativePath = location.pathname.startsWith(BASE_PATH)
                  ? location.pathname.slice(BASE_PATH.length)
                  : location.pathname;
                if (relativePath.startsWith('/workbench')) return '个人工作台';

                if (relativePath.startsWith('/reports/repo') || relativePath.startsWith('/tasks/repo')) return '历史报告';
                if (relativePath.startsWith('/reports') || relativePath.startsWith('/tasks')) return '报告概览';

                // 动态匹配专项分析路由标题
                if (relativePath.startsWith('/analysis/')) {
                  const subKey = relativePath.replace('/analysis/', '').split('/')[0];
                  const matched = taskTypes.find(t => t.campaign_path === subKey || t.name === subKey);
                  if (matched) {
                    return `${matched.display_name}专项`;
                  }
                  return '专项分析治理';
                }

                if (relativePath.startsWith('/admin/scan')) return '扫描任务管理';
                if (relativePath.startsWith('/admin/task-types')) return '任务类型管理';
                if (relativePath.startsWith('/admin/debug')) return '系统性能与堆栈诊断';
                if (relativePath.startsWith('/admin/teams') || relativePath.startsWith('/teams')) return '团队与代码仓管理';
                if (relativePath.startsWith('/admin/users')) return '用户管理';
                if (relativePath.startsWith('/admin/activity')) return '执行日志';
                if (relativePath.startsWith('/admin')) return '管理中心';
                return '报告概览';
              })()}
            </h1>
            <div style={{ display: 'flex', gap: '1.5rem', alignItems: 'center' }}>
              <UserMenu user={user} onLogout={logout} />
            </div>
          </header>
        )}
        <main style={{ flex: 1, padding: '2rem', overflowY: 'auto' }}>
            {children}
        </main>
      </div>
    </div>
  );
}

function AppContent() {
  const [taskTypes, setTaskTypes] = useState<TaskTypeMenuMeta[]>(() => {
    try {
      const cached = sessionStorage.getItem('shield_task_types_cache');
      if (cached) {
        const parsed = JSON.parse(cached);
        if (Array.isArray(parsed) && parsed.length > 0) return parsed;
      }
    } catch (_) { /* ignore cache read errors */ }
    return [];
  });

  const loadTaskTypes = () => {
    fetch('/api/task-types?active_only=true')
      .then(res => res.ok ? res.json() : [])
      .then(data => {
        if (Array.isArray(data)) {
          setTaskTypes(data);
          try { sessionStorage.setItem('shield_task_types_cache', JSON.stringify(data)); } catch (_) { /* ignore quota/security errors */ }
        }
      })
      .catch(() => { /* ignore load errors, keep empty list */ });
  };

  useEffect(() => {
    loadTaskTypes();

    window.addEventListener('shield-task-types-changed', loadTaskTypes);
    window.addEventListener('auth-change', loadTaskTypes);
    return () => {
      window.removeEventListener('shield-task-types-changed', loadTaskTypes);
      window.removeEventListener('auth-change', loadTaskTypes);
    };
  }, []);

  return (
    <ConfirmProvider>
    <div style={{ minHeight: '100vh', display: 'flex', flexDirection: 'column' }}>
      <ToastProvider>
        <MainLayout taskTypes={taskTypes}>
          <Routes>
            <Route
              path="/login"
              element={
                localStorage.getItem(AUTH_TOKEN_KEY) ? (
                  <Navigate to={appNavigatePath("/")} replace />
                ) : (
                  <UnifiedLogin
                    systemName="Code-Shield 代码卫士"
                    systemSubtitle="企业级代码质量与漏洞扫描平台"
                    systemDesc="集成多维度大模型代码分析、专项缺陷攻关与自动化质量防护体系"
                    onLoginSuccess={() => {
                      window.location.href = appNavigatePath('/');
                    }}
                  />
                )
              }
            />

            <Route path="/" element={<Navigate to={appNavigatePath("/reports")} replace />} />

            {/* 个人工作台 */}
            <Route path="/workbench" element={<PrivateRoute><Workbench /></PrivateRoute>} />

            {/* 报告中心 */}
            <Route path="/reports" element={<PrivateRoute><ReportsOverview /></PrivateRoute>} />
            <Route path="/reports/repo/:repoId" element={<PrivateRoute><RepoTaskHistory /></PrivateRoute>} />

            {/* 通用参数化专项分析治理路由 (适配所有内置及自定义专项) */}
            <Route path="/analysis/:campaignKey" element={<PrivateRoute><UniversalCampaignPage /></PrivateRoute>} />

            {/* 管理中心 */}
            <Route path="/admin/scan" element={<PrivateRoute><ScanManagement /></PrivateRoute>} />
            <Route path="/admin/scan/:tab" element={<PrivateRoute><ScanManagement /></PrivateRoute>} />
            <Route path="/admin/task-types" element={<PrivateRoute><TaskTypeManagement /></PrivateRoute>} />
            <Route path="/admin/debug" element={<PrivateRoute><SystemDebug /></PrivateRoute>} />
            <Route path="/admin/activity" element={<Navigate to={appNavigatePath("/admin/scan/logs")} replace />} />

            {/* 报告独立视图路由（受保护） */}
            <Route path="/public/report/:reportId" element={<PrivateRoute><PublicReportFindings /></PrivateRoute>} />
            <Route path="/public/reports/:reportId" element={<PrivateRoute><PublicReportFindings /></PrivateRoute>} />
            <Route path="/reports/task/:reportId" element={<PrivateRoute><PublicReportFindings /></PrivateRoute>} />
            <Route path="/reports/tasks/:reportId" element={<PrivateRoute><PublicReportFindings /></PrivateRoute>} />

            {/* 兼容旧路由重定向 */}
            <Route path="/tasks" element={<Navigate to={appNavigatePath("/reports")} replace />} />
            <Route path="/tasks/*" element={<Navigate to={appNavigatePath("/reports")} replace />} />
            <Route path="/issues" element={<Navigate to={appNavigatePath("/reports")} replace />} />
            <Route path="/config" element={<Navigate to={appNavigatePath("/admin/scan")} replace />} />
            <Route path="/config/*" element={<Navigate to={appNavigatePath("/admin/scan")} replace />} />
          </Routes>
        </MainLayout>
      </ToastProvider>
    </div>
    </ConfirmProvider>
  );
}

interface AppProps {
  isEmbedded?: boolean;
}

function App({ isEmbedded = false }: AppProps) {
  const isEmbeddedMode = isEmbedded || !!window.__POWERED_BY_PORTAL__;

  if (isEmbeddedMode) {
    return (
      <EmbeddedContext.Provider value={true}>
        <AppContent />
      </EmbeddedContext.Provider>
    );
  }

  return (
    <BrowserRouter basename={BASE_PATH}>
      <EmbeddedContext.Provider value={false}>
        <AppContent />
      </EmbeddedContext.Provider>
    </BrowserRouter>
  );
}

export default App;
