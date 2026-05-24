import { useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';

import { updateUser } from '@api/auth';
import { auth, useCurrentUser, useRequireAuth } from '@stores/auth';
import { usePermissions } from '@/lib/auth/permissions';
import {
  getLocaleLabel,
  setLocale,
  useCurrentLocale,
  useSupportedLocales,
  useTranslator,
  type AppLocale,
} from '@/lib/i18n/store';

import { ApiKeysSection } from './ApiKeysSection';
import { GroupsSection } from './GroupsSection';
import { MfaSection } from './MfaSection';
import { PasswordSection } from './PasswordSection';
import { PermissionsSection } from './PermissionsSection';
import { PoliciesSection } from './PoliciesSection';
import { RestrictedViewsSection } from './RestrictedViewsSection';
import { RolesSection } from './RolesSection';
import { SsoProvidersSection } from './SsoProvidersSection';
import { UsersSection } from './UsersSection';

type SectionId =
  | 'profile'
  | 'password'
  | 'mfa'
  | 'tokens'
  | 'users'
  | 'groups'
  | 'roles'
  | 'permissions'
  | 'policies'
  | 'restricted-views'
  | 'sso';

interface SectionEntry {
  id: SectionId;
  label: string;
  visible: boolean;
}

interface SectionGroup {
  id: string;
  label: string;
  items: SectionEntry[];
}

export function SettingsPage() {
  const t = useTranslator();
  useRequireAuth();
  const perms = usePermissions();
  const currentUser = useCurrentUser();
  const currentLocale = useCurrentLocale();
  const supportedLocales = useSupportedLocales();
  const [searchParams, setSearchParams] = useSearchParams();

  const [selectedLocale, setSelectedLocale] = useState<AppLocale>(currentLocale);
  const [savingLanguage, setSavingLanguage] = useState(false);
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');

  useEffect(() => {
    setSelectedLocale(currentLocale);
  }, [currentLocale]);

  const groups = useMemo<SectionGroup[]>(
    () => [
      {
        id: 'user',
        label: 'User settings',
        items: [
          { id: 'profile', label: 'Profile', visible: true },
          { id: 'password', label: 'Password', visible: true },
          { id: 'mfa', label: 'Multi-factor auth', visible: true },
          { id: 'tokens', label: 'Tokens', visible: true },
        ],
      },
      {
        id: 'platform',
        label: 'Platform settings',
        items: [
          { id: 'users', label: 'Users', visible: perms.canReadUsers },
          { id: 'groups', label: 'Groups', visible: perms.canReadGroups },
          { id: 'roles', label: 'Roles', visible: perms.canReadRoles },
          { id: 'permissions', label: 'Permissions', visible: perms.canReadPermissions },
          { id: 'policies', label: 'Policies', visible: perms.canReadPolicies },
          { id: 'restricted-views', label: 'Restricted views', visible: perms.canReadPolicies },
          { id: 'sso', label: 'Third-party applications', visible: perms.canReadSso },
        ],
      },
    ],
    [
      perms.canReadGroups,
      perms.canReadPermissions,
      perms.canReadPolicies,
      perms.canReadRoles,
      perms.canReadSso,
      perms.canReadUsers,
    ],
  );

  const visibleIds = useMemo(
    () =>
      groups
        .flatMap((g) => g.items)
        .filter((item) => item.visible)
        .map((item) => item.id),
    [groups],
  );

  const requestedSection = (searchParams.get('section') ?? '') as SectionId | '';
  const active: SectionId = visibleIds.includes(requestedSection as SectionId)
    ? (requestedSection as SectionId)
    : 'profile';

  function selectSection(id: SectionId) {
    setError('');
    setNotice('');
    const next = new URLSearchParams(searchParams);
    if (id === 'profile') next.delete('section');
    else next.set('section', id);
    setSearchParams(next, { replace: true });
  }

  async function handleSaveLanguagePreference() {
    if (!currentUser) return;
    setSavingLanguage(true);
    setError('');
    setNotice('');
    try {
      const updated = await updateUser(currentUser.id, {
        attributes: { ...(currentUser.attributes ?? {}), locale: selectedLocale },
      });
      auth.updateCurrentUserProfile(updated);
      setLocale(selectedLocale);
      setNotice(t('settings.language.saved'));
    } catch (err) {
      setError(err instanceof Error ? err.message : t('settings.language.failed'));
    } finally {
      setSavingLanguage(false);
    }
  }

  return (
    <section className="of-page settings-page">
      <header className="settings-page__header">
        <p className="of-eyebrow">Settings</p>
        <h1 className="of-heading-xl">{t('settings.heroHeading')}</h1>
        <p className="of-text-muted" style={{ marginTop: 6, maxWidth: 720 }}>
          {t('settings.heroSubtitle')}
        </p>
      </header>

      <div className="settings-page__layout">
        <aside className="settings-nav" aria-label="Settings navigation">
          {groups.map((group) => {
            const items = group.items.filter((item) => item.visible);
            if (items.length === 0) return null;
            return (
              <div key={group.id} className="settings-nav__group">
                <div className="settings-nav__group-title">{group.label}</div>
                <ul className="settings-nav__list">
                  {items.map((item) => {
                    const isActive = active === item.id;
                    return (
                      <li key={item.id}>
                        <button
                          type="button"
                          className={`settings-nav__item${isActive ? ' settings-nav__item--active' : ''}`}
                          onClick={() => selectSection(item.id)}
                          aria-current={isActive ? 'page' : undefined}
                        >
                          {item.label}
                        </button>
                      </li>
                    );
                  })}
                </ul>
              </div>
            );
          })}
        </aside>

        <div className="settings-content">
          {(error || notice) && (
            <div className="settings-content__banners">
              {error && <div className="of-inline-note settings-banner settings-banner--error">{error}</div>}
              {notice && <div className="settings-banner settings-banner--success">{notice}</div>}
            </div>
          )}

          {active === 'profile' && (
            <section className="settings-section">
              <header className="settings-section__header">
                <div>
                  <h2 className="of-heading-lg">{t('settings.language.heading')}</h2>
                  <p className="of-text-muted" style={{ marginTop: 4, maxWidth: 640 }}>
                    {t('settings.language.description')}
                  </p>
                </div>
                <button
                  type="button"
                  className="of-btn of-btn-primary"
                  onClick={handleSaveLanguagePreference}
                  disabled={savingLanguage || !currentUser}
                >
                  {savingLanguage ? t('common.saving') : t('settings.language.save')}
                </button>
              </header>

              <div className="settings-card">
                <p className="of-eyebrow">{t('settings.language.badge')}</p>
                <div
                  style={{
                    display: 'grid',
                    gridTemplateColumns: 'minmax(0, 22rem) 1fr',
                    gap: 16,
                    marginTop: 12,
                    alignItems: 'start',
                  }}
                >
                  <label style={{ display: 'block', fontSize: 13 }}>
                    <span style={{ display: 'block', fontWeight: 500, marginBottom: 6 }}>
                      {t('settings.language.selectLabel')}
                    </span>
                    <select
                      className="of-select"
                      value={selectedLocale}
                      onChange={(e) => setSelectedLocale(e.target.value as AppLocale)}
                    >
                      {supportedLocales.map((locale) => (
                        <option key={locale} value={locale}>
                          {getLocaleLabel(locale, currentLocale)}
                        </option>
                      ))}
                    </select>
                  </label>
                  <div className="of-panel-muted" style={{ padding: '10px 14px' }}>
                    <span className="of-text-muted">{t('settings.language.help')}</span>
                  </div>
                </div>

                {currentUser && (
                  <div className="settings-card__meta">
                    <span className="of-eyebrow">{t('settings.signedInAs')}</span>
                    <span style={{ color: 'var(--text-strong)', fontWeight: 500 }}>
                      {currentUser.name}
                    </span>
                    <span className="of-text-muted">{currentUser.email}</span>
                  </div>
                )}
              </div>
            </section>
          )}

          {active === 'password' && (
            <PasswordSection setNotice={setNotice} setError={setError} />
          )}

          {active === 'mfa' && (
            <MfaSection setNotice={setNotice} setError={setError} />
          )}

          {active === 'tokens' && (
            <ApiKeysSection setNotice={setNotice} setError={setError} />
          )}

          {active === 'users' && (
            <UsersSection setNotice={setNotice} setError={setError} />
          )}

          {active === 'groups' && (
            <GroupsSection setNotice={setNotice} setError={setError} />
          )}

          {active === 'roles' && (
            <RolesSection setNotice={setNotice} setError={setError} />
          )}

          {active === 'permissions' && (
            <PermissionsSection setNotice={setNotice} setError={setError} />
          )}

          {active === 'policies' && (
            <PoliciesSection setNotice={setNotice} setError={setError} />
          )}

          {active === 'restricted-views' && (
            <RestrictedViewsSection setNotice={setNotice} setError={setError} />
          )}

          {active === 'sso' && (
            <SsoProvidersSection setNotice={setNotice} setError={setError} />
          )}
        </div>
      </div>
    </section>
  );
}
