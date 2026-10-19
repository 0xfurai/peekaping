import { test, expect, type Page } from '@playwright/test';

// Loads the monitors page as the logged-in user and records every API request
// the page sends until the network is idle.
async function loadMonitorsPage(page: Page) {
  const requests: { method: string; url: URL; authorization?: string }[] = [];
  page.on('request', (request) => {
    const url = new URL(request.url());
    if (url.pathname.startsWith('/api/v1/')) {
      requests.push({
        method: request.method(),
        url,
        authorization: request.headers()['authorization'],
      });
    }
  });

  await page.goto('/monitors');
  await page.waitForLoadState('networkidle');
  await expect(page.getByTestId('create-entity')).toBeVisible();

  // The API lives at window.__CONFIG__.API_URL; when that is empty it is the web origin.
  const apiUrl: string = await page.evaluate(() => (window as any).__CONFIG__?.API_URL ?? '');
  const apiOrigin = new URL(apiUrl, page.url()).origin;

  return { requests, apiOrigin };
}

test.describe('API client setup', () => {
  test('every API request goes to the configured API URL', async ({ page }) => {
    const { requests, apiOrigin } = await loadMonitorsPage(page);

    expect(requests.length).toBeGreaterThan(0);
    expect(requests.filter(({ url }) => url.origin !== apiOrigin).map(({ url }) => url.href)).toEqual([]);
  });

  test('a page load sends one version request', async ({ page }) => {
    const { requests } = await loadMonitorsPage(page);

    const versionRequests = requests.filter(({ method, url }) => method === 'GET' && url.pathname === '/api/v1/version');
    expect(versionRequests.map(({ url }) => url.href)).toHaveLength(1);
  });

  test('every API request of a logged-in user carries the Authorization header', async ({ page }) => {
    const { requests } = await loadMonitorsPage(page);

    // The monitor list is an authenticated endpoint, so the page did make one.
    expect(requests.some(({ url }) => url.pathname === '/api/v1/monitors')).toBe(true);
    expect(
      requests.filter(({ authorization }) => !authorization?.startsWith('Bearer ')).map(({ url }) => url.href)
    ).toEqual([]);
  });
});
