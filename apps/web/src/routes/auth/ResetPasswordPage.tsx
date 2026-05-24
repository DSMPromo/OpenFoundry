import { useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';

import { confirmPasswordReset } from '@api/auth';

// ResetPasswordPage — public page reached via the link in the reset
// email (or, in dev, via the dev-link rendered on ForgotPasswordPage).
// Reads the token from the ?token= query string, prompts for a new
// password, calls /api/v1/auth/password/reset/confirm. On success
// redirects to /auth/login with a flash hint that the password was
// updated.
export function ResetPasswordPage() {
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const token = params.get('token') ?? '';

  const [newPassword, setNewPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError('');

    if (newPassword.length < 8) {
      setError('Password must be at least 8 characters.');
      return;
    }
    if (newPassword !== confirm) {
      setError('Password and confirmation do not match.');
      return;
    }
    setBusy(true);
    try {
      await confirmPasswordReset({ token, new_password: newPassword });
      navigate('/auth/login?password_reset=1', { replace: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Reset failed. Request a new link.');
    } finally {
      setBusy(false);
    }
  }

  if (!token) {
    return (
      <section style={{ maxWidth: 420, margin: '40px auto', padding: 24 }}>
        <h1 style={{ margin: 0, fontSize: 22 }}>Missing reset token</h1>
        <p style={{ marginTop: 12, color: '#9ca3af', fontSize: 14 }}>
          This page needs a one-time reset token in the URL (the link from the email).{' '}
          <Link to="/auth/forgot-password" style={{ color: '#60a5fa' }}>
            Request a new reset link →
          </Link>
        </p>
      </section>
    );
  }

  return (
    <section style={{ maxWidth: 420, margin: '40px auto', padding: 24 }}>
      <header style={{ marginBottom: 24 }}>
        <h1 style={{ margin: 0, fontSize: 22 }}>Set a new password</h1>
        <p style={{ marginTop: 8, color: '#9ca3af', fontSize: 14 }}>
          Enter a new password (≥ 8 chars). The reset link works only once and expires after one
          hour — if this fails, request a new one.
        </p>
      </header>

      <form onSubmit={handleSubmit} style={{ display: 'grid', gap: 12 }}>
        <label style={{ display: 'grid', gap: 4, fontSize: 13 }}>
          <span>New password</span>
          <input
            type="password"
            autoComplete="new-password"
            required
            minLength={8}
            value={newPassword}
            onChange={(e) => setNewPassword(e.target.value)}
            className="of-input"
            placeholder="••••••••"
          />
        </label>
        <label style={{ display: 'grid', gap: 4, fontSize: 13 }}>
          <span>Confirm new password</span>
          <input
            type="password"
            autoComplete="new-password"
            required
            minLength={8}
            value={confirm}
            onChange={(e) => setConfirm(e.target.value)}
            className="of-input"
            placeholder="••••••••"
          />
        </label>
        {error && <p style={{ color: '#f87171', fontSize: 13, margin: 0 }}>{error}</p>}
        <button
          type="submit"
          disabled={busy || !newPassword || !confirm}
          className="of-btn of-btn-primary"
        >
          {busy ? 'Updating…' : 'Set new password'}
        </button>
      </form>

      <p style={{ marginTop: 24, fontSize: 13 }}>
        <Link to="/auth/login" style={{ color: '#60a5fa' }}>Back to sign in</Link>
      </p>
    </section>
  );
}
