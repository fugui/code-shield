import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { AUTH_TOKEN_KEY, appNavigatePath } from '../config';

/**
 * OAuthCallback — handles the redirect from the backend OAuth2 callback.
 * Extracts the JWT token from URL query parameters, stores it in localStorage,
 * and redirects to the main application.
 */
function OAuthCallback() {
  const navigate = useNavigate();
  const [error, setError] = useState('');

  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const token = params.get('token');
    const ssoError = params.get('sso_error');

    if (ssoError) {
      setError(ssoError);
      setTimeout(() => navigate(appNavigatePath('/login')), 3000);
      return;
    }

    if (token) {
      // TODO(security): Token is received via URL query parameter from the server-side OAuth2 callback.
      // This is acceptable because: (1) the callback URL is a short-lived redirect, (2) the token is
      // consumed immediately and the URL is replaced, (3) HTTPS is enforced in production.
      // For higher security environments, consider using a one-time exchange code pattern instead.
      localStorage.setItem(AUTH_TOKEN_KEY, token);
      window.dispatchEvent(new Event('auth-change'));

      // Cleanly navigate to the tasks page and replace history entry (removing token from history)
      navigate(appNavigatePath('/tasks'), { replace: true });
    } else {
      setError('SSO 登录失败：未收到有效的认证凭证');
      setTimeout(() => navigate(appNavigatePath('/login')), 3000);
    }
  }, [navigate]);

  if (error) {
    return (
      <div style={{
        display: 'flex', justifyContent: 'center', alignItems: 'center',
        minHeight: '100vh', background: '#0b1120'
      }}>
        <div style={{
          padding: '2rem', borderRadius: '12px', maxWidth: '420px', textAlign: 'center',
          background: 'rgba(239,68,68,0.08)', border: '1px solid rgba(239,68,68,0.2)'
        }}>
          <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="#ef4444" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={{ marginBottom: '1rem' }}>
            <circle cx="12" cy="12" r="10"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/>
          </svg>
          <h3 style={{ color: '#fca5a5', margin: '0 0 0.5rem', fontSize: '1.1rem' }}>SSO 登录失败</h3>
          <p style={{ color: '#94a3b8', fontSize: '0.9rem', margin: '0 0 1rem' }}>{error}</p>
          <p style={{ color: '#475569', fontSize: '0.8rem', margin: 0 }}>正在跳转到登录页面...</p>
        </div>
      </div>
    );
  }

  // Loading state while processing
  return (
    <div style={{
      display: 'flex', justifyContent: 'center', alignItems: 'center',
      minHeight: '100vh', background: '#0b1120'
    }}>
      <div style={{ textAlign: 'center' }}>
        <div style={{
          width: '48px', height: '48px', borderRadius: '50%', margin: '0 auto 1rem',
          border: '3px solid rgba(59,130,246,0.2)', borderTop: '3px solid #3b82f6',
          animation: 'spin 0.8s linear infinite'
        }} />
        <style>{`@keyframes spin { to { transform: rotate(360deg); } }`}</style>
        <p style={{ color: '#94a3b8', fontSize: '0.9rem' }}>正在完成 SSO 登录...</p>
      </div>
    </div>
  );
}

export default OAuthCallback;
