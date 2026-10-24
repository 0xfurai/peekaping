import { test, expect, request, type APIRequestContext, type Page } from '@playwright/test';
import { apiBaseURL } from './support/admin';
import { adminAPI, deleteCreated, randomID } from './support/api';
import { targetHost, targetPort, targetURL } from './support/fake-target';
import { listFor } from './support/monitor-list';

// A MongoDB install has no probes, not even the local one (ADR 0001), so there is nothing to
// choose. The workflow runs every spec on Mongo and no variable names the database, so the empty
// list is the signal to skip.
test.beforeEach(async ({ page }) => {
  const api = await adminAPI(page);
  const response = await api.get('probes');
  expect(response.ok(), `GET probes: ${await response.text()}`).toBeTruthy();
  const { data: probes } = await response.json();
  await api.dispose();
  test.skip(probes.length === 0, 'probes need SQLite or PostgreSQL storage (ADR 0001)');
});

// The names are lowercase: on Postgres the monitor list search matches only a lowercase term
// (PP-72), and cleanUp and listFor search for them.
const names = () => {
  const id = randomID();
  return { probe: `e2e probe ${id}`, monitor: `e2e-probe-monitor-${id}` };
};

async function createProbe(api: APIRequestContext, name: string): Promise<{ id: string; token: string }> {
  const response = await api.post('probes', { data: { name } });
  expect(response.status(), await response.text()).toBe(201);
  return (await response.json()).data;
}

// What a probe's first call tells the server: the monitor types it can run. A probe that has
// never connected lists none, which means it runs every type.
async function sayHello(page: Page, token: string, supportedTypes: string[]) {
  const instance = `e2e-${randomID()}`;
  const probeAPI = await request.newContext({
    baseURL: `${(await apiBaseURL(page)).replace(/\/api\/v1$/, '/probe/v1')}/`,
    extraHTTPHeaders: { Authorization: `Bearer ${token}`, 'X-Probe-Instance': instance },
  });
  const response = await probeAPI.post('hello', {
    data: { instance_id: instance, protocol_version: 1, hostname: 'e2e', supported_types: supportedTypes },
  });
  expect(response.ok(), `POST hello: ${await response.text()}`).toBeTruthy();
  await probeAPI.dispose();
}

// Deletes what a spec made, whatever state it was left in, so reruns start clean. The monitors go
// first: a probe that has monitors can't be deleted.
async function cleanUp(page: Page, { monitor, probe }: { monitor: string; probe: string }) {
  const api = await adminAPI(page);
  const monitors = await (await api.get('monitors', { params: { q: monitor, limit: 100 } })).json();
  for (const found of monitors.data.filter((m: { name: string }) => m.name === monitor)) {
    await deleteCreated(api, `monitors/${found.id}`);
  }
  const probes = await (await api.get('probes')).json();
  for (const found of probes.data.filter((p: { name: string }) => p.name === probe)) {
    await deleteCreated(api, `probes/${found.id}`);
  }
  await api.dispose();
}

const probeSelect = (page: Page) => page.getByTestId('monitor-probe-select');

async function chooseProbe(page: Page, name: string) {
  const select = probeSelect(page);
  await select.click();
  await page.getByRole('option', { name, exact: true }).click();
  await expect(select).toHaveText(name);
}

async function chooseType(page: Page, option: string) {
  const type = page.getByRole('combobox', { name: 'Type', exact: true });
  await type.click();
  await page.getByRole('option', { name: option, exact: true }).click();
  await expect(type).toHaveText(option);
}

const submit = (page: Page) => page.getByRole('button', { name: 'Create', exact: true }).click();

// The monitor's page, not /monitors/new: a rejected form stays there.
const monitorPageURL = /\/monitors\/(?!new$)[A-Za-z0-9-]+$/;

const cardOf = (page: Page, monitor: string) => page.getByTestId('monitor-card').filter({ hasText: monitor });

