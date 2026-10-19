import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import { adminAPI, createHTTPMonitor, deleteCreated, randomID } from './support/api';
import { setTargetStatus, targetURL } from './support/fake-target';

// A new monitor is checked at once and then every 20 seconds, the minimum
// check interval. If the producer misses the create event, its 30-second
// refresh picks the monitor up. The heartbeat then still has to reach the page.
const nextCheck = { timeout: 60_000 };

// A page that has joined its websocket room shows a heartbeat within moments of
// another page showing it. Pending itself lasts one 20-second retry interval.
const sameHeartbeat = { timeout: 10_000 };

test('a failing target shows Pending, then Down, on the card and the page', async ({ page, context, request }) => {
  test.setTimeout(240_000);
  const id = randomID();
  const name = `Flaky ${id}`;
  const created: string[] = [];

  await setTargetStatus(request, id, 200);
  const api = await adminAPI(page);
  try {
    // One failed check with max retries 1 is Pending, the next one Down. A
    // monitor's first check is Down at once, so it has to start Up.
    const monitor = await createHTTPMonitor(api, name, targetURL(id), { maxRetries: 1 });
    created.push(`monitors/${monitor.id}`);

    // Both pages join their rooms before the target fails and stay put until
    // Down: Pending is too short to reload for, and they update live.
    const monitorPage = await context.newPage();
    await openLive(page, `/monitors?search=${id}`, 'monitor:all');
    await openLive(monitorPage, `/monitors/${monitor.id}`, `monitor:${monitor.id}`);
    const onCard = cardStatusOf(page, name);
    const onPage = pageStatusOf(monitorPage);

    await expect(onCard).toHaveText('Up', nextCheck);
    await expect(onPage).toHaveText('Up', nextCheck);

    await setTargetStatus(request, id, 500);
    await expect(onCard).toHaveText('Pending', nextCheck);
    await expect(onPage).toHaveText('Pending', sameHeartbeat);

    await expect(onCard).toHaveText('Down', nextCheck);
    await expect(onPage).toHaveText('Down', sameHeartbeat);
  } finally {
    for (const path of created.reverse()) await deleteCreated(api, path);
    await api.dispose();
  }
});

test('a monitor in a maintenance window shows Maintenance on the card and the page', async ({ page, context }) => {
  test.setTimeout(240_000);
  const id = randomID();
  const name = `Planned ${id}`;
  const created: string[] = [];

  const api = await adminAPI(page);
  try {
    const monitor = await createHTTPMonitor(api, name, targetURL(id));
    created.push(`monitors/${monitor.id}`);
    // A manual window that's switched on covers its monitors at once. Its
    // monitors' checks then record Maintenance without reaching the target.
    created.push(`maintenances/${await createManualMaintenance(api, `Window ${id}`, [monitor.id])}`);

    const monitorPage = await context.newPage();
    await page.goto(`/monitors?search=${id}`);
    await monitorPage.goto(`/monitors/${monitor.id}`);

    await expect(cardStatusOf(page, name)).toHaveText('Maintenance', nextCheck);
    await expect(pageStatusOf(monitorPage)).toHaveText('Maintenance', nextCheck);
  } finally {
    for (const path of created.reverse()) await deleteCreated(api, path);
    await api.dispose();
  }
});

// Opens the page and waits until the app has joined the websocket room, so a
// heartbeat recorded from then on reaches the page without a reload.
async function openLive(page: Page, url: string, room: string) {
  const joined = page.waitForEvent('console', (message) => message.text() === `Subscribed to heartbeat ${room}`);
  await Promise.all([joined, page.goto(url)]);
}

// The status name beside the icon on the monitor's card in the list. It's matched
// as an element of its own, since the card's text runs on from the status into the name.
const cardStatusOf = (page: Page, name: string) =>
  page
    .getByTestId('monitor-card')
    .filter({ hasText: name })
    .getByText(/^\s*(Up|Down|Pending|Maintenance|Paused)\s*$/);

// The "Current status" on the monitor page.
const pageStatusOf = (page: Page) =>
  page.locator('[data-slot="card"]').filter({ hasText: 'Current status' }).locator('.text-2xl');

async function createManualMaintenance(api: APIRequestContext, title: string, monitorIDs: string[]): Promise<string> {
  const response = await api.post('maintenances', {
    data: { title, description: '', active: true, strategy: 'manual', monitor_ids: monitorIDs },
  });
  expect(response.status(), await response.text()).toBe(201);
  return (await response.json()).data.id;
}
