import { type Page } from '@playwright/test';
import { test, expect } from './fixtures/base';
import { captureRequests } from './fixtures/api-mocks';
import { mockAuth } from './fixtures/mocks';

/**
 * E2E coverage for `/auth/callback`
 * (apps/web/src/routes/auth/CallbackPage.tsx).
 *
 * The shipped CallbackPage:
 *   - POSTs to `/auth/sso/callback` (NOT `/auth/oidc/exchange` from the
 *     roadmap) via `auth.handleSsoCallback({ code, state, saml_response,
 *     relay_state })`.
 *   - Reads `?code&state` (OIDC), `?SAMLResponse&RelayState` (SAML), or
 *     `?error&error_description` (IdP-side failure) from the URL.
 *   - On `result.status === 'authenticated'` → navigates to
 *     `state.return_to` or `/`.
 *   - On `result.status === 'mfa_required'` → navigates to `/auth/mfa`.
 *   - On any other failure → renders the error in a `role="alert"`
 *     region and a `Back to login` link (the roadmap's "Try again"
 *     button is not implemented — the actual control returns the user
 *     to `/auth/login`).
 *   - There is no IdP catalog check in this component; a disabled
 *     provider surfaces as a server-side 4xx on the exchange call. The
 *     "provider deshabilitado" case from the roadmap is therefore
 *     folded into the generic 400 test.
 *
 * The auth client uses httpOnly cookies for the real session; the
 * `of_access_token` / `of_refresh_token` keys in `localStorage` are
 * legacy compatibility shims seeded by `mockAuth` so client-side code
 * paths see "a token exists". We assert those remain set after a
 * successful callback (the post-success assertion in the roadmap), but
 * the real authn signal in production is the `of_session` cookie.
 */

const VALID_CODE = 'e2e-authz-code';
const VALID_STATE = 'e2e-state-token';
const SSO_ENDPOINT = /\/api\/v1\/auth\/sso\/callback$/;

const errorAlert = (page: Page) => page.getByRole('alert');
const backToLoginLink = (page: Page) =>
  page.getByRole('link', { name: /back to (sign[- ]?in|login)|sign[- ]?in|^back/i });

async function mockUsersMeAuthenticated(page: Page): Promise<void> {
  await page.route('**/api/v1/users/me', async (route) => {
    await route.fulfill({
      json: {
        id: '00000000-0000-0000-0000-000000000001',
        email: 'sso-runner@example.com',
        name: 'SSO Runner',
        is_active: true,
        roles: ['admin'],
        groups: [],
        permissions: ['*'],
        organization_id: null,
        attributes: {},
        mfa_enabled: false,
        mfa_enforced: false,
        auth_source: 'sso',
        created_at: '2026-05-11T00:00:00Z',
      },
    });
  });
}

async function mockSsoExchange(
  page: Page,
  response:
    | { kind: 'authenticated' }
    | { kind: 'mfa_required' }
    | { kind: 'error'; status: number; message: string },
): Promise<void> {
  await page.route('**/api/v1/auth/sso/callback', async (route) => {
    if (route.request().method() !== 'POST') {
      await route.fallback();
      return;
    }
    if (response.kind === 'error') {
      await route.fulfill({
        status: response.status,
        json: { error: response.message },
      });
      return;
    }
    if (response.kind === 'mfa_required') {
      await route.fulfill({
        status: 200,
        json: {
          status: 'mfa_required',
          challenge_token: 'e2e-challenge-token',
          expires_in: 300,
          methods: ['totp'],
        },
      });
      return;
    }
    await route.fulfill({
      status: 200,
      json: {
        status: 'authenticated',
        access_token: 'e2e-access',
        refresh_token: 'e2e-refresh',
        token_type: 'Bearer',
        expires_in: 3600,
      },
    });
  });
}

test.beforeEach(async ({ page }) => {
  // Every test starts as "tokens already present in localStorage" — the
  // SSO flow only kicks in when the SPA already has a session cookie or
  // is about to acquire one. mockAuth({ authenticated: true }) seeds
  // the of_access_token / of_refresh_token compatibility shims.
  await mockAuth(page, { authenticated: true });
});

test('valid code+state: exchanges tokens and redirects to /', async ({ page }) => {
  await mockUsersMeAuthenticated(page);
  await mockSsoExchange(page, { kind: 'authenticated' });
  const cap = captureRequests(page, SSO_ENDPOINT);

  await page.goto(
    `/auth/callback?code=${encodeURIComponent(VALID_CODE)}&state=${encodeURIComponent(VALID_STATE)}`,
  );

  // Post-success redirect — no return_to provided so we land on /.
  await expect(page).toHaveURL(/127\.0\.0\.1:55173\/?$/);

  // The exchange was invoked with the URL params. React StrictMode in
  // dev double-mounts the effect; the page de-dupes concurrent
  // identical-key exchanges via `completeCallbackOnce` but two sequential
  // mounts can still produce two POSTs. We pin >= 1 and validate the
  // shape, which is what we actually care about.
  await expect.poll(() => cap.count()).toBeGreaterThanOrEqual(1);
  const body = cap.last()?.body as Record<string, unknown> | undefined;
  expect(body).toMatchObject({ code: VALID_CODE, state: VALID_STATE });

  // The localStorage compatibility shims are still in place — mockAuth
  // seeded them and a successful callback must not blow them away (the
  // legacy code paths that still inspect them rely on this).
  const stored = await page.evaluate(() => ({
    access: window.localStorage.getItem('of_access_token'),
    refresh: window.localStorage.getItem('of_refresh_token'),
  }));
  expect(stored.access).not.toBeNull();
  expect(stored.refresh).not.toBeNull();
});

