import { type Page } from '@playwright/test';
import { test, expect } from './fixtures/base';
import { DEFAULT_ERROR_ALLOWLIST } from './fixtures/base';
import { captureRequests } from './fixtures/api-mocks';
import { RecentPage } from './pages';

/**
 * E2E coverage for `/recent`
 * (apps/web/src/routes/recent/RecentPage.tsx).
 *
 * **Reality vs roadmap.** The shipped RecentPage is a flat table with
 * three columns — Resource / Kind / Last accessed — sourced from
 * `GET /api/v1/workspace/recents?limit=50` and labelled via
 * `POST /api/v1/workspace/resources/resolve`. There is NO grouping
 * by day (Today / Yesterday / This week), NO type filter, NO "Clear
 * recent" button, and NO `DELETE /api/v1/recent` endpoint. The
 * roadmap controls that map to those are folded into a single
 * "absent today" regression guard at the end of the file.
 */

test.use({
  errorAllowlist: {
    patterns: [
      ...DEFAULT_ERROR_ALLOWLIST,
      /Cannot read properties of undefined/,
      /React Router caught the following error during render/,
      /^console\.error: %o$/,
      /Failed to load recent activity/i,
    ],
  },
});

const RECENTS_LIST = /\/api\/v1\/workspace\/recents(\?|$)/;
const E2E_NOW = '2026-05-11T00:00:00Z';

interface RecentEntryFixture {
  resource_kind: string;
  resource_id: string;
  last_accessed_at: string;
}

interface ResolvedLabelFixture {
  resource_kind: string;
  resource_id: string;
  label: string;
}

function makeRecent(overrides: Partial<RecentEntryFixture> = {}): RecentEntryFixture {
  return {
    resource_kind: 'dataset',
    resource_id: 'dataset-1',
    last_accessed_at: E2E_NOW,
    ...overrides,
  };
}

interface RecentMockOptions {
  entries?: RecentEntryFixture[];
  labels?: ResolvedLabelFixture[];
  /** When set, every GET returns this status — see favorites.spec.ts
   *  for the StrictMode-double-mount rationale. */
  alwaysFail?: number;
}

async function mockRecents(page: Page, options: RecentMockOptions = {}): Promise<void> {
  const entries = options.entries ?? [];
  const labels = options.labels ?? [];

  await page.route(RECENTS_LIST, async (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    if (options.alwaysFail) {
      await route.fulfill({
        status: options.alwaysFail,
        json: { error: 'mocked failure' },
      });
      return;
    }
    await route.fulfill({ json: { data: entries } });
  });

  await page.route('**/api/v1/workspace/resources/resolve', async (route) => {
    if (route.request().method() !== 'POST') return route.fallback();
    await route.fulfill({ json: { data: labels } });
  });
}

const ALPHA = makeRecent({
  resource_kind: 'dataset',
  resource_id: 'dataset-alpha',
  last_accessed_at: '2026-05-20T10:00:00Z',
});
const BETA = makeRecent({
  resource_kind: 'pipeline',
  resource_id: 'pipeline-beta',
  last_accessed_at: '2026-05-19T22:00:00Z',
});

const ALPHA_LABEL: ResolvedLabelFixture = {
  resource_kind: 'dataset',
  resource_id: 'dataset-alpha',
  label: 'Alpha customers',
};
const BETA_LABEL: ResolvedLabelFixture = {
  resource_kind: 'pipeline',
  resource_id: 'pipeline-beta',
  label: 'Beta nightly',
};

test('renders mocked recents with resolved labels in a 3-column table', async ({
  adminPage,
}) => {
  await mockRecents(adminPage, {
    entries: [ALPHA, BETA],
    labels: [ALPHA_LABEL, BETA_LABEL],
  });

  const recent = new RecentPage(adminPage);
  await recent.goto();
  await recent.expectLoaded();

  await expect(
    adminPage.getByRole('heading', { level: 1, name: /^recent$/i }),
  ).toBeVisible();

  // 3-column table: Resource / Kind / Last accessed.
  await expect(adminPage.getByRole('columnheader', { name: /^resource$/i })).toBeVisible();
  await expect(adminPage.getByRole('columnheader', { name: /^kind$/i })).toBeVisible();
  await expect(
    adminPage.getByRole('columnheader', { name: /last accessed/i }),
  ).toBeVisible();

  // Both resolved labels rendered as Links.
  await expect(
    adminPage.getByRole('link', { name: /alpha customers/i }),
  ).toBeVisible();
  await expect(
    adminPage.getByRole('link', { name: /beta nightly/i }),
  ).toBeVisible();
});

