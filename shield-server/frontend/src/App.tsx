import React, { useEffect, useState } from 'react';
import { BrowserRouter, Routes, Route, Link, Navigate, useNavigate, useLocation } from 'react-router-dom';
import { BASE_PATH, apiUrl, AUTH_TOKEN_KEY, appNavigatePath } from './config';
import TaskManagement from './pages/CodeReviewManagement';
import RepoTaskHistory from './pages/RepoReviewHistory';
import Login from './pages/Login';
import TeamManagement from './pages/TeamManagement';
import PublicReportFindings from './pages/PublicReportFindings';
import OAuthCallback from './pages/OAuthCallback';
import ScanManagement from './pages/ScanManagement';
import TaskTypeManagement from './pages/TaskTypeManagement';
import UserManagement from './pages/UserManagement';
import ExecutionLogs from './pages/ExecutionLogs';
import UTAnalysis from './pages/UTAnalysis';
import { menuGroups } from './menu';
import { ToastProvider, useToast } from './components/Toast';

// Setup global fetch interceptor to inject JWT token and prepend BASE_PATH
const originalFetch = window.fetch;
window.fetch = async (...args) => {
  let [resource, config] = args;
  const token = localStorage.getItem(AUTH_TOKEN_KEY);
  const url = resource.toString();

  // Auto-prepend BASE_PATH for absolute /api calls
  if (url.startsWith('/api')) {
    resource = apiUrl(url);
  }

  if (token && resource.toString().includes('/api')) {
    const defaultHeaders: any = config?.headers || {};
    var res = await originalFetch(resource, {
      ...config,
      headers: {
        ...defaultHeaders,
        Authorization: `Bearer ${token}`
      }
    });
  } else {
    var res = await originalFetch(resource, config);
  }

  // Handle Token Automatic Renewal
  if (res.ok && resource.toString().includes('/api')) {
    const newToken = res.headers.get('X-Refresh-Token');
    if (newToken) {
      localStorage.setItem(AUTH_TOKEN_KEY, newToken);
      window.dispatchEvent(new Event('auth-change'));
    }
  }

  // Intercept 401 to handle expired tokens globally (only for system /api calls)
  if (res.status === 401 && resource.toString().includes('/api')) {
    localStorage.removeItem(AUTH_TOKEN_KEY);
    window.dispatchEvent(new Event('auth-change'));
    const loginPath = BASE_PATH + '/login';
    if (window.location.pathname !== loginPath) {
      window.location.href = loginPath;
    }
  }

  return res;
};

const PrivateRoute = ({ children }: { children: JSX.Element }) => {
  const token = localStorage.getItem(AUTH_TOKEN_KEY);
  if (!token) return <Navigate to={appNavigatePath("/login")} replace />;
  return children;
};

