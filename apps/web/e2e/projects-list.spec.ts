import { type Page, type Route } from '@playwright/test';
import { test, expect } from './fixtures/base';
import { DEFAULT_ERROR_ALLOWLIST } from './fixtures/base';
import { captureRequests } from './fixtures/api-mocks';

/**
 * E2E coverage for `/projects`
 * (apps/web/src/routes/projects/ProjectsListPage.tsx).
 *
 * **Reality vs roadmap.** The shipped page is a 5-section Compass-style
 * sub-nav (Portfolios / Projects / Your files / Shared / Trash) with a
 * locally-filtered table, a "+ New" button that opens a TEMPLATE-FIRST
 * multi-step CreateProjectModal, and an inline "Move to trash" confirm.
 * The page does NOT match several roadmap claims:
 *
 *   - The new-project button is labelled "+ New", not "New project".
 *   - The create modal is a wizard (template → form), not a single
 *     name+description dialog.
 *   - Delete is a "Move to trash" `ConfirmDialog`, NOT a "type project
 *     name to confirm" destructive pattern.
 *   - There is no per-row inline edit nor a "..." menu — the row's
 *     `aria-label="More actions for {name}"` button directly opens
 *     the delete confirm.
 *   - The search input is a LOCAL filter (no backend re-query) and
 *     there is no sort UI nor pagination — `listProjects` is called
 *     with `per_page: 200` and that's it.
 *   - The "+ New" button is rendered unconditionally; the page does
 *     not gate it by permissions (the server rejects unauthorised
 *     POSTs). The roadmap's "viewer doesn't see New" case is folded
 *     into the "absent today" regression guard at the end of the
 *     file, NOT enforced as a passing test.
 *
 * Endpoints exercised:
 *   - GET    /api/v1/ontology/projects?per_page=200
 *   - DELETE /api/v1/ontology/projects/{id}
 *   - POST   /api/v1/ontology/projects                   (modal submit)
 *   - GET    /api/v1/workspace/shared-with-me?limit=200  (shared tab)
 *   - GET    /api/v1/workspace/trash?limit=200           (trash tab)
 *   - GET    /api/v1/nexus/spaces                        (modal mount)
 *   - GET    /api/v1/ontology/projects/templates         (modal mount)
 *   - POST   /api/v1/file-access-presets/visible         (modal mount)
 */

test.use({
  errorAllowlist: {
    patterns: [
      ...DEFAULT_ERROR_ALLOWLIST,
      /Cannot read properties of undefined/,
      /React Router caught the following error during render/,
      /^console\.error: %o$/,
      /Failed to load/i,
    ],
  },
});

const PROJECTS_LIST = /\/api\/v1\/ontology\/projects(\?|$)/;
const PROJECTS_CREATE = /\/api\/v1\/ontology\/projects$/;
const PROJECT_DELETE = /\/api\/v1\/ontology\/projects\/[^/]+$/;
const E2E_NOW = '2026-05-11T00:00:00Z';

interface ProjectFixture {
  id: string;
  slug: string;
  display_name: string;
  description: string;
  workspace_slug: string;
  owner_id: string;
  created_at: string;
  updated_at: string;
}

function makeProject(overrides: Partial<ProjectFixture> = {}): ProjectFixture {
  return {
    id: 'project-1',
    slug: 'analytics',
    display_name: 'Analytics',
    description: 'Sample analytics collection',
    workspace_slug: 'default',
    owner_id: '00000000-0000-0000-0000-000000000001',
    created_at: E2E_NOW,
    updated_at: E2E_NOW,
    ...overrides,
  };
}

interface ProjectsMockState {
  projects: ProjectFixture[];
}

