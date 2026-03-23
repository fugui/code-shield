import React, { useState } from 'react';
import { useNavigate } from 'react-router-dom';

function Login() {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const navigate = useNavigate();

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setLoading(true);
    
    fetch('/api/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password })
    })
    .then(async res => {
      const data = await res.json();
      if (res.ok) {
        localStorage.setItem('token', data.token);
        window.dispatchEvent(new Event('auth-change'));
        navigate('/');
      } else {
        setError(data.error || '登录失败，可能是密码错误或账号被禁用');
      }
    })
    .catch(() => setError('网络错误，请稍后重试'))
    .finally(() => setLoading(false));
  };

  return (
    <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: '100vh', background: '#0b1120', overflow: 'hidden', position: 'relative' }}>
      {/* Centered container */}
      <div style={{ display: 'flex', width: '100%', maxWidth: '1100px', minHeight: '640px', maxHeight: '90vh', borderRadius: '16px', overflow: 'hidden', boxShadow: '0 25px 60px rgba(0,0,0,0.5)', border: '1px solid rgba(255,255,255,0.06)', position: 'relative' }}>
      {/* CSS Animations */}
      <style>{`
        @keyframes gradientShift {
          0%, 100% { background-position: 0% 50%; }
          50% { background-position: 100% 50%; }
        }
        @keyframes float1 {
          0%, 100% { transform: translate(0, 0) rotate(0deg); }
          25% { transform: translate(30px, -40px) rotate(5deg); }
          50% { transform: translate(-20px, -80px) rotate(-3deg); }
          75% { transform: translate(40px, -30px) rotate(4deg); }
        }
        @keyframes float2 {
          0%, 100% { transform: translate(0, 0) rotate(0deg); }
          33% { transform: translate(-40px, 30px) rotate(-4deg); }
          66% { transform: translate(30px, -50px) rotate(6deg); }
        }
        @keyframes float3 {
          0%, 100% { transform: translate(0, 0); }
          50% { transform: translate(50px, -60px); }
        }
        @keyframes pulse {
          0%, 100% { opacity: 0.4; }
          50% { opacity: 0.8; }
        }
        @keyframes fadeInUp {
          from { opacity: 0; transform: translateY(20px); }
          to { opacity: 1; transform: translateY(0); }
        }
        @keyframes shimmer {
          0% { background-position: -200% center; }
          100% { background-position: 200% center; }
        }
        .login-input:focus {
          border-color: #3b82f6 !important;
          box-shadow: 0 0 0 3px rgba(59, 130, 246, 0.15) !important;
        }
        .login-btn:hover {
          transform: translateY(-1px);
          box-shadow: 0 8px 25px rgba(59, 130, 246, 0.45) !important;
        }
        .login-btn:active {
          transform: translateY(0);
        }
        .feature-item:hover {
          background: rgba(255,255,255,0.08) !important;
          transform: translateX(4px);
        }
      `}</style>

      {/* Left Panel — Branding & Features */}
      <div style={{
        flex: 1, display: 'flex', flexDirection: 'column', justifyContent: 'center',
        padding: '3rem', position: 'relative', overflow: 'hidden',
        background: 'linear-gradient(135deg, #0f172a 0%, #1e293b 50%, #0f172a 100%)',
        borderRadius: '16px 0 0 16px'
      }}>
        {/* Animated floating orbs */}
        <div style={{ position: 'absolute', top: '10%', left: '15%', width: '300px', height: '300px', borderRadius: '50%', background: 'radial-gradient(circle, rgba(59,130,246,0.15) 0%, transparent 70%)', animation: 'float1 20s ease-in-out infinite', pointerEvents: 'none' }} />
        <div style={{ position: 'absolute', bottom: '20%', right: '10%', width: '250px', height: '250px', borderRadius: '50%', background: 'radial-gradient(circle, rgba(139,92,246,0.12) 0%, transparent 70%)', animation: 'float2 25s ease-in-out infinite', pointerEvents: 'none' }} />
        <div style={{ position: 'absolute', top: '50%', left: '60%', width: '200px', height: '200px', borderRadius: '50%', background: 'radial-gradient(circle, rgba(6,182,212,0.1) 0%, transparent 70%)', animation: 'float3 18s ease-in-out infinite', pointerEvents: 'none' }} />

        {/* Grid pattern overlay */}
        <div style={{
          position: 'absolute', inset: 0, opacity: 0.03, pointerEvents: 'none',
          backgroundImage: 'linear-gradient(rgba(255,255,255,0.1) 1px, transparent 1px), linear-gradient(90deg, rgba(255,255,255,0.1) 1px, transparent 1px)',
          backgroundSize: '60px 60px'
        }} />

        {/* Content */}
        <div style={{ position: 'relative', zIndex: 1, maxWidth: '520px', animation: 'fadeInUp 0.8s ease' }}>
          {/* Logo */}
          <div style={{ display: 'flex', alignItems: 'center', gap: '1rem', marginBottom: '3rem' }}>
            <img src="/madun-logo.png" alt="码盾" style={{ width: '48px', height: '48px', objectFit: 'contain' }} />
            <div>
              <h1 style={{ margin: 0, fontSize: '1.75rem', fontWeight: 700, color: '#f1f5f9', letterSpacing: '1px' }}>码盾</h1>
              <span style={{ fontSize: '0.8rem', color: '#64748b', letterSpacing: '2px', textTransform: 'uppercase' }}>Code Shield</span>
            </div>
          </div>

          <h2 style={{
            fontSize: '2.25rem', fontWeight: 700, lineHeight: 1.3, margin: '0 0 1rem',
            background: 'linear-gradient(135deg, #f1f5f9 0%, #94a3b8 100%)',
            WebkitBackgroundClip: 'text', WebkitTextFillColor: 'transparent',
            backgroundClip: 'text'
          }}>
            AI 驱动的<br/>代码质量守护平台
          </h2>

          <p style={{ color: '#64748b', fontSize: '1.05rem', lineHeight: 1.7, margin: '0 0 2.5rem' }}>
            自动化代码检视 · 智能问题追踪 · 多维度质量洞察<br/>
            让每一次提交都经过 AI 的严格审视
          </p>

          {/* Feature list */}
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
            {[
              { icon: 'M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z', title: '智能安全审计', desc: '深度分析潜在漏洞与安全隐患' },
              { icon: 'M13 10V3L4 14h7v7l9-11h-7z', title: '极速检视引擎', desc: '分钟级完成全量代码扫描' },
              { icon: 'M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z', title: '质量趋势追踪', desc: '全方位代码健康度可视化' },
            ].map((f, i) => (
              <div key={i} className="feature-item" style={{
                display: 'flex', alignItems: 'center', gap: '1rem', padding: '0.875rem 1rem',
                borderRadius: '10px', background: 'rgba(255,255,255,0.04)',
                border: '1px solid rgba(255,255,255,0.06)',
                transition: 'all 0.3s ease', cursor: 'default',
                animation: `fadeInUp ${0.8 + i * 0.15}s ease`
              }}>
                <div style={{
                  width: '40px', height: '40px', borderRadius: '10px', flexShrink: 0,
                  background: `linear-gradient(135deg, ${['rgba(59,130,246,0.2)', 'rgba(139,92,246,0.2)', 'rgba(6,182,212,0.2)'][i]} 0%, transparent 100%)`,
                  display: 'flex', alignItems: 'center', justifyContent: 'center'
                }}>
                  <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke={['#3b82f6', '#8b5cf6', '#06b6d4'][i]} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                    <path d={f.icon} />
                  </svg>
                </div>
                <div>
                  <div style={{ color: '#e2e8f0', fontWeight: 600, fontSize: '0.9rem', marginBottom: '2px' }}>{f.title}</div>
                  <div style={{ color: '#64748b', fontSize: '0.8rem' }}>{f.desc}</div>
                </div>
              </div>
            ))}
          </div>
        </div>

        {/* Bottom stats bar */}
        <div style={{
          position: 'relative', zIndex: 1, marginTop: '2.5rem',
          display: 'flex', gap: '1rem', animation: 'fadeInUp 1.2s ease'
        }}>
          {[
            { num: '200+', label: '代码仓覆盖' },
            { num: '7×24', label: '持续守护' },
            { num: 'AI', label: '多模型驱动' },
          ].map((s, i) => (
            <div key={i} style={{ flex: 1, padding: '1rem', borderRadius: '8px', background: 'rgba(255,255,255,0.03)', border: '1px solid rgba(255,255,255,0.05)', textAlign: 'center' }}>
              <div style={{ color: '#3b82f6', fontSize: '1.25rem', fontWeight: 700 }}>{s.num}</div>
              <div style={{ color: '#475569', fontSize: '0.75rem', marginTop: '2px' }}>{s.label}</div>
            </div>
          ))}
        </div>
      </div>

      {/* Right Panel — Login Form */}
      <div style={{
        width: '480px', display: 'flex', flexDirection: 'column', justifyContent: 'center',
        alignItems: 'center', padding: '3rem',
        background: 'linear-gradient(180deg, #1e293b 0%, #0f172a 100%)',
        borderLeft: '1px solid rgba(255,255,255,0.06)',
        position: 'relative'
      }}>
        {/* Subtle glow on top */}
        <div style={{ position: 'absolute', top: 0, left: '50%', transform: 'translateX(-50%)', width: '200px', height: '2px', background: 'linear-gradient(90deg, transparent, #3b82f6, transparent)', animation: 'pulse 3s ease-in-out infinite' }} />

        <div style={{ width: '100%', maxWidth: '360px', animation: 'fadeInUp 0.6s ease' }}>
          {/* Shield icon */}
          <div style={{ textAlign: 'center', marginBottom: '2.5rem' }}>
            <div style={{
              width: '64px', height: '64px', borderRadius: '16px', margin: '0 auto 1.25rem',
              background: 'linear-gradient(135deg, #3b82f6 0%, #1d4ed8 100%)',
              display: 'flex', alignItems: 'center', justifyContent: 'center',
              boxShadow: '0 0 30px rgba(59,130,246,0.3), 0 0 60px rgba(59,130,246,0.1)',
              position: 'relative'
            }}>
              <svg width="30" height="30" viewBox="0 0 24 24" fill="none" stroke="white" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>
              </svg>
              {/* Pulsing ring */}
              <div style={{ position: 'absolute', inset: '-4px', borderRadius: '18px', border: '1px solid rgba(59,130,246,0.3)', animation: 'pulse 2s ease-in-out infinite' }} />
            </div>
            <h2 style={{ margin: '0 0 0.375rem', color: '#f1f5f9', fontSize: '1.5rem', fontWeight: 700 }}>欢迎回来</h2>
            <p style={{ color: '#64748b', fontSize: '0.875rem', margin: 0 }}>请登录以进入码盾管理平台</p>
          </div>

          {/* Error message */}
          {error && (
            <div style={{
              padding: '0.75rem 1rem', borderRadius: '8px', marginBottom: '1.25rem',
              background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.2)',
              color: '#fca5a5', fontSize: '0.85rem', display: 'flex', alignItems: 'center', gap: '0.5rem'
            }}>
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <circle cx="12" cy="12" r="10"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/>
              </svg>
              {error}
            </div>
          )}

          <form onSubmit={handleSubmit} style={{ display: 'flex', flexDirection: 'column', gap: '1.25rem' }}>
            <div>
              <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.8rem', color: '#94a3b8', fontWeight: 500, letterSpacing: '0.5px' }}>用户名</label>
              <div style={{ position: 'relative' }}>
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="#475569" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={{ position: 'absolute', left: '0.875rem', top: '50%', transform: 'translateY(-50%)', pointerEvents: 'none' }}>
                  <path d="M20 21v-2a4 4 0 00-4-4H8a4 4 0 00-4 4v2"/><circle cx="12" cy="7" r="4"/>
                </svg>
                <input
                  className="login-input"
                  required
                  type="text"
                  placeholder="请输入用户名"
                  value={username}
                  onChange={e => setUsername(e.target.value)}
                  style={{
                    width: '100%', padding: '0.8rem 0.875rem 0.8rem 2.75rem', borderRadius: '10px',
                    border: '1px solid rgba(255,255,255,0.08)', background: 'rgba(255,255,255,0.04)',
                    color: '#f1f5f9', fontSize: '0.9rem', boxSizing: 'border-box',
                    transition: 'all 0.25s ease', outline: 'none'
                  }}
                />
              </div>
            </div>

            <div>
              <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.8rem', color: '#94a3b8', fontWeight: 500, letterSpacing: '0.5px' }}>密码</label>
              <div style={{ position: 'relative' }}>
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="#475569" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={{ position: 'absolute', left: '0.875rem', top: '50%', transform: 'translateY(-50%)', pointerEvents: 'none' }}>
                  <rect x="3" y="11" width="18" height="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0110 0v4"/>
                </svg>
                <input
                  className="login-input"
                  required
                  type="password"
                  placeholder="请输入密码"
                  value={password}
                  onChange={e => setPassword(e.target.value)}
                  style={{
                    width: '100%', padding: '0.8rem 0.875rem 0.8rem 2.75rem', borderRadius: '10px',
                    border: '1px solid rgba(255,255,255,0.08)', background: 'rgba(255,255,255,0.04)',
                    color: '#f1f5f9', fontSize: '0.9rem', boxSizing: 'border-box',
                    transition: 'all 0.25s ease', outline: 'none'
                  }}
                />
              </div>
            </div>

            <button
              type="submit"
              className="login-btn"
              disabled={loading}
              style={{
                width: '100%', padding: '0.85rem', marginTop: '0.5rem', fontSize: '0.95rem',
                fontWeight: 600, border: 'none', borderRadius: '10px', cursor: loading ? 'not-allowed' : 'pointer',
                color: 'white', letterSpacing: '1px',
                background: loading
                  ? '#475569'
                  : 'linear-gradient(135deg, #3b82f6 0%, #2563eb 50%, #1d4ed8 100%)',
                boxShadow: '0 4px 20px rgba(59,130,246,0.35)',
                transition: 'all 0.3s ease',
                opacity: loading ? 0.7 : 1
              }}
            >
              {loading ? '登录中...' : '登 录'}
            </button>
          </form>

          <div style={{ marginTop: '2rem', textAlign: 'center', fontSize: '0.8rem', color: '#475569', lineHeight: 1.6 }}>
            系统不对外开放注册<br/>如需开通账号请联系管理员
          </div>

          {/* Security badge */}
          <div style={{
            marginTop: '2.5rem', display: 'flex', alignItems: 'center', justifyContent: 'center',
            gap: '0.5rem', padding: '0.625rem 1rem', borderRadius: '8px',
            background: 'rgba(34,197,94,0.06)', border: '1px solid rgba(34,197,94,0.1)'
          }}>
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="#22c55e" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>
            </svg>
            <span style={{ color: '#4ade80', fontSize: '0.75rem', fontWeight: 500 }}>端到端加密 · 安全登录</span>
          </div>
        </div>
      </div>
      </div>
    </div>
  );
}

export default Login;
