import { useEffect, useMemo, useState } from 'react';
import { Link, useLocation, useNavigate, useSearchParams } from 'react-router-dom';

import { getBootstrapStatus, listPublicSsoProviders, type PublicSsoProvider } from '@api/auth';
import {
  clearStoredAuthReturnTo,
  getAuthReturnTo,
  getStoredAuthReturnTo,
  rememberAuthReturnTo,
  resolveAuthReturnTo,
  withAuthReturnTo,
} from '@/lib/auth/redirects';
import { auth } from '@stores/auth';
import { useTranslator } from '@/lib/i18n/store';

type LoginUiStatus = 'idle' | 'loading' | 'success' | 'mfa_required' | 'error';
type Step = 'email' | 'password';

const REMEMBER_KEY = 'of_login_remember_email';

const inputStyle: React.CSSProperties = {
  width: '100%',
  padding: '10px 12px 10px 36px',
  fontSize: 14,
  borderRadius: 4,
  border: '1px solid rgba(255, 255, 255, 0.12)',
  background: '#2c3540',
  color: '#f3f4f6',
  outline: 'none',
};

const plainInputStyle: React.CSSProperties = {
  ...inputStyle,
  paddingLeft: 12,
};

const primaryButtonStyle: React.CSSProperties = {
  width: '100%',
  padding: '10px 14px',
  fontSize: 14,
  fontWeight: 600,
  color: '#fff',
  background: '#2563eb',
  border: 'none',
  borderRadius: 4,
  cursor: 'pointer',
};

const secondaryButtonStyle: React.CSSProperties = {
  width: '100%',
  padding: '10px 14px',
  fontSize: 13,
  color: 'rgba(243, 244, 246, 0.85)',
  background: 'transparent',
  border: '1px solid rgba(255, 255, 255, 0.18)',
  borderRadius: 4,
  cursor: 'pointer',
};

function PersonIcon() {
  return (
    <svg
      aria-hidden
      width={16}
      height={16}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.8}
      strokeLinecap="round"
      strokeLinejoin="round"
      style={{ position: 'absolute', left: 12, top: '50%', transform: 'translateY(-50%)', color: 'rgba(243, 244, 246, 0.55)' }}
    >
      <circle cx="12" cy="8" r="4" />
      <path d="M4 21c0-4 4-7 8-7s8 3 8 7" />
    </svg>
  );
}

