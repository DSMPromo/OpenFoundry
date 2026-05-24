import { useState } from 'react';
import { Link } from 'react-router-dom';

import { requestPasswordReset } from '@api/auth';

// ForgotPasswordPage — public page reached from the Sign-In view.
// Submits the user's email to /api/v1/auth/password/reset/request and
// renders a constant "if that email matches, you'll get a link"
// message regardless of whether the address exists. In dev (no SMTP),
// the backend echoes the plaintext token in the response so we render
// a clickable link directly — the developer doesn't have to dig
// through container logs.
export function ForgotPasswordPage() {
  const [email, setEmail] = useState('');
  const [busy, setBusy] = useState(false);
  const [submitted, setSubmitted] = useState(false);
  const [devLink, setDevLink] = useState('');
  const [error, setError] = useState('');

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError('');
    setBusy(true);
    try {
      const resp = await requestPasswordReset(email);
      setSubmitted(true);
      if (resp.dev_reset_url) {
        // Prefer the relative path so the link works through the
        // Vite proxy on whatever host the page is open at.
        const path = resp.dev_reset_url.startsWith('http')
          ? new URL(resp.dev_reset_url).pathname + new URL(resp.dev_reset_url).search
          : resp.dev_reset_url;
        setDevLink(path);
      } else if (resp.dev_token) {
        setDevLink(`/auth/reset-password?token=${encodeURIComponent(resp.dev_token)}`);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Reset request failed.');
    } finally {
      setBusy(false);
    }
  }

  return (
    <section style={{ maxWidth: 420, margin: '40px auto', padding: 24 }}>
      <header style={{ marginBottom: 24 }}>
        <h1 style={{ margin: 0, fontSize: 22 }}>Forgot your password?</h1>
        <p style={{ marginTop: 8, color: '#9ca3af', fontSize: 14 }}>
          Enter the email you signed up with. If it matches an active account, we'll send a one-time
          reset link that's valid for one hour.
        </p>
      </header>

      {!submitted && (
        <form onSubmit={handleSubmit} style={{ display: 'grid', gap: 12 }}>
          <label style={{ display: 'grid', gap: 4, fontSize: 13 }}>
            <span>Email</span>
            <input
              type="email"
              autoComplete="email"
              required
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              className="of-input"
              placeholder="you@example.com"
            />
          </label>
          {error && (
            <p style={{ color: '#f87171', fontSize: 13, margin: 0 }}>{error}</p>
          )}
          <button
            type="submit"
            disabled={busy || !email}
            className="of-btn of-btn-primary"
          >
            {busy ? 'Sending…' : 'Send reset link'}
          </button>
        </form>
      )}

      {submitted && (
        <div style={{ display: 'grid', gap: 12 }}>
          <p style={{ fontSize: 14, lineHeight: 1.5 }}>
            If <strong>{email}</strong> matches an active account, a reset link is on its way.
            Check your inbox (and spam folder). The link works only once and expires in one hour.
          </p>
          {devLink && (
            <div
              style={{
                padding: 12,
                background: '#1f2937',
                border: '1px solid #374151',
                borderRadius: 6,
                fontSize: 13,
              }}
            >
              <p style={{ margin: '0 0 8px 0', color: '#a3a3a3' }}>
                Dev mode (no SMTP relay configured): the reset link is rendered inline so you can
                click through without leaving the app. Production deployments never expose tokens
                in the API response.
              </p>
              <Link to={devLink} style={{ color: '#60a5fa' }}>Open reset link →</Link>
            </div>
          )}
        </div>
      )}

      <p style={{ marginTop: 24, fontSize: 13 }}>
        Remembered it? <Link to="/auth/login" style={{ color: '#60a5fa' }}>Back to sign in</Link>
      </p>
    </section>
  );
}
