import { test, expect } from '@playwright/test';
import { admin } from './support/admin';

// Logging out only clears the browser's session; the tokens are stateless, so signing in
// again in a fresh context leaves the saved session the other specs share untouched.
test('After a logout the API client sends no Authorization header, and a new login sends the new token', async ({
  browser,
}) => {
  const visitor = await browser.newContext({
    baseURL: test.info().project.use.baseURL,
    storageState: { cookies: [], origins: [] },
  });
  const page = await visitor.newPage();
  const requests: { method: string; path: string; authorization?: string }[] = [];
  page.on('request', (request) => {
    const { pathname } = new URL(request.url());
    if (pathname.startsWith('/api/v1/')) {
      requests.push({ method: request.method(), path: pathname, authorization: request.headers()['authorization'] });
    }
  });

  async function logIn(): Promise<string> {
    await page.getByRole('textbox', { name: 'Email' }).fill(admin.email);
    await page.locator('input[name="password"]').fill(admin.password);
    const loggedIn = page.waitForResponse((response) => response.url().endsWith('/auth/login'));
    await page.getByRole('button', { name: 'Login' }).click();
    const accessToken: string = (await (await loggedIn).json()).data.accessToken;
    await expect(page).toHaveURL(/\/monitors$/);
    await page.waitForLoadState('networkidle');
    return accessToken;
  }

  try {
    await page.goto('/login');
    await logIn();
    await expect(page.getByTestId('create-entity')).toBeVisible();

    // Logging out does not reload the page, so whatever the API client holds stays in memory.
    requests.length = 0;
    await page.getByRole('button', { name: admin.email }).click();
    await page.getByRole('menuitem', { name: 'Log out' }).click();
    await expect(page).toHaveURL(/\/login$/);

    // The login request is the first request of the signed-out page, so it is among the ones
    // recorded since the logout.
    const accessToken = await logIn();
    const sinceLogout = requests.splice(0);
    const login = sinceLogout.find(({ path }) => path === '/api/v1/auth/login');
    expect(login, 'the login request').toBeDefined();
    const signedOut = sinceLogout.slice(0, sinceLogout.indexOf(login!) + 1);
    expect(signedOut.filter(({ authorization }) => authorization).map(({ method, path }) => `${method} ${path}`)).toEqual(
      [],
    );

    // The page keeps the monitor list it loaded before the logout, so open a page it has not
    // loaded yet, still without a reload, and check that its requests carry the new token.
    await page.getByRole('link', { name: 'Notification Channels' }).click();
    await expect(page).toHaveURL(/\/notification-channels$/);
    await page.waitForLoadState('networkidle');
    const afterLogin = [...sinceLogout.slice(sinceLogout.indexOf(login!) + 1), ...requests];
    expect(afterLogin.length, 'requests after the login').toBeGreaterThan(0);
    expect(
      afterLogin.filter(({ authorization }) => authorization !== `Bearer ${accessToken}`).map(({ path }) => path),
    ).toEqual([]);
  } finally {
    await visitor.close();
  }
});
