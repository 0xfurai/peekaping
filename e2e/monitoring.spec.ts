import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import { adminAPI, createHTTPMonitor, deleteCreated, randomID } from './support/api';
import { receivedWebhooks, setTargetStatus, targetURL, webhookURL } from './support/fake-target';

// Heartbeat statuses as the webhook body carries them.
const DOWN = 0;
const UP = 1;

// A new monitor is checked at once and then every 20 seconds, the minimum
// check interval. If whatever runs the checks misses the create event, its next
// refresh (within about 30 seconds) picks the monitor up. The heartbeat then still has to reach
// the page and the notification channel.
const nextCheck = { timeout: 60_000 };

test('An HTTP monitor goes Down, sends a webhook notification, and comes back Up', async ({ page, request }) => {
  test.setTimeout(240_000);
  const id = randomID();
  const monitorName = `Monitoring ${id}`;
  const channelName = `Webhook ${id}`;
  const created: string[] = [];

  await setTargetStatus(request, id, 200);
  try {
    await createWebhookChannel(page, created, channelName, webhookURL(id));
    await createMonitor(page, created, monitorName, targetURL(id), channelName);

    const currentStatus = page.locator('[data-slot="card"]').filter({ hasText: 'Current status' }).locator('.text-2xl');
    // Start Up, so the channel is attached well before the Down that's notified.
    await expect(currentStatus).toHaveText('Up', nextCheck);

    await setTargetStatus(request, id, 500);
    await expect(currentStatus).toHaveText('Down', nextCheck);
    // The first check's Up notification can arrive before this one, after the
    // recovery's, or never, so notifications are matched by heartbeat time, not order.
    let downAt = 0;
    await expect
      .poll(async () => {
        downAt = (await notifiedTimes(request, id, monitorName, DOWN))[0] ?? 0;
        return downAt;
      }, nextCheck)
      .toBeGreaterThan(0);

    await setTargetStatus(request, id, 200);
    await expect(currentStatus).toHaveText('Up', nextCheck);
    // The recovery's Up is the one from a heartbeat after the Down. The first check's is older.
    await expect
      .poll(async () => (await notifiedTimes(request, id, monitorName, UP)).some((time) => time > downAt), nextCheck)
      .toBe(true);
  } finally {
    const api = await adminAPI(page);
    for (const path of created.reverse()) await deleteCreated(api, path);
    await api.dispose();
  }
});

test('An HTTP monitor sends no Content-Type on a bodyless GET', async ({ page, request }) => {
  test.setTimeout(120_000);
  const id = randomID();
  const monitorName = `No Content-Type ${id}`;
  // This is the server from issue #395: a bodyless GET that carries
  // Content-Type gets 400, so the monitor can only be Up if the header is gone.
  await setTargetStatus(request, id, 200, { rejectContentType: true });

  const api = await adminAPI(page);
  const monitor = await createHTTPMonitor(api, monitorName, targetURL(id));
  try {
    await page.goto(`/monitors/${monitor.id}`);
    const currentStatus = page.locator('[data-slot="card"]').filter({ hasText: 'Current status' }).locator('.text-2xl');
    await expect(currentStatus).toHaveText('Up', nextCheck);
  } finally {
    await deleteCreated(api, `monitors/${monitor.id}`);
    await api.dispose();
  }
});

// The times of the heartbeats behind this monitor's notifications with the given
// status, oldest first, in ms since the epoch.
async function notifiedTimes(request: APIRequestContext, id: string, monitorName: string, status: number) {
  return (await receivedWebhooks(request, id))
    .filter((webhook) => webhook.body.monitor?.name === monitorName && webhook.body.heartbeat.status === status)
    .map((webhook) => {
      const time = Date.parse(webhook.body.heartbeat.time);
      expect(time, `heartbeat time ${webhook.body.heartbeat.time} of a webhook`).not.toBeNaN();
      return time;
    })
    .sort((a, b) => a - b);
}

async function createWebhookChannel(page: Page, created: string[], name: string, url: string) {
  await page.goto('/notification-channels/new');
  await page.getByRole('combobox', { name: 'Notifier Type' }).click();
  await page.getByRole('option', { name: 'Webhook', exact: true }).click();
  await page.locator('input[name="name"]').fill(name);
  await page.locator('input[name="webhook_url"]').fill(url);
  await page.getByRole('combobox', { name: 'Request Body' }).click();
  await page.getByRole('option', { name: 'application/json', exact: true }).click();

  const saved = page.waitForResponse(
    (response) => response.url().endsWith('/notification-channels') && response.request().method() === 'POST',
  );
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  const response = await saved;
  expect(response.status(), await response.text()).toBe(201);
  created.push(`notification-channels/${(await response.json()).data.id}`);
  await expect(page).toHaveURL(/\/notification-channels$/);
}

async function createMonitor(page: Page, created: string[], name: string, url: string, channelName: string) {
  await page.goto('/monitors/new');
  await page.locator('input[name="name"]').fill(name);
  await page.locator('input[name="url"]').fill(url);
  // Check every 20 seconds, the minimum, and go Down on the first failed check.
  await page.locator('input[name="interval"]').fill('20');
  await page.locator('input[name="max_retries"]').fill('0');
  await page.locator('input[name="retry_interval"]').fill('20');
  await page.locator('input[name="timeout"]').fill('16');

  await page.getByRole('combobox').filter({ hasText: 'Select channel' }).click();
  await page.getByRole('option', { name: channelName, exact: true }).click();
  await expect(page.getByRole('button', { name: `Remove ${channelName}` })).toBeVisible();

  const saved = page.waitForResponse(
    (response) => response.url().endsWith('/monitors') && response.request().method() === 'POST',
  );
  await page.getByRole('button', { name: 'Create', exact: true }).click();
  const response = await saved;
  expect(response.status(), await response.text()).toBe(201);
  const { id } = (await response.json()).data;
  created.push(`monitors/${id}`);
  await expect(page).toHaveURL(new RegExp(`/monitors/${id}$`));
}