function AuthHeader() {
  const { showToast } = useToast();
  const [user, setUser] = useState<any>(null);
  const [showPasswordModal, setShowPasswordModal] = useState(false);
  const [passwordForm, setPasswordForm] = useState({ oldPassword: '', newPassword: '' });
  const navigate = useNavigate();

  const loadUser = () => {
    const token = localStorage.getItem(AUTH_TOKEN_KEY);
    if (token) {
      fetch('/api/me')
        .then(res => res.ok ? res.json() : null)
        .then(data => {
          if (data) setUser(data);
          else handleLogout(); // Invalid token
        })
        .catch(() => handleLogout());
    } else {
      setUser(null);
    }
  };

  useEffect(() => {
    loadUser();
    window.addEventListener('auth-change', loadUser);
    return () => window.removeEventListener('auth-change', loadUser);
  }, []);

  const handleLogout = () => {
    localStorage.removeItem(AUTH_TOKEN_KEY);
    window.dispatchEvent(new Event('auth-change'));
    navigate(appNavigatePath('/login'));
  };

  const [showDropdown, setShowDropdown] = useState(false);

  // Close dropdown when clicking outside
  useEffect(() => {
    const handleClickOutside = () => setShowDropdown(false);
    window.addEventListener('click', handleClickOutside);
    return () => window.removeEventListener('click', handleClickOutside);
  }, []);

  const handlePasswordChange = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      const res = await fetch('/api/password', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ old_password: passwordForm.oldPassword, new_password: passwordForm.newPassword })
      });
      const data = await res.json();
      if (res.ok) {
        showToast('密码修改成功，即将退出登录…', 'success');
        setShowPasswordModal(false);
        setTimeout(handleLogout, 1500);
      } else {
        showToast(data.error || '修改密码失败', 'error');
      }
    } catch (err) {
      console.error(err);
      showToast('发生网络错误', 'error');
    }
  };

  if (!user) return null;

  return (
    <div style={{ position: 'relative' }}>
      <button
        onClick={(e) => {
          e.stopPropagation();
          setShowDropdown(!showDropdown);
        }}
        style={{
          display: 'flex', alignItems: 'center', gap: '0.75rem', background: 'transparent',
          border: 'none', cursor: 'pointer', padding: '0.5rem', borderRadius: '8px',
          transition: 'background-color 0.2s'
        }}
        onMouseEnter={e => e.currentTarget.style.backgroundColor = 'var(--bg-color)'}
        onMouseLeave={e => e.currentTarget.style.backgroundColor = 'transparent'}
      >
        <div style={{ width: '36px', height: '36px', borderRadius: '50%', background: 'var(--primary-color)', color: 'white', display: 'flex', alignItems: 'center', justifyContent: 'center', fontWeight: 600, fontSize: '1rem' }}>
          {(user.name || user.email || user.username || '').charAt(0).toUpperCase()}
        </div>
        <div style={{ textAlign: 'left', display: 'flex', flexDirection: 'column' }}>
          <span style={{ fontSize: '0.875rem', fontWeight: 600, color: 'var(--text-color)', lineHeight: 1.2 }}>{user.name || user.email || user.username}</span>
          <span style={{ fontSize: '0.75rem', color: '#64748b' }}>{user.is_admin ? '管理员' : '普通用户'}</span>
        </div>
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="#64748b" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={{ marginLeft: '0.5rem', transform: showDropdown ? 'rotate(180deg)' : 'none', transition: 'transform 0.2s' }}>
          <polyline points="6 9 12 15 18 9"></polyline>
        </svg>
      </button>

      {showDropdown && (
        <div style={{
          position: 'absolute', top: '100%', right: 0, marginTop: '0.5rem', width: '200px',
          background: 'white', borderRadius: '8px', border: '1px solid var(--border-color)',
          boxShadow: '0 10px 15px -3px rgba(0,0,0,0.1)', overflow: 'hidden', zIndex: 100
        }}>
          <div style={{ padding: '0.5rem' }}>
            <button
              onClick={(e) => { e.stopPropagation(); setShowDropdown(false); setShowPasswordModal(true); }}
              style={{ width: '100%', textAlign: 'left', padding: '0.75rem 1rem', background: 'transparent', border: 'none', cursor: 'pointer', fontSize: '0.875rem', color: 'var(--text-color)', display: 'flex', alignItems: 'center', gap: '0.5rem', borderRadius: '4px' }}
              onMouseEnter={e => e.currentTarget.style.backgroundColor = 'var(--bg-color)'}
              onMouseLeave={e => e.currentTarget.style.backgroundColor = 'transparent'}
            >
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><rect x="3" y="11" width="18" height="11" rx="2" ry="2"></rect><path d="M7 11V7a5 5 0 0 1 10 0v4"></path></svg>
              修改密码
            </button>
            <div style={{ height: '1px', background: 'var(--border-color)', margin: '0.25rem 0' }}></div>
            <button
              onClick={(e) => { e.stopPropagation(); handleLogout(); }}
              style={{ width: '100%', textAlign: 'left', padding: '0.75rem 1rem', background: 'transparent', border: 'none', cursor: 'pointer', fontSize: '0.875rem', color: 'var(--danger-color)', display: 'flex', alignItems: 'center', gap: '0.5rem', borderRadius: '4px' }}
              onMouseEnter={e => e.currentTarget.style.backgroundColor = 'rgba(239, 68, 68, 0.1)'}
              onMouseLeave={e => e.currentTarget.style.backgroundColor = 'transparent'}
            >
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"></path><polyline points="16 17 21 12 16 7"></polyline><line x1="21" y1="12" x2="9" y2="12"></line></svg>
              退出登录
            </button>
          </div>
        </div>
      )}

      {showPasswordModal && (
        <div style={{ position: 'fixed', top: 0, left: 0, width: '100%', height: '100%', background: 'rgba(0,0,0,0.5)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 }}>
          <div className="card" style={{ width: '400px', maxWidth: '90%' }}>
            <h3 style={{ marginTop: 0, marginBottom: '1.5rem' }}>修改密码</h3>
            <form onSubmit={handlePasswordChange} style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
              <div>
                <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem', color: 'var(--text-color)' }}>当前密码</label>
                <input required type="password" value={passwordForm.oldPassword} onChange={e => setPasswordForm({ ...passwordForm, oldPassword: e.target.value })} style={{ width: '100%', padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--bg-color)', color: 'var(--text-color)', boxSizing: 'border-box' }} />
              </div>
              <div>
                <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem', color: 'var(--text-color)' }}>新密码</label>
                <input required type="password" value={passwordForm.newPassword} onChange={e => setPasswordForm({ ...passwordForm, newPassword: e.target.value })} style={{ width: '100%', padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--bg-color)', color: 'var(--text-color)', boxSizing: 'border-box' }} />
              </div>
              <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '1rem', marginTop: '1rem' }}>
                <button type="button" onClick={() => setShowPasswordModal(false)} style={{ background: 'transparent', border: 'none', color: '#64748b', cursor: 'pointer', padding: '0.5rem 1rem' }}>取消</button>
                <button type="submit" className="btn">确认修改</button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}

function Sidebar() {
  const location = useLocation();
  const [isAdmin, setIsAdmin] = useState(false);

  useEffect(() => {
    fetch('/api/me')
      .then(res => res.ok ? res.json() : null)
      .then(data => { if (data) setIsAdmin(!!data.is_admin); })
      .catch(() => {});
  }, []);

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

function MainLayout({ children }: { children: React.ReactNode }) {
  const location = useLocation();
  const isEmbedded = React.useContext(EmbeddedContext);

  // Don't show sidebar on login page, OAuth callback, or public read-only pages
  const isLoginPath = location.pathname.endsWith('/login');
  const isPublicPath = location.pathname.includes('/public/');
  const isOAuthPath = location.pathname.includes('/oauth2/');
  if (isLoginPath || isPublicPath || isOAuthPath) {
    return <>{children}</>;
  }

  return (
    <div className={isEmbedded ? "embedded" : ""} style={{ display: 'flex', minHeight: isEmbedded ? 'auto' : '100vh', background: 'var(--bg-color)', flex: 1 }}>
      {!isEmbedded && <Sidebar />}
      <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
        {!isEmbedded && (
          <header style={{ height: '70px', background: 'var(--card-bg)', borderBottom: '1px solid var(--border-color)', display: 'flex', alignItems: 'center', padding: '0 2rem', justifyContent: 'space-between', zIndex: 10 }}>
            <h1 style={{ fontSize: '1.25rem', margin: 0, fontWeight: 600 }}>
              {(() => {
                const relativePath = location.pathname.startsWith(BASE_PATH)
                  ? location.pathname.slice(BASE_PATH.length)
                  : location.pathname;
                if (relativePath.startsWith('/reports/repo') || relativePath.startsWith('/tasks/repo')) return '历史报告';
                if (relativePath.startsWith('/reports') || relativePath.startsWith('/tasks')) return '报告概览';
                if (relativePath.startsWith('/analysis/ut') || relativePath.startsWith('/issues')) return '测试有效性分析';
                if (relativePath.startsWith('/admin/scan')) return '扫描任务管理';
                if (relativePath.startsWith('/admin/task-types')) return '任务类型管理';
                if (relativePath.startsWith('/admin/teams') || relativePath.startsWith('/teams')) return '团队与代码仓管理';
                if (relativePath.startsWith('/admin/users')) return '用户管理';
                if (relativePath.startsWith('/admin/activity')) return '执行日志';
                if (relativePath.startsWith('/admin')) return '管理中心';
                return '报告概览';
              })()}
            </h1>
            <div style={{ display: 'flex', gap: '1.5rem', alignItems: 'center' }}>
              <AuthHeader />
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
  return (
    <ToastProvider>
      <MainLayout>
        <Routes>
          <Route path="/login" element={<Login />} />
          <Route path="/oauth2/callback" element={<OAuthCallback />} />
          <Route path="/" element={<Navigate to={appNavigatePath("/reports")} replace />} />

          {/* 报告中心 */}
          <Route path="/reports" element={<PrivateRoute><TaskManagement /></PrivateRoute>} />
          <Route path="/reports/repo/:repoId" element={<PrivateRoute><RepoTaskHistory /></PrivateRoute>} />

          {/* 专项分析 */}
          <Route path="/analysis/ut" element={<PrivateRoute><UTAnalysis /></PrivateRoute>} />

          {/* 管理中心 */}
          <Route path="/admin/scan" element={<PrivateRoute><ScanManagement /></PrivateRoute>} />
          <Route path="/admin/scan/:tab" element={<PrivateRoute><ScanManagement /></PrivateRoute>} />
          <Route path="/admin/task-types" element={<PrivateRoute><TaskTypeManagement /></PrivateRoute>} />
          <Route path="/admin/teams" element={<PrivateRoute><TeamManagement /></PrivateRoute>} />
          <Route path="/admin/teams/:tab" element={<PrivateRoute><TeamManagement /></PrivateRoute>} />
          <Route path="/admin/users" element={<PrivateRoute><UserManagement /></PrivateRoute>} />
          <Route path="/admin/activity" element={<PrivateRoute><ExecutionLogs /></PrivateRoute>} />

          {/* 公开报告 */}
          <Route path="/public/report/:reportId" element={<PublicReportFindings />} />
          <Route path="/public/reports/:reportId" element={<PublicReportFindings />} />

          {/* 兼容旧路由重定向 */}
          <Route path="/tasks" element={<Navigate to={appNavigatePath("/reports")} replace />} />
          <Route path="/tasks/*" element={<Navigate to={appNavigatePath("/reports")} replace />} />
          <Route path="/issues" element={<Navigate to={appNavigatePath("/reports")} replace />} />
          <Route path="/config" element={<Navigate to={appNavigatePath("/admin/scan")} replace />} />
          <Route path="/config/*" element={<Navigate to={appNavigatePath("/admin/scan")} replace />} />
        </Routes>
      </MainLayout>
    </ToastProvider>
  );
}

interface AppProps {
  isEmbedded?: boolean;
}

function App({ isEmbedded = false }: AppProps) {
  const isEmbeddedMode = isEmbedded || !!(window as any).__POWERED_BY_PORTAL__;

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