async function mockProjects(
  page: Page,
  initial: { projects: ProjectFixture[] },
): Promise<ProjectsMockState> {
  const state: ProjectsMockState = { projects: initial.projects.slice() };

  // GET /ontology/projects (any query string).
  await page.route(PROJECTS_LIST, async (route) => {
    const method = route.request().method();
    if (method === 'GET') {
      // Don't intercept the templates list — that's a sub-path handled
      // by mockCreateProjectModalDeps below.
      if (route.request().url().includes('/templates')) return route.fallback();
      await route.fulfill({
        json: {
          data: state.projects,
          total: state.projects.length,
          page: 1,
          per_page: 200,
        },
      });
      return;
    }
    if (method === 'POST') {
      // create — defer to a more specific handler in the success test.
      return route.fallback();
    }
    return route.fallback();
  });

  // DELETE /ontology/projects/:id — supports both the "Move to trash" flow
  // and the absent-today guards that don't expect any DELETE to fire.
  await page.route(PROJECT_DELETE, async (route) => {
    const method = route.request().method();
    if (method !== 'DELETE') return route.fallback();
    const match = /\/ontology\/projects\/([^/?#]+)(?:\?|$)/.exec(route.request().url());
    if (match) {
      state.projects = state.projects.filter((entry) => entry.id !== match[1]);
    }
    await route.fulfill({ status: 204, body: '' });
  });

  // Empty stubs for the other section endpoints.
  await page.route('**/api/v1/workspace/shared-with-me**', async (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    await route.fulfill({ json: { data: [] } });
  });
  await page.route('**/api/v1/workspace/trash**', async (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    await route.fulfill({ json: { data: [] } });
  });

  return state;
}

/**
 * Stubs the three endpoints the CreateProjectModal calls on mount so the
 * template-picker step renders with at least one selectable template.
 */
async function mockCreateProjectModalDeps(page: Page): Promise<void> {
  await page.route('**/api/v1/nexus/spaces', async (route: Route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    await route.fulfill({
      json: {
        items: [
          {
            id: 'space-1',
            slug: 'default',
            display_name: 'Default workspace',
            description: '',
          },
        ],
      },
    });
  });
  await page.route('**/api/v1/ontology/projects/templates', async (route: Route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    await route.fulfill({
      json: {
        data: [
          {
            id: 'template-blank',
            key: 'blank',
            name: 'Blank project',
            description: 'Start with an empty workspace.',
            default_role: 'editor',
          },
        ],
      },
    });
  });
  await page.route('**/api/v1/file-access-presets/visible', async (route: Route) => {
    if (route.request().method() !== 'POST') return route.fallback();
    await route.fulfill({
      json: { presets: [], default_preset_id: null, warning: null },
    });
  });
}

const ALPHA = makeProject({
  id: 'project-alpha',
  slug: 'analytics',
  display_name: 'Analytics',
});
const BETA = makeProject({
  id: 'project-beta',
  slug: 'finance',
  display_name: 'Finance',
  description: 'Quarterly numbers',
});

test('renders the mocked projects in the Projects section table', async ({ adminPage }) => {
  await mockProjects(adminPage, { projects: [ALPHA, BETA] });
  await adminPage.goto('/projects');

  await expect(
    adminPage.getByRole('heading', { level: 1, name: /^projects$/i }),
  ).toBeVisible();

  // Both display_name Links visible.
  await expect(adminPage.getByRole('link', { name: /^Analytics$/i })).toBeVisible();
  await expect(adminPage.getByRole('link', { name: /^Finance$/i })).toBeVisible();

  // The "More actions for <name>" row buttons exist (one per row).
  await expect(
    adminPage.getByRole('button', { name: /more actions for Analytics/i }),
  ).toBeVisible();
  await expect(
    adminPage.getByRole('button', { name: /more actions for Finance/i }),
  ).toBeVisible();
});

test('section tabs: switching to "Your files" updates ?section= and stays on /projects', async ({
  adminPage,
}) => {
  await mockProjects(adminPage, { projects: [ALPHA] });
  await adminPage.goto('/projects');

  // All five Compass section tabs are rendered.
  for (const label of ['Portfolios', 'Projects', 'Your files', 'Shared with you', 'Trash']) {
    await expect(adminPage.getByRole('tab', { name: label })).toBeVisible();
  }

  // `projects` is the default — URL has no `?section=`.
  await expect(adminPage).toHaveURL(/\/projects$/);

  // Switch to "Your files".
  await adminPage.getByRole('tab', { name: 'Your files' }).click();
  await expect(adminPage).toHaveURL(/[?&]section=your-files\b/);
});

test('row click navigates to the per-project route', async ({ adminPage }) => {
  await mockProjects(adminPage, { projects: [ALPHA] });
  await adminPage.goto('/projects');

  const link = adminPage.getByRole('link', { name: /^Analytics$/i });
  await expect(link).toHaveAttribute('href', /^\/projects\//);
  await link.click();

  await expect(adminPage).toHaveURL(/^http:\/\/127\.0\.0\.1:55173\/projects\//);
});

test('local search filters the table without re-querying the API', async ({ adminPage }) => {
  await mockProjects(adminPage, { projects: [ALPHA, BETA] });
  const cap = captureRequests(adminPage, PROJECTS_LIST);
  await adminPage.goto('/projects');

  // Wait for the initial GET so we can measure additional calls.
  await expect(adminPage.getByRole('link', { name: /^Analytics$/i })).toBeVisible();
  const initialCalls = cap.calls.filter((c) => c.method === 'GET').length;

  await adminPage.getByPlaceholder(/search projects/i).fill('finance');

  // Only Finance row remains.
  await expect(adminPage.getByRole('link', { name: /^Analytics$/i })).toHaveCount(0);
  await expect(adminPage.getByRole('link', { name: /^Finance$/i })).toBeVisible();

  // No extra fetch fired — the filter is local.
  await adminPage.waitForTimeout(200);
  const finalCalls = cap.calls.filter((c) => c.method === 'GET').length;
  expect(finalCalls).toBe(initialCalls);
});

test('"+ New" opens the CreateProjectModal with the template-picker step', async ({
  adminPage,
}) => {
  await mockProjects(adminPage, { projects: [ALPHA] });
  await mockCreateProjectModalDeps(adminPage);
  await adminPage.goto('/projects');

  await adminPage.getByRole('button', { name: /^\+? ?New$/ }).click();

  // Modal heading.
  await expect(
    adminPage.getByRole('heading', { name: /create new project/i }),
  ).toBeVisible();

  // The mocked template renders as a button — its accessible name
  // concatenates the template name + description.
  await expect(
    adminPage.getByRole('button', { name: /blank project/i }).first(),
  ).toBeVisible();
});

test('validation: the "Create project" submit button is disabled while name is empty', async ({
  adminPage,
}) => {
  await mockProjects(adminPage, { projects: [] });
  await mockCreateProjectModalDeps(adminPage);
  await adminPage.goto('/projects');

  await adminPage.getByRole('button', { name: /^\+? ?New$/ }).click();
  await adminPage.getByRole('button', { name: /blank project/i }).first().click();

  // Form step now visible: Name + description + submit.
  const submit = adminPage.getByRole('button', { name: /create project/i });

  // The default name pre-fill is non-empty (`Learning <user>`), so we
  // clear it to exercise the empty-name guard.
  const nameInput = adminPage.getByLabel(/^name$/i);
  await nameInput.fill('');
  await expect(submit).toBeDisabled();

  // Re-filling enables the submit.
  await nameInput.fill('Quarterly review');
  await expect(submit).toBeEnabled();
});

test('successful create: POSTs the project and redirects to /projects/:id', async ({
  adminPage,
}) => {
  await mockProjects(adminPage, { projects: [] });
  await mockCreateProjectModalDeps(adminPage);
  const cap = captureRequests(adminPage, PROJECTS_CREATE);

  // Override the POST in front of the GET-only mock from mockProjects.
  await adminPage.route('**/api/v1/ontology/projects', async (route) => {
    if (route.request().method() !== 'POST') return route.fallback();
    await route.fulfill({
      status: 201,
      json: {
        id: 'project-new-1',
        slug: 'new-collection',
        display_name: 'New collection',
        description: 'Created in E2E',
        workspace_slug: 'default',
        owner_id: '00000000-0000-0000-0000-000000000001',
        created_at: E2E_NOW,
        updated_at: E2E_NOW,
      },
    });
  });

  await adminPage.goto('/projects');

  await adminPage.getByRole('button', { name: /^\+? ?New$/ }).click();
  await adminPage.getByRole('button', { name: /blank project/i }).first().click();
  await adminPage.getByLabel(/^name$/i).fill('New collection');
  await adminPage.getByLabel(/project description/i).fill('Created in E2E');
  await adminPage.getByRole('button', { name: /create project/i }).click();

  await expect.poll(() => cap.count()).toBeGreaterThanOrEqual(1);
  const body = cap.last()?.body as Record<string, unknown> | undefined;
  expect(body).toMatchObject({
    display_name: 'New collection',
    description: 'Created in E2E',
    template_key: 'blank',
  });

  // After the modal calls onCreated, the page navigates to the new
  // project's stable URL (a `/projects/<rid-slug>` path).
  await expect(adminPage).toHaveURL(/\/projects\/[^/]+/);
});

test('delete: more-actions opens the Move-to-trash confirm and DELETEs the project', async ({
  adminPage,
}) => {
  await mockProjects(adminPage, { projects: [ALPHA, BETA] });
  const cap = captureRequests(adminPage, PROJECT_DELETE);
  await adminPage.goto('/projects');

  await adminPage
    .getByRole('button', { name: /more actions for Analytics/i })
    .click();

  // The shipped delete flow is a single ConfirmDialog — `role="dialog"`
  // with the title in a `<div aria-labelledby>` (not a heading
  // element). NOT a "type project name to confirm" destructive
  // pattern. Match via the dialog's accessible name.
  const dialog = adminPage.getByRole('dialog', { name: /move project to trash/i });
  await expect(dialog).toBeVisible();
  await expect(
    dialog.getByText(/Move "Analytics" to trash\?/i),
  ).toBeVisible();

  await adminPage.getByRole('button', { name: /move to trash/i }).click();

  await expect.poll(() => cap.count()).toBeGreaterThanOrEqual(1);
  const call = cap.last();
  expect(call?.method).toBe('DELETE');
  expect(call?.url).toMatch(/\/ontology\/projects\/project-alpha$/);

  // The stateful mock + page reload removes the row.
  await expect(
    adminPage.getByRole('link', { name: /^Analytics$/i }),
  ).toHaveCount(0);
  await expect(adminPage.getByRole('link', { name: /^Finance$/i })).toBeVisible();
});

test('"Manage spaces" dropdown opens with its menu items', async ({ adminPage }) => {
  await mockProjects(adminPage, { projects: [ALPHA] });
  await adminPage.goto('/projects');

  const toggle = adminPage.getByRole('button', { name: /manage spaces/i });
  await expect(toggle).toHaveAttribute('aria-haspopup', 'menu');

  await toggle.click();

  const menu = adminPage.getByRole('menu');
  await expect(menu).toBeVisible();
  await expect(menu.getByRole('menuitem', { name: /open trash/i })).toBeVisible();
  await expect(menu.getByRole('menuitem', { name: /workspace settings/i })).toBeVisible();
  await expect(menu.getByRole('menuitem', { name: /^refresh$/i })).toBeVisible();
});

test('absent today: per-row "..." menu, type-to-confirm dialog, sort UI, pagination, and viewer-hide of "+ New"', async ({
  adminPage,
  browser,
}) => {
  // Single regression guard for the five roadmap controls that don't
  // ship today. When each one lands, the matching assertion flips and
  // forces an update.
  await mockProjects(adminPage, { projects: [ALPHA, BETA] });
  await adminPage.goto('/projects');

  // 1. No "..." menu with multiple items — the row button is a single
  //    "More actions for {name}" trigger that opens the delete confirm
  //    directly. There is no menuitem named "Edit" or "Rename".
  await adminPage
    .getByRole('button', { name: /more actions for Analytics/i })
    .click();
  expect(await adminPage.getByRole('menuitem', { name: /^(edit|rename)$/i }).count()).toBe(0);
  // Close the confirm so subsequent assertions aren't blocked.
  await adminPage.getByRole('button', { name: /^cancel$/i }).click();

  // 2. No "type project name to confirm" pattern — the confirm dialog
  //    above was a plain ConfirmDialog with no extra typed-name input.
  expect(
    await adminPage.getByPlaceholder(/type .* (name )?to confirm/i).count(),
  ).toBe(0);

  // 3. No sort UI.
  expect(await adminPage.getByRole('button', { name: /^sort/i }).count()).toBe(0);
  expect(
    await adminPage.getByRole('columnheader', { name: /^name/i }).getByRole('button').count(),
  ).toBe(0);

  // 4. No pagination controls — the page loads `per_page: 200` and
  //    renders all rows at once.
  expect(await adminPage.getByRole('button', { name: /next page|previous page/i }).count()).toBe(0);

  // 5. "+ New" is rendered for viewers too — the page does NOT gate it
  //    by permissions. Open a fresh context with the viewer identity
  //    and assert the button is visible (this pins current behaviour;
  //    a permission-aware UI ticket will flip the assertion).
  const viewerContext = await browser.newContext();
  try {
    const viewerPage = await viewerContext.newPage();
    const { mockAuth } = await import('./fixtures/mocks');
    await mockAuth(viewerPage, {
      authenticated: true,
      user: { roles: ['viewer'], permissions: ['read:*'] },
    });
    await viewerPage.route(PROJECTS_LIST, async (route) => {
      if (route.request().method() !== 'GET') return route.fallback();
      await route.fulfill({
        json: { data: [ALPHA], total: 1, page: 1, per_page: 200 },
      });
    });
    await viewerPage.goto('/projects');
    await expect(
      viewerPage.getByRole('button', { name: /^\+? ?New$/ }),
    ).toBeVisible();
  } finally {
    await viewerContext.close();
  }
});
