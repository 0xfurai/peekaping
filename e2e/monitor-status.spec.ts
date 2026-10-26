import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import { adminAPI, createHTTPMonitor, deleteCreated, randomID } from './support/api';
import { setTargetStatus, targetURL } from './support/fake-target';
import { cardStatusOf, listFor, openLive } from './support/monitor-list';

// A new monitor is checked at once and then every 20 seconds, the minimum
// check interval. If whatever runs the checks misses the create event, its next
// refresh (within about 30 seconds) picks the monitor up. The heartbeat then still has to reach
// the page.
const nextCheck = { timeout: 60_000 };

// A page that has joined its websocket room shows a heartbeat within moments of
// another page showing it. Pending itself lasts one 20-second check: the server
// still schedules it at the check interval, not the retry interval (PP-65).
const sameHeartbeat = { timeout: 10_000 };

test('a failing target shows Pending, then Down, on the card and the page', async ({ page, context, request }) => {
  // If every wait runs out they add up to 280 seconds: 2 x 10 to join the rooms,
  // then 60, 60, 60, 10, 60 and 10 for the statuses. The timeout has to be longer,
  // so a failing wait fails on its own assertion and the finally block still runs.
  test.setTimeout(300_000);
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
    await openLive(page, listFor(id), 'monitor:all');
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
  // If every wait runs out they add up to 140 seconds: 2 x 10 to join the rooms
  // and 2 x 60 for the statuses, well inside the timeout.
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
    await openLive(page, listFor(id), 'monitor:all');
    await openLive(monitorPage, `/monitors/${monitor.id}`, `monitor:${monitor.id}`);

    await expect(cardStatusOf(page, name)).toHaveText('Maintenance', nextCheck);
    await expect(pageStatusOf(monitorPage)).toHaveText('Maintenance', nextCheck);
  } finally {
    for (const path of created.reverse()) await deleteCreated(api, path);
    await api.dispose();
  }
});

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
