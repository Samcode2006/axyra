import { useState } from 'react';
import { useNavigate, useLocation } from 'react-router-dom';
import { auth, errMsg } from '../../lib/api';
import { ThemeToggle } from '../../components/ThemeToggle';

export default function LoginPage() {
  const nav = useNavigate();
  const location = useLocation();
  const from = (location.state as { from?: { pathname: string } })?.from?.pathname ?? '/';
  const [form, setForm] = useState({ username: '', password: '' });
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setLoading(true);
    setError('');
    try {
      await auth.login(form.username, form.password);
      nav(from, { replace: true });
    } catch (err) {
      setError(errMsg(err, 'Sign-in failed'));
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="login-page">
      <ThemeToggle className="login-theme-toggle" />
      <div className="login-card">
        <div className="login-brand">
          <span className="brand-logo">Ax</span>
          <h1>Axyra Console</h1>
        </div>
        <form onSubmit={handleSubmit}>
          <label>Username
            <input
              required
              autoFocus
              autoComplete="username"
              value={form.username}
              onChange={(e) => setForm({ ...form, username: e.target.value })}
              placeholder="admin"
            />
          </label>
          <label>Password
            <input
              required
              type="password"
              autoComplete="current-password"
              value={form.password}
              onChange={(e) => setForm({ ...form, password: e.target.value })}
              placeholder="••••••••"
            />
          </label>
          {error && <p className="error-msg">{error}</p>}
          <button type="submit" className="btn btn--primary btn--full" disabled={loading}>
            {loading ? 'Signing in…' : 'Sign in'}
          </button>
        </form>
      </div>
    </div>
  );
}

