import { defineConfig, devices } from '@playwright/test';

const isCI = Boolean(process.env.CI);

/**
 * Browser projects can be filtered via env to keep local iteration fast.
 * Set `E2E_BROWSERS=chromium,firefox,webkit` (default: chromium) to opt in.
 * CI runs all three via the `ci-frontend.yml` matrix.
 */
const enabledBrowsers = (process.env.E2E_BROWSERS ?? 'chromium')
  .split(',')
  .map((b) => b.trim())
  .filter(Boolean);

const browserProjects = [
  {
    name: 'chromium',
    use: { ...devices['Desktop Chrome'] },
  },
  {
    name: 'firefox',
    use: { ...devices['Desktop Firefox'] },
  },
  {
    name: 'webkit',
    use: { ...devices['Desktop Safari'] },
  },
  {
    name: 'mobile-chrome',
    use: { ...devices['Pixel 5'] },
  },
].filter((p) => enabledBrowsers.includes(p.name));

export default defineConfig({
  testDir: './e2e',
  snapshotDir: './e2e/__snapshots__',
  outputDir: './test-results/playwright',

  // Determinism.
  fullyParallel: true,
  forbidOnly: isCI,
  retries: isCI ? 2 : 0,
  workers: isCI ? 2 : undefined,

  // Sensible bounds — long enough for lazy chunks to load, short enough that
  // a hung locator fails fast.
  timeout: 30_000,
  expect: { timeout: 7_500 },

  reporter: isCI
    ? [
        ['github'],
        ['html', { outputFolder: 'playwright-report', open: 'never' }],
        ['junit', { outputFile: 'test-results/junit.xml' }],
        ['list'],
      ]
    : [['html', { outputFolder: 'playwright-report', open: 'never' }], ['list']],

  use: {
    baseURL: 'http://127.0.0.1:55173',
    actionTimeout: 10_000,
    navigationTimeout: 20_000,
    trace: isCI ? 'on-first-retry' : 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: isCI ? 'retain-on-failure' : 'off',
    // Snapshot pixel tolerance — small enough to catch real regressions,
    // forgiving enough to survive font hinting differences across runners.
    // Per-assertion overrides go on `toHaveScreenshot({ maxDiffPixelRatio })`.
  },

  webServer: {
    command: 'pnpm dev -- --host 127.0.0.1',
    url: 'http://127.0.0.1:55173',
    reuseExistingServer: !isCI,
    timeout: 120_000,
    stdout: 'pipe',
    stderr: 'pipe',
  },

  projects: browserProjects.length > 0 ? browserProjects : [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
});