test('renders a flat table (no day-bucket headings)', async ({ adminPage }) => {
  // Regression guard. The roadmap describes a day-grouped UI (Today /
  // Yesterday / This week). The shipped page is flat — if grouping
  // gets added without updating this test, the count goes > 0 and
  // forces an update.
  await mockRecents(adminPage, {
    entries: [ALPHA, BETA],
    labels: [ALPHA_LABEL, BETA_LABEL],
  });
  await new RecentPage(adminPage).goto();

  // Only one `<h1>Recent</h1>` exists; there should be no h2/h3 with
  // the bucket labels the roadmap describes.
  for (const bucket of [/^today$/i, /^yesterday$/i, /this week/i]) {
    expect(await adminPage.getByRole('heading', { name: bucket }).count()).toBe(0);
  }
});

test('clicking a recent row navigates to the resource path', async ({ adminPage }) => {
  await mockRecents(adminPage, {
    entries: [ALPHA],
    labels: [ALPHA_LABEL],
  });
  await new RecentPage(adminPage).goto();

  const link = adminPage.getByRole('link', { name: /alpha customers/i });
  await expect(link).toHaveAttribute('href', /^\/datasets\//);
  await link.click();

  await expect(adminPage).toHaveURL(/^http:\/\/127\.0\.0\.1:55173\/datasets\//);
});

test('empty state: "No visible recent resources."', async ({ adminPage }) => {
  await mockRecents(adminPage, { entries: [], labels: [] });
  await new RecentPage(adminPage).goto();

  await expect(adminPage.getByText(/no visible recent resources/i)).toBeVisible();
  // The table doesn't render — no row links exist.
  expect(await adminPage.getByRole('link', { name: /alpha|beta/i }).count()).toBe(0);
});

test('list endpoint failure surfaces the error banner', async ({ adminPage }) => {
  await mockRecents(adminPage, { alwaysFail: 500 });
  await new RecentPage(adminPage).goto();

  await expect(adminPage.locator('.of-status-danger')).toBeVisible();
});

test('reachable from the AppShell sidebar', async ({ adminPage }) => {
  await mockRecents(adminPage, { entries: [], labels: [] });
  await adminPage.goto('/');

  await adminPage
    .getByRole('navigation', { name: /primary navigation/i })
    .getByRole('link', { name: /^recent$/i })
    .click();

  await expect(adminPage).toHaveURL(/\/recent(\?|$)/);
  await expect(
    adminPage.getByRole('heading', { level: 1, name: /^recent$/i }),
  ).toBeVisible();
});

test('absent today: day-bucket grouping, type filter, and "Clear recent" CTA', async ({
  adminPage,
}) => {
  // Regression guards for the three roadmap controls that don't ship
  // today. When the feature catches up, each `count() === 0` below
  // will flip and force the developer to update this spec.
  await mockRecents(adminPage, {
    entries: [ALPHA, BETA],
    labels: [ALPHA_LABEL, BETA_LABEL],
  });
  await new RecentPage(adminPage).goto();

  // No day-bucket headings (covered separately above but kept here as
  // the canonical "absent" inventory).
  expect(
    await adminPage.getByRole('heading', { name: /^(today|yesterday|this week)/i }).count(),
  ).toBe(0);

  // No type-filter chip / button.
  expect(
    await adminPage.getByRole('button', { name: /^(datasets?|pipelines?|projects?)$/i }).count(),
  ).toBe(0);

  // No "Clear recent" CTA — the page also has no DELETE endpoint for
  // /api/v1/recent (the roadmap's path doesn't exist in the codebase).
  expect(
    await adminPage.getByRole('button', { name: /clear (recent|history)/i }).count(),
  ).toBe(0);

  // No DELETE call escapes during the page lifetime.
  const deletes = captureRequests(adminPage, /\/api\/v1\/(recent|workspace\/recents)/);
  await adminPage.waitForTimeout(300);
  const deleteCalls = deletes.calls.filter((c) => c.method === 'DELETE');
  expect(deleteCalls).toHaveLength(0);
});