test('A monitor is put on a remote probe, then moved back to Local', async ({ page }) => {
  const { probe, monitor } = names();

  await page.goto('/monitors');
  try {
    const api = await adminAPI(page);
    await createProbe(api, probe);
    await api.dispose();

    await page.goto('/monitors/new');
    await page.locator('input[name="name"]').fill(monitor);
    await page.locator('input[name="url"]').fill(targetURL('monitor-probes'));
    // A new monitor starts on Local, the probe every install has.
    await expect(probeSelect(page)).toHaveText('Local');
    await chooseProbe(page, probe);
    await submit(page);

    await expect(page).toHaveURL(monitorPageURL, { timeout: 10000 });
    await expect(page.getByTestId('monitor-probe')).toHaveText(`Probe: ${probe}`);

    await page.goto(listFor(monitor));
    await expect(cardOf(page, monitor).getByTestId('monitor-probe-badge')).toHaveText(probe);

    await cardOf(page, monitor).click();
    await page.waitForURL(monitorPageURL);
    await page.getByRole('button', { name: /edit/i }).click();
    await page.waitForURL(/\/monitors\/[A-Za-z0-9-]+\/edit$/);
    await expect(probeSelect(page)).toHaveText(probe);
    await chooseProbe(page, 'Local');
    await page.getByRole('button', { name: 'Update' }).click();

    await page.waitForURL(monitorPageURL, { timeout: 10000 });
    await expect(page.getByTestId('monitor-probe')).toHaveText('Probe: Local');
    await page.goto(listFor(monitor));
    await expect(cardOf(page, monitor).getByTestId('monitor-probe-badge')).toHaveText('Local');
  } finally {
    await cleanUp(page, { probe, monitor });
  }
});

test('A push monitor is fixed to Local', async ({ page }) => {
  const { probe, monitor } = names();

  await page.goto('/monitors');
  try {
    const api = await adminAPI(page);
    await createProbe(api, probe);
    await api.dispose();

    await page.goto('/monitors/new');
    await page.locator('input[name="name"]').fill(monitor);
    await page.locator('input[name="url"]').fill(targetURL('monitor-probes'));
    await chooseProbe(page, probe);

    // The push monitor waits for pushes at this server, so it can't run anywhere else, and the
    // probe chosen before the switch doesn't carry over.
    await chooseType(page, 'Push Monitor (external service calls a generated URL)');
    await expect(probeSelect(page)).toBeDisabled();
    await expect(probeSelect(page)).toHaveText('Local');
    await submit(page);

    await expect(page).toHaveURL(monitorPageURL, { timeout: 10000 });
    await expect(page.getByTestId('monitor-probe')).toHaveText('Probe: Local');
  } finally {
    await cleanUp(page, { probe, monitor });
  }
});

test("A probe that doesn't run the monitor's type can't be picked", async ({ page }) => {
  const { probe, monitor } = names();

  await page.goto('/monitors');
  try {
    const api = await adminAPI(page);
    const { token } = await createProbe(api, probe);
    await api.dispose();
    await sayHello(page, token, ['http']);

    await page.goto('/monitors/new');
    await page.locator('input[name="name"]').fill(monitor);
    await page.locator('input[name="url"]').fill(targetURL('monitor-probes'));
    await chooseProbe(page, probe);

    // The type changes under the probe: the form keeps it and flags it instead of moving the
    // monitor to another probe.
    await chooseType(page, 'TCP Port Monitor');
    await page.locator('[name="host"]').fill(targetHost);
    await page.locator('[name="port"]').fill(String(targetPort));
    const unsupported = `${probe} (doesn't run this type)`;
    await expect(probeSelect(page)).toHaveText(unsupported);
    await expect(page.getByText("This probe doesn't run this type of monitor")).toBeVisible();
    await submit(page);
    await expect(page).toHaveURL(/\/monitors\/new$/);

    // It stays in the list, but can't be picked for this type.
    await probeSelect(page).click();
    await expect(page.getByRole('option', { name: unsupported, exact: true })).toBeDisabled();
    await page.getByRole('option', { name: 'Local', exact: true }).click();
    await expect(page.getByText("This probe doesn't run this type of monitor")).toHaveCount(0);
    await submit(page);

    await expect(page).toHaveURL(monitorPageURL, { timeout: 10000 });
    await expect(page.getByTestId('monitor-probe')).toHaveText('Probe: Local');
  } finally {
    await cleanUp(page, { probe, monitor });
  }
});
