import React, { useEffect, useState } from 'react';
import { BrowserRouter, Routes, Route, Link, Navigate, useNavigate, useLocation } from 'react-router-dom';
import CodeReviewManagement from './pages/CodeReviewManagement';
import KeyIssues from './pages/KeyIssues';
import Configuration from './pages/Configuration';
import Login from './pages/Login';
import TeamManagement from './pages/TeamManagement';
import OpenSourceManagement from './pages/OpenSourceManagement';
import { ToastProvider } from './components/Toast';

// Setup global fetch interceptor to inject JWT token
const originalFetch = window.fetch;
window.fetch = async (...args) => {
  const [resource, config] = args;
  const token = localStorage.getItem('token');
  if (token && resource.toString().startsWith('/api')) {
    const defaultHeaders: any = config?.headers || {};
    return originalFetch(resource, {
      ...config,
      headers: {
        ...defaultHeaders,
        Authorization: `Bearer ${token}`
      }
    });
  }
  return originalFetch(resource, config);
};

const PrivateRoute = ({ children }: { children: JSX.Element }) => {
  const token = localStorage.getItem('token');
  if (!token) return <Navigate to="/login" replace />;
  return children;
};

function AuthHeader() {
  const [user, setUser] = useState<any>(null);
  const [showPasswordModal, setShowPasswordModal] = useState(false);
  const [passwordForm, setPasswordForm] = useState({ oldPassword: '', newPassword: '' });
  const navigate = useNavigate();

  const loadUser = () => {
    const token = localStorage.getItem('token');
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
    localStorage.removeItem('token');
    window.dispatchEvent(new Event('auth-change'));
    navigate('/login');
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
        alert('密码修改成功，请重新登录。');
        setShowPasswordModal(false);
        handleLogout();
      } else {
        alert(data.error || '修改密码失败');
      }
    } catch (err) {
      console.error(err);
      alert('发生网络错误');
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
          {user.username.charAt(0).toUpperCase()}
        </div>
        <div style={{ textAlign: 'left', display: 'flex', flexDirection: 'column' }}>
          <span style={{ fontSize: '0.875rem', fontWeight: 600, color: 'var(--text-color)', lineHeight: 1.2 }}>{user.username}</span>
          <span style={{ fontSize: '0.75rem', color: '#64748b' }}>Admin</span>
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
                <input required type="password" value={passwordForm.oldPassword} onChange={e => setPasswordForm({...passwordForm, oldPassword: e.target.value})} style={{ width: '100%', padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--bg-color)', color: 'var(--text-color)', boxSizing: 'border-box' }} />
              </div>
              <div>
                <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem', color: 'var(--text-color)' }}>新密码</label>
                <input required type="password" value={passwordForm.newPassword} onChange={e => setPasswordForm({...passwordForm, newPassword: e.target.value})} style={{ width: '100%', padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--border-color)', background: 'var(--bg-color)', color: 'var(--text-color)', boxSizing: 'border-box' }} />
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
  
  const navItems = [
    { path: '/reviews', label: '代码检视', icon: 'M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2m-3 7h3m-3 4h3m-6-4h.01M9 16h.01' },
    { path: '/issues', label: '核心问题', icon: 'M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z' },
    { path: '/opensource', label: '开源管理', icon: 'M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4' },
    { path: '/teams', label: '团队管理', icon: 'M12 4.354a4 4 0 110 5.292M15 21H3v-1a6 6 0 0112 0v1zm0 0h6v-1a6 6 0 00-9-5.197M13 7a4 4 0 11-8 0 4 4 0 018 0z' },
    { path: '/config', label: '系统管理', icon: 'M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z M15 12a3 3 0 11-6 0 3 3 0 016 0z' }
  ];

  return (
    <aside style={{ width: '260px', background: 'var(--card-bg)', borderRight: '1px solid var(--border-color)', display: 'flex', flexDirection: 'column', height: '100vh', position: 'sticky', top: 0 }}>
      <div style={{ padding: '1.25rem 1.5rem', borderBottom: '1px solid var(--border-color)', display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
        <img src="/madun-logo.png" alt="码盾" style={{ width: '36px', height: '36px', borderRadius: '8px', objectFit: 'cover', flexShrink: 0 }} />
        <div style={{ display: 'flex', flexDirection: 'column', lineHeight: 1.2 }}>
          <h2 style={{ margin: 0, fontSize: '1.2rem', color: 'var(--text-color)', letterSpacing: '0.5px', fontWeight: 700 }}>码盾</h2>
          <span style={{ fontSize: '0.7rem', color: '#94a3b8', letterSpacing: '0.3px' }}>Code Shield</span>
        </div>
      </div>
      
      <nav style={{ padding: '1.5rem 1rem', display: 'flex', flexDirection: 'column', gap: '0.5rem', flex: 1 }}>
        
        {navItems.map(item => {
          const isActive = location.pathname === item.path || location.pathname.startsWith(item.path + '/');
          return (
            <Link key={item.path} to={item.path} style={{ 
              display: 'flex', alignItems: 'center', gap: '0.75rem', padding: '0.75rem', 
              borderRadius: '8px', textDecoration: 'none', 
              color: isActive ? 'var(--primary-color)' : '#64748b',
              background: isActive ? 'rgba(37, 99, 235, 0.08)' : 'transparent',
              fontWeight: isActive ? 600 : 500,
              transition: 'all 0.2s'
            }}>
              <svg width="20" height="20" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <path d={item.icon}></path>
              </svg>
              {item.label}
            </Link>
          );
        })}
      </nav>
    </aside>
  );
}

function MainLayout({ children }: { children: React.ReactNode }) {
  const location = useLocation();
  // Don't show sidebar on login page
  if (location.pathname === '/login') {
    return <>{children}</>;
  }

  return (
    <div style={{ display: 'flex', minHeight: '100vh', background: 'var(--bg-color)' }}>
      <Sidebar />
      <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
        <header style={{ height: '70px', background: 'var(--card-bg)', borderBottom: '1px solid var(--border-color)', display: 'flex', alignItems: 'center', padding: '0 2rem', justifyContent: 'space-between', zIndex: 10 }}>
          <h1 style={{ fontSize: '1.25rem', margin: 0, fontWeight: 600 }}>
            {location.pathname.startsWith('/reviews') ? '代码检视' : location.pathname === '/opensource' ? '开源管理' : location.pathname === '/config' ? '系统管理' : location.pathname.startsWith('/teams') ? '团队组织架构与代码仓配置' : '核心问题追踪'}
          </h1>
          <div style={{ display: 'flex', gap: '1.5rem', alignItems: 'center' }}>
            <AuthHeader />
          </div>
        </header>
        <main style={{ flex: 1, padding: '2rem', overflowY: 'auto' }}>
          <div style={{ maxWidth: '1200px', margin: '0 auto' }}>
            {children}
          </div>
        </main>
      </div>
    </div>
  );
}

function App() {
  return (
    <BrowserRouter>
      <ToastProvider>
        <MainLayout>
          <Routes>
            <Route path="/login" element={<Login />} />
            <Route path="/" element={<Navigate to="/reviews" replace />} />
            <Route path="/reviews" element={<PrivateRoute><CodeReviewManagement /></PrivateRoute>} />
            <Route path="/reviews/:tab" element={<PrivateRoute><CodeReviewManagement /></PrivateRoute>} />
            <Route path="/opensource" element={<PrivateRoute><OpenSourceManagement /></PrivateRoute>} />
            <Route path="/issues" element={<PrivateRoute><KeyIssues /></PrivateRoute>} />
            <Route path="/teams" element={<PrivateRoute><TeamManagement /></PrivateRoute>} />
            <Route path="/teams/:tab" element={<PrivateRoute><TeamManagement /></PrivateRoute>} />
            <Route path="/config" element={<PrivateRoute><Configuration /></PrivateRoute>} />
          </Routes>
        </MainLayout>
      </ToastProvider>
    </BrowserRouter>
  );
}

export default App;