test('valid callback honours `returnTo` from the URL', async ({ page }) => {
  await mockUsersMeAuthenticated(page);
  await mockSsoExchange(page, { kind: 'authenticated' });

  // The redirect-helper reads `?returnTo=…` (camelCase, the constant
  // AUTH_RETURN_TO_PARAM in src/lib/auth/redirects.ts). The value also
  // has to clear `sanitizeAuthReturnTo` — must be same-origin, start
  // with "/", and not point at an /auth/* route.
  await page.goto(
    `/auth/callback?code=${encodeURIComponent(VALID_CODE)}&state=${encodeURIComponent(VALID_STATE)}&returnTo=${encodeURIComponent('/datasets')}`,
  );

  await expect(page).toHaveURL(/\/datasets(\?|$)/);
});

test('mfa_required response: redirects to /auth/mfa', async ({ page }) => {
  await mockSsoExchange(page, { kind: 'mfa_required' });

  await page.goto(
    `/auth/callback?code=${encodeURIComponent(VALID_CODE)}&state=${encodeURIComponent(VALID_STATE)}`,
  );

  await expect(page).toHaveURL(/\/auth\/mfa(\?|$)/);
});

test('IdP error in query string (access_denied): renders the description', async ({ page }) => {
  const cap = captureRequests(page, SSO_ENDPOINT);

  await page.goto(
    '/auth/callback?error=access_denied&error_description=' +
      encodeURIComponent('User cancelled the consent prompt'),
  );

  // The component never reached the exchange step — pure URL-side error.
  await expect(
    errorAlert(page).filter({ hasText: /user cancelled the consent prompt/i }),
  ).toBeVisible();
  expect(cap.count()).toBe(0);

  // Back to login link is rendered (the "Try again" control in the
  // roadmap is implemented as a back-to-login Link).
  await expect(backToLoginLink(page)).toBeVisible();
  await expect(backToLoginLink(page)).toHaveAttribute('href', /\/auth\/login/);
});

test('server-side exchange failure (400): shows error and the back-to-login link', async ({ page }) => {
  // Covers the roadmap's "state mismatch", "token exchange fails 400",
  // and "provider deshabilitado" cases — they all manifest as a 4xx
  // from /auth/sso/callback and surface the same way in the UI.
  await mockSsoExchange(page, {
    kind: 'error',
    status: 400,
    message: 'Invalid state token',
  });

  await page.goto(
    `/auth/callback?code=${encodeURIComponent(VALID_CODE)}&state=${encodeURIComponent(VALID_STATE)}`,
  );

  await expect(errorAlert(page).filter({ hasText: /invalid state token/i })).toBeVisible();
  await expect(backToLoginLink(page)).toBeVisible();
  await expect(backToLoginLink(page)).toHaveAttribute('href', /\/auth\/login/);

  // The user is NOT redirected — they stay on /auth/callback so the
  // error stays visible until they click "Back to login".
  await expect(page).toHaveURL(/\/auth\/callback/);
});

test('provider-disabled (403) surfaces the same way as 400', async ({ page }) => {
  // The shipped CallbackPage does not pre-check an IdP catalog; a
  // disabled provider comes back as a server-side rejection. We pin
  // the 403 path explicitly so a regression that changes status
  // handling doesn't slip through.
  await mockSsoExchange(page, {
    kind: 'error',
    status: 403,
    message: 'Identity provider is disabled',
  });

  await page.goto(
    `/auth/callback?code=${encodeURIComponent(VALID_CODE)}&state=${encodeURIComponent(VALID_STATE)}`,
  );

  await expect(
    errorAlert(page).filter({ hasText: /identity provider is disabled/i }),
  ).toBeVisible();
  await expect(backToLoginLink(page)).toBeVisible();
});

test('missing code AND state AND SAML params: shows "missing" error', async ({ page }) => {
  const cap = captureRequests(page, SSO_ENDPOINT);

  // No URL params at all — the page renders its own validation error
  // and never calls the exchange endpoint.
  await page.goto('/auth/callback');

  // The user-facing copy comes from the i18n bundle; we don't pin the
  // text. The presence of role="alert" + the absence of a network call
  // is the regression guard.
  await expect(errorAlert(page)).toBeVisible();
  expect(cap.count()).toBe(0);
  await expect(backToLoginLink(page)).toBeVisible();
});
