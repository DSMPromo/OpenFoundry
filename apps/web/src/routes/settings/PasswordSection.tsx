import { useState } from 'react';

import { changePassword } from '@api/auth';

interface PasswordSectionProps {
  setNotice: (msg: string) => void;
  setError: (msg: string) => void;
}

// PasswordSection — Settings → Profile pane card for self-service
// password change. Calls identity-federation-service via the gateway
// at POST /api/v1/auth/password. The handler verifies the current
// password before rewriting the argon2id hash; the session cookie
// stays valid so the user is not bumped back to /login.
export function PasswordSection({ setNotice, setError }: PasswordSectionProps) {
  const [currentPassword, setCurrentPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [busy, setBusy] = useState(false);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setNotice('');
    setError('');

    if (newPassword.length < 8) {
      setError('New password must be at least 8 characters.');
      return;
    }
    if (newPassword !== confirmPassword) {
      setError('New password and confirmation do not match.');
      return;
    }
    if (newPassword === currentPassword) {
      setError('New password must differ from the current one.');
      return;
    }

    setBusy(true);
    try {
      await changePassword({ current_password: currentPassword, new_password: newPassword });
      setNotice('Password updated. Use the new value the next time you sign in.');
      setCurrentPassword('');
      setNewPassword('');
      setConfirmPassword('');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Password change failed.');
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="settings-section">
      <header className="settings-section__header">
        <div>
          <h2 className="of-heading-lg">Change password</h2>
          <p className="of-text-muted" style={{ marginTop: 4, maxWidth: 640 }}>
            Replace the password used to sign in. We verify your current password before applying
            the change. The active session continues; you only need the new value next time you log in.
          </p>
        </div>
      </header>

      <form
        onSubmit={handleSubmit}
        style={{ display: 'grid', gap: 12, maxWidth: 420, marginTop: 16 }}
      >
        <label style={{ display: 'grid', gap: 4, fontSize: 13 }}>
          <span>Current password</span>
          <input
            type="password"
            autoComplete="current-password"
            value={currentPassword}
            onChange={(e) => setCurrentPassword(e.target.value)}
            required
            className="of-input"
            placeholder="••••••••"
          />
        </label>
        <label style={{ display: 'grid', gap: 4, fontSize: 13 }}>
          <span>New password (≥ 8 chars)</span>
          <input
            type="password"
            autoComplete="new-password"
            value={newPassword}
            onChange={(e) => setNewPassword(e.target.value)}
            required
            minLength={8}
            className="of-input"
            placeholder="••••••••"
          />
        </label>
        <label style={{ display: 'grid', gap: 4, fontSize: 13 }}>
          <span>Confirm new password</span>
          <input
            type="password"
            autoComplete="new-password"
            value={confirmPassword}
            onChange={(e) => setConfirmPassword(e.target.value)}
            required
            minLength={8}
            className="of-input"
            placeholder="••••••••"
          />
        </label>
        <div>
          <button
            type="submit"
            className="of-btn of-btn-primary"
            disabled={busy || !currentPassword || !newPassword || !confirmPassword}
          >
            {busy ? 'Updating…' : 'Update password'}
          </button>
        </div>
      </form>
    </section>
  );
}
