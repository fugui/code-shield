import React, { useState } from 'react';
import { useNavigate } from 'react-router-dom';

function Login() {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const navigate = useNavigate();

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    
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
        alert(data.error || '登录失败，可能是密码错误或账号被禁用');
      }
    })
    .catch(console.error);
  };

  return (
    <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: '80vh' }}>
      <div className="card" style={{ width: '400px', maxWidth: '90%', padding: '2rem', textAlign: 'center', position: 'relative', overflow: 'hidden' }}>
        {/* Decorative background element for tech feel */}
        <div style={{ position: 'absolute', top: '-50%', left: '-50%', width: '200%', height: '200%', background: 'radial-gradient(circle, rgba(37,99,235,0.1) 0%, rgba(0,0,0,0) 70%)', zIndex: 0, pointerEvents: 'none' }}></div>
        
        <div style={{ position: 'relative', zIndex: 1 }}>
          <div style={{ width: '60px', height: '60px', background: 'var(--primary-color)', borderRadius: '12px', margin: '0 auto 1.5rem', display: 'flex', alignItems: 'center', justifyContent: 'center', boxShadow: '0 0 20px rgba(37,99,235,0.4)' }}>
            <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="white" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M12 2L2 7l10 5 10-5-10-5zM2 17l10 5 10-5M2 12l10 5 10-5"/>
            </svg>
          </div>
          
          <h2 style={{ marginBottom: '0.5rem', letterSpacing: '-0.5px' }}>欢迎登录</h2>
          <p style={{ color: '#94a3b8', marginBottom: '2rem', fontSize: '0.9rem' }}>请输入系统分配给您的凭据以进入系统</p>

          <form onSubmit={handleSubmit} style={{ display: 'flex', flexDirection: 'column', gap: '1.25rem' }}>
            <div style={{ textAlign: 'left' }}>
              <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem', color: 'var(--text-color)', fontWeight: 500 }}>用户名</label>
              <input 
                required 
                type="text" 
                value={username} 
                onChange={e => setUsername(e.target.value)} 
                style={{ width: '100%', padding: '0.75rem', borderRadius: '6px', border: '1px solid var(--border-color)', background: 'var(--card-bg)', color: 'var(--text-color)', boxSizing: 'border-box', transition: 'border-color 0.2s, box-shadow 0.2s', outline: 'none' }} 
                onFocus={e => e.target.style.borderColor = 'var(--primary-color)'}
                onBlur={e => e.target.style.borderColor = 'var(--border-color)'}
              />
            </div>
            
            <div style={{ textAlign: 'left' }}>
              <label style={{ display: 'block', marginBottom: '0.5rem', fontSize: '0.875rem', color: 'var(--text-color)', fontWeight: 500 }}>密码</label>
              <input 
                required 
                type="password" 
                value={password} 
                onChange={e => setPassword(e.target.value)} 
                style={{ width: '100%', padding: '0.75rem', borderRadius: '6px', border: '1px solid var(--border-color)', background: 'var(--card-bg)', color: 'var(--text-color)', boxSizing: 'border-box', transition: 'border-color 0.2s, box-shadow 0.2s', outline: 'none' }} 
                onFocus={e => e.target.style.borderColor = 'var(--primary-color)'}
                onBlur={e => e.target.style.borderColor = 'var(--border-color)'}
              />
            </div>

            <button type="submit" className="btn" style={{ width: '100%', padding: '0.75rem', marginTop: '0.5rem', fontSize: '1rem', boxShadow: '0 4px 14px 0 rgba(37,99,235,0.39)' }}>
              登 录
            </button>
          </form>

          <div style={{ marginTop: '1.5rem', fontSize: '0.875rem', color: '#94a3b8' }}>
            系统不对外开放注册，如需开通账号请联系管理员。
          </div>
        </div>
      </div>
    </div>
  );
}

export default Login;
