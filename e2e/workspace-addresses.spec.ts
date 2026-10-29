import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import { adminAPI, createHTTPMonitor, deleteCreated, randomID } from './support/api';
import { targetURL } from './support/fake-target';

// The web app's addresses are /w/<workspaceId>/..., and every request it sends
// to a workspace's data names that workspace in X-Workspace-ID (PP-147).
// An address without a workspace, or with one the user isn't in, opens the same
// page in the default workspace.

const UUID = '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}';

// A workspace the user is in nowhere: a random id that no workspace has.
const strangerWorkspace = () => crypto.randomUUID();

async function defaultWorkspaceOf(api: APIRequestContext): Promise<string> {
  const me = await api.get('me');
  expect(me.ok(), await me.text()).toBeTruthy();
  const { defaultWorkspaceId } = (await me.json()).data;
  expect(defaultWorkspaceId).toMatch(new RegExp(`^${UUID}$`));
  return defaultWorkspaceId;
}

// Records the X-Workspace-ID header of every request to the API's path.
function recordWorkspaceHeaders(page: Page, apiPath: string) {
  const headers: (string | undefined)[] = [];
  page.on('request', (request) => {
    if (new URL(request.url()).pathname === apiPath) headers.push(request.headers()['x-workspace-id']);
  });
  return headers;
}

test('an old address opens the same page in the default workspace', async ({ page }) => {
  const api = await adminAPI(page);
  const workspace = await defaultWorkspaceOf(api);

  await page.goto('/monitors');
  await expect(page).toHaveURL(new RegExp(`/w/${workspace}/monitors$`));
  await expect(page.getByTestId('create-entity')).toBeVisible();

  // The search and the hash go along
  await page.goto('/monitors?search=nothing-matches-this');
  await expect(page).toHaveURL(new RegExp(`/w/${workspace}/monitors\\?search=nothing-matches-this$`));
  await expect(page.getByText('No monitors found')).toBeVisible();

  await page.goto('/settings#error-reports');
  await expect(page).toHaveURL(new RegExp(`/w/${workspace}/settings#error-reports$`));

  // The root opens the monitors
  await page.goto('/');
  await expect(page).toHaveURL(new RegExp(`/w/${workspace}/monitors$`));
});

test('an old address of a monitor opens that monitor in the default workspace', async ({ page }) => {
  const api = await adminAPI(page);
  const workspace = await defaultWorkspaceOf(api);
  const id = randomID();
  const name = `Address ${id}`;
  const monitor = await createHTTPMonitor(api, name, targetURL(id));
  try {
    await page.goto(`/monitors/${monitor.id}`);
    await expect(page).toHaveURL(new RegExp(`/w/${workspace}/monitors/${monitor.id}$`));
    await expect(page.getByRole('heading', { name }).first()).toBeVisible();

    await page.goto(`/monitors/${monitor.id}/edit`);
    await expect(page).toHaveURL(new RegExp(`/w/${workspace}/monitors/${monitor.id}/edit$`));
    await expect(page.locator('input[name="name"]')).toHaveValue(name);
  } finally {
    await deleteCreated(api, `monitors/${monitor.id}`);
  }
});

test('an address naming a workspace the user is not in opens the default workspace', async ({ page }) => {
  const api = await adminAPI(page);
  const workspace = await defaultWorkspaceOf(api);
  const id = randomID();
  const name = `Stranger ${id}`;
  const monitor = await createHTTPMonitor(api, name, targetURL(id));
  try {
    await page.goto(`/w/${strangerWorkspace()}/monitors`);
    await expect(page).toHaveURL(new RegExp(`/w/${workspace}/monitors$`));
    await expect(page.getByTestId('create-entity')).toBeVisible();

    // The rest of the path is kept
    await page.goto(`/w/${strangerWorkspace()}/monitors/${monitor.id}`);
    await expect(page).toHaveURL(new RegExp(`/w/${workspace}/monitors/${monitor.id}$`));
    await expect(page.getByRole('heading', { name }).first()).toBeVisible();

    // So is an id that isn't a workspace id at all
    await page.goto('/w/not-a-workspace/settings');
    await expect(page).toHaveURL(new RegExp(`/w/${workspace}/settings$`));
  } finally {
    await deleteCreated(api, `monitors/${monitor.id}`);
  }
});

test('the monitor list is requested in the workspace of the address', async ({ page }) => {
  const api = await adminAPI(page);
  const workspace = await defaultWorkspaceOf(api);
  const monitorHeaders = recordWorkspaceHeaders(page, '/api/v1/monitors');

  await page.goto(`/w/${workspace}/monitors`);
  await expect(page.getByTestId('create-entity')).toBeVisible();
  await page.waitForLoadState('networkidle');

  expect(monitorHeaders.length).toBeGreaterThan(0);
  expect(monitorHeaders).toEqual(monitorHeaders.map(() => workspace));

  // The same after an old address redirected. /me, which says which workspaces
  // there are, goes out from that address and so names none.
  monitorHeaders.length = 0;
  const meHeaders = recordWorkspaceHeaders(page, '/api/v1/me');
  await page.goto('/monitors');
  await expect(page).toHaveURL(new RegExp(`/w/${workspace}/monitors$`));
  await expect(page.getByTestId('create-entity')).toBeVisible();
  await page.waitForLoadState('networkidle');
  expect(monitorHeaders.length).toBeGreaterThan(0);
  expect(monitorHeaders).toEqual(monitorHeaders.map(() => workspace));
  expect(meHeaders).toEqual([undefined]);
});

test('a link inside the app keeps the workspace in the address', async ({ page }) => {
  const api = await adminAPI(page);
  const workspace = await defaultWorkspaceOf(api);

  await page.goto(`/w/${workspace}/monitors`);
  await page.getByTestId('create-entity').click();
  await expect(page).toHaveURL(new RegExp(`/w/${workspace}/monitors/new$`));

  await page.getByRole('link', { name: 'Proxies' }).click();
  await expect(page).toHaveURL(new RegExp(`/w/${workspace}/proxies$`));
});