export function LoginPage() {
  const t = useTranslator();
  const navigate = useNavigate();
  const location = useLocation();
  const [searchParams] = useSearchParams();
  const registeredEmail = searchParams.get('email')?.trim() ?? '';
  const justRegistered = searchParams.get('registered') === 'true';
  const justResetPassword = searchParams.get('password_reset') === '1';
  const explicitReturnTo = getAuthReturnTo(location.search);
  const intendedReturnTo = explicitReturnTo ?? getStoredAuthReturnTo();
  const postAuthRedirect = resolveAuthReturnTo(location.search);

  const [step, setStep] = useState<Step>('email');
  const [email, setEmail] = useState(() => {
    if (registeredEmail) return registeredEmail;
    if (typeof localStorage === 'undefined') return '';
    return localStorage.getItem(REMEMBER_KEY) ?? '';
  });
  const [remember, setRemember] = useState(() => {
    if (typeof localStorage === 'undefined') return false;
    return Boolean(localStorage.getItem(REMEMBER_KEY));
  });
  const [password, setPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [error, setError] = useState('');
  const [status, setStatus] = useState<LoginUiStatus>('idle');
  const [ssoLoadingSlug, setSsoLoadingSlug] = useState<string | null>(null);
  const [providers, setProviders] = useState<PublicSsoProvider[]>([]);
  const [requiresInitialAdmin, setRequiresInitialAdmin] = useState(false);

  const isBusy = status === 'loading' || status === 'success' || status === 'mfa_required';
  const ssoBusy = Boolean(ssoLoadingSlug);

  useEffect(() => {
    document.title = t('auth.login.title');
  }, [t]);

  useEffect(() => {
    if (registeredEmail) {
      setEmail(registeredEmail);
      setStep('password');
    }
  }, [registeredEmail]);

  useEffect(() => {
    rememberAuthReturnTo(explicitReturnTo);
  }, [explicitReturnTo]);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const s = await getBootstrapStatus();
        if (!cancelled) setRequiresInitialAdmin(s.requires_initial_admin);
      } catch {
        if (!cancelled) setRequiresInitialAdmin(false);
      }
      try {
        const list = await listPublicSsoProviders();
        if (!cancelled) setProviders(list);
      } catch {
        if (!cancelled) setProviders([]);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  function handleEmailSubmit(event: React.FormEvent) {
    event.preventDefault();
    setError('');
    const trimmed = email.trim();
    if (!trimmed) {
      setError(t('auth.login.validationEmail'));
      return;
    }
    if (!trimmed.includes('@')) {
      setError(t('auth.login.validationEmailFormat'));
      return;
    }
    if (typeof localStorage !== 'undefined') {
      if (remember) localStorage.setItem(REMEMBER_KEY, trimmed);
      else localStorage.removeItem(REMEMBER_KEY);
    }
    setEmail(trimmed);
    setStep('password');
  }

  async function handlePasswordSubmit(event: React.FormEvent) {
    event.preventDefault();
    setError('');
    if (!password) {
      setError(t('auth.login.validationPassword'));
      return;
    }

    setStatus('loading');
    try {
      const result = await auth.login(email.trim(), password);
      if (result.status === 'mfa_required') {
        setStatus('mfa_required');
        rememberAuthReturnTo(postAuthRedirect);
        window.setTimeout(() => {
          navigate(withAuthReturnTo('/auth/mfa', postAuthRedirect), { replace: true });
        }, 350);
        return;
      }
      setStatus('success');
      clearStoredAuthReturnTo();
      navigate(postAuthRedirect, { replace: true });
    } catch (err) {
      setStatus('error');
      setError(err instanceof Error ? err.message : t('auth.login.failed'));
    }
  }

  async function handleSsoLogin(slug: string) {
    setError('');
    setStatus('loading');
    setSsoLoadingSlug(slug);
    rememberAuthReturnTo(postAuthRedirect);
    try {
      await auth.startSsoLogin(slug, postAuthRedirect);
    } catch (err) {
      setStatus('error');
      setError(err instanceof Error ? err.message : t('auth.login.ssoFailed'));
      setSsoLoadingSlug(null);
    }
  }

  const submitLabel = useMemo(() => {
    switch (status) {
      case 'loading':
        return t('auth.login.signingIn');
      case 'success':
        return t('auth.login.successCta');
      case 'mfa_required':
        return t('auth.login.mfaRedirecting');
      default:
        return t('auth.login.signIn');
    }
  }, [status, t]);

  return (
    <div
      style={{
        position: 'fixed',
        inset: 0,
        background: '#1f2933',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        padding: '40px 24px',
        zIndex: 50,
      }}
    >
      <div
        style={{
          width: '100%',
          maxWidth: 360,
          display: 'grid',
          justifyItems: 'center',
          gap: 28,
        }}
      >
        <img
          src="/empty-logo.png"
          alt="OpenFoundry"
          style={{ width: 96, height: 96, objectFit: 'contain', filter: 'drop-shadow(0 1px 2px rgba(0,0,0,0.4))' }}
        />

        {requiresInitialAdmin && (
          <div
            role="status"
            style={{
              width: '100%',
              padding: '8px 12px',
              borderRadius: 6,
              background: 'rgba(59, 130, 246, 0.16)',
              border: '1px solid rgba(59, 130, 246, 0.4)',
              color: '#bfdbfe',
              fontSize: 12.5,
              textAlign: 'center',
            }}
          >
            {t('auth.login.bootstrapNotice')}
          </div>
        )}

        {justRegistered && status !== 'success' && status !== 'mfa_required' && (
          <div
            role="status"
            style={{
              width: '100%',
              padding: '8px 12px',
              borderRadius: 6,
              background: 'rgba(34, 197, 94, 0.16)',
              border: '1px solid rgba(34, 197, 94, 0.4)',
              color: '#bbf7d0',
              fontSize: 12.5,
              textAlign: 'center',
            }}
          >
            {registeredEmail
              ? t('auth.login.registeredFor', { email: registeredEmail })
              : t('auth.login.registered')}
          </div>
        )}

        {justResetPassword && status !== 'success' && status !== 'mfa_required' && (
          <div
            role="status"
            style={{
              width: '100%',
              padding: '8px 12px',
              borderRadius: 6,
              background: 'rgba(34, 197, 94, 0.16)',
              border: '1px solid rgba(34, 197, 94, 0.4)',
              color: '#bbf7d0',
              fontSize: 12.5,
              textAlign: 'center',
            }}
          >
            Password updated. Sign in with your new password.
          </div>
        )}

        {step === 'email' ? (
          <form onSubmit={handleEmailSubmit} style={{ width: '100%', display: 'grid', gap: 14 }} noValidate>
            {error && (
              <div
                role="alert"
                style={{
                  padding: '8px 12px',
                  borderRadius: 6,
                  background: 'rgba(239, 68, 68, 0.16)',
                  border: '1px solid rgba(239, 68, 68, 0.4)',
                  color: '#fecaca',
                  fontSize: 12.5,
                }}
              >
                {error}
              </div>
            )}

            <div style={{ position: 'relative' }}>
              <PersonIcon />
              <input
                type="email"
                value={email}
                onChange={(e) => {
                  setEmail(e.target.value);
                  if (error) setError('');
                }}
                placeholder={t('auth.login.emailPlaceholder')}
                autoComplete="email"
                autoFocus
                required
                style={inputStyle}
              />
            </div>

            <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13, color: 'rgba(243, 244, 246, 0.78)' }}>
              <input
                type="checkbox"
                checked={remember}
                onChange={(e) => setRemember(e.target.checked)}
                style={{ accentColor: '#3b82f6' }}
              />
              {t('auth.login.rememberMe')}
            </label>

            <button type="submit" style={primaryButtonStyle}>
              {t('auth.login.next')}
            </button>

            {providers.length > 0 && (
              <div style={{ display: 'grid', gap: 8, paddingTop: 6 }}>
                <div
                  role="separator"
                  aria-orientation="horizontal"
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 10,
                    color: 'rgba(243, 244, 246, 0.5)',
                    fontSize: 10,
                    fontWeight: 700,
                    textTransform: 'uppercase',
                    letterSpacing: '0.08em',
                  }}
                >
                  <span style={{ flex: 1, height: 1, background: 'rgba(255, 255, 255, 0.12)' }} />
                  {t('auth.login.sso')}
                  <span style={{ flex: 1, height: 1, background: 'rgba(255, 255, 255, 0.12)' }} />
                </div>
                {providers.map((provider) => (
                  <button
                    key={provider.id}
                    type="button"
                    style={secondaryButtonStyle}
                    disabled={ssoBusy}
                    onClick={() => handleSsoLogin(provider.slug)}
                  >
                    {ssoLoadingSlug === provider.slug
                      ? t('auth.login.ssoRedirecting')
                      : t('auth.login.continueWith', { provider: provider.name })}
                  </button>
                ))}
              </div>
            )}
          </form>
        ) : (
          <form onSubmit={handlePasswordSubmit} style={{ width: '100%', display: 'grid', gap: 14 }} noValidate>
            <div
              style={{
                textAlign: 'center',
                fontSize: 12,
                color: 'rgba(243, 244, 246, 0.6)',
                fontFamily: 'var(--font-mono)',
                wordBreak: 'break-all',
              }}
            >
              {email}
            </div>

            {status === 'success' && (
              <div
                role="status"
                style={{
                  padding: '8px 12px',
                  borderRadius: 6,
                  background: 'rgba(34, 197, 94, 0.16)',
                  border: '1px solid rgba(34, 197, 94, 0.4)',
                  color: '#bbf7d0',
                  fontSize: 12.5,
                }}
              >
                {t('auth.login.successMessage')}
              </div>
            )}

            {status === 'mfa_required' && (
              <div
                role="status"
                style={{
                  padding: '8px 12px',
                  borderRadius: 6,
                  background: 'rgba(59, 130, 246, 0.16)',
                  border: '1px solid rgba(59, 130, 246, 0.4)',
                  color: '#bfdbfe',
                  fontSize: 12.5,
                }}
              >
                {t('auth.login.mfaRequired')}
              </div>
            )}

            {status === 'error' && error && (
              <div
                role="alert"
                style={{
                  padding: '8px 12px',
                  borderRadius: 6,
                  background: 'rgba(239, 68, 68, 0.16)',
                  border: '1px solid rgba(239, 68, 68, 0.4)',
                  color: '#fecaca',
                  fontSize: 12.5,
                }}
              >
                {error}
              </div>
            )}

            <div>
              <div
                style={{
                  display: 'flex',
                  alignItems: 'baseline',
                  justifyContent: 'flex-end',
                  marginBottom: 4,
                }}
              >
                <button
                  type="button"
                  onClick={() => setShowPassword((v) => !v)}
                  aria-pressed={showPassword}
                  style={{
                    background: 'none',
                    border: 'none',
                    padding: 0,
                    fontSize: 11,
                    cursor: 'pointer',
                    color: '#93c5fd',
                  }}
                >
                  {showPassword ? t('auth.login.hidePassword') : t('auth.login.showPassword')}
                </button>
              </div>
              <input
                type={showPassword ? 'text' : 'password'}
                value={password}
                onChange={(e) => {
                  setPassword(e.target.value);
                  if (status === 'error') setStatus('idle');
                  if (error) setError('');
                }}
                placeholder={t('auth.login.passwordPlaceholder')}
                autoComplete="current-password"
                autoFocus
                required
                disabled={isBusy}
                style={plainInputStyle}
              />
            </div>

            <div style={{ display: 'flex', gap: 8 }}>
              <button
                type="button"
                onClick={() => {
                  setError('');
                  setStatus('idle');
                  setPassword('');
                  setStep('email');
                }}
                disabled={isBusy}
                style={{
                  ...secondaryButtonStyle,
                  width: 'auto',
                  flex: '0 0 auto',
                  cursor: isBusy ? 'not-allowed' : 'pointer',
                }}
              >
                {t('auth.login.back')}
              </button>
              <button
                type="submit"
                disabled={isBusy || !password}
                style={{
                  ...primaryButtonStyle,
                  flex: 1,
                  cursor: isBusy ? 'not-allowed' : 'pointer',
                  opacity: isBusy ? 0.7 : 1,
                }}
              >
                {submitLabel}
              </button>
            </div>
          </form>
        )}

        <p
          style={{
            textAlign: 'center',
            fontSize: 12.5,
            color: 'rgba(243, 244, 246, 0.6)',
            margin: 0,
          }}
        >
          {requiresInitialAdmin ? t('auth.login.bootstrapCta') : t('auth.login.noAccount')}{' '}
          <Link
            to={withAuthReturnTo('/auth/register', intendedReturnTo)}
            style={{ color: '#93c5fd', textDecoration: 'none' }}
          >
            {t('auth.login.register')}
          </Link>
          {' · '}
          <Link to="/auth/forgot-password" style={{ color: '#93c5fd', textDecoration: 'none' }}>
            Forgot password?
          </Link>
        </p>
      </div>
    </div>
  );
}
