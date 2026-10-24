import { test, expect, request, type APIRequestContext, type Page } from '@playwright/test';
import { apiBaseURL } from './support/admin';
import { adminAPI, cleanupRetry, createHTTPMonitor, deleteCreated, randomID } from './support/api';
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
// first: a probe that has monitors can't be deleted (it answers 409). A form that was submitted
// when the spec failed can still be saving its monitor, so a refused probe delete lists the
// monitors again and tries once more, instead of failing the spec with a message that hides why
// it failed in the first place.
async function cleanUp(page: Page, { monitor, probe }: { monitor: string; probe: string }) {
  const api = await adminAPI(page);
  try {
    await expect(async () => {
      const monitors = await api.get('monitors', { params: { q: monitor, limit: 100 } });
      expect(monitors.ok(), `GET monitors: ${await monitors.text()}`).toBeTruthy();
      for (const found of (await monitors.json()).data.filter((m: { name: string }) => m.name === monitor)) {
        await deleteCreated(api, `monitors/${found.id}`);
      }
      const probes = await (await api.get('probes')).json();
      for (const found of probes.data.filter((p: { name: string }) => p.name === probe)) {
        const response = await api.delete(`probes/${found.id}`);
        expect(response.ok(), `DELETE probes/${found.id}: ${response.status()} ${await response.text()}`).toBeTruthy();
      }
    }).toPass(cleanupRetry);
  } finally {
    await api.dispose();
  }
}

const probeSelect = (page: Page) => page.getByTestId('monitor-probe-select');

// The probe select is a multi-select: this picks exactly the probes named, toggling the ones that
// differ from what is selected.
async function chooseProbes(page: Page, names: string[]) {
  await probeSelect(page).click();
  const options = page.getByRole('option');
  for (let i = 0; i < (await options.count()); i++) {
    const option = options.nth(i);
    const label = (await option.textContent())?.trim() ?? '';
    const selected = (await option.getAttribute('aria-selected')) === 'true';
    if (names.includes(label) !== selected) await option.click();
  }
  await page.keyboard.press('Escape');
  for (const name of names) {
    await expect(probeSelect(page).getByText(name, { exact: true })).toBeVisible();
  }
}

const chooseProbe = (page: Page, name: string) => chooseProbes(page, [name]);

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

    // It can be unselected, and then it can't be picked again for this type.
    await probeSelect(page).click();
    await page.getByRole('option', { name: unsupported, exact: true }).click();
    await expect(page.getByRole('option', { name: unsupported, exact: true })).toBeDisabled();
    await page.getByRole('option', { name: 'Local', exact: true }).click();
    await page.keyboard.press('Escape');
    await expect(page.getByText("This probe doesn't run this type of monitor")).toHaveCount(0);
    await submit(page);

    await expect(page).toHaveURL(monitorPageURL, { timeout: 10000 });
    await expect(page.getByTestId('monitor-probe')).toHaveText('Probe: Local');
  } finally {
    await cleanUp(page, { probe, monitor });
  }
});

test('A monitor on Local and a remote probe shows each location on its page and its card', async ({ page }) => {
  const { probe, monitor } = names();

  await page.goto('/monitors');
  try {
    const api = await adminAPI(page);
    const { id: probeID } = await createProbe(api, probe);
    // The probe has no instance, so it is offline and never reports.
    const { id } = await createHTTPMonitor(api, monitor, targetURL('monitor-locations'), {
      probeIds: ['local', probeID],
    });
    await api.dispose();

    await page.goto(listFor(monitor));
    await expect(cardOf(page, monitor).getByTestId('monitor-probe-badge')).toHaveText(['Local', probe]);

    await page.goto(`/monitors/${id}`);
    // The locations replace the single probe line and the single heartbeat bar.
    await expect(page.getByTestId('monitor-probe')).toHaveCount(0);
    const locations = page.getByTestId('monitor-location');
    await expect(locations).toHaveCount(2);
    await expect(locations.nth(0).getByTestId('monitor-location-name')).toHaveText('Local');
    await expect(locations.nth(1).getByTestId('monitor-location-name')).toHaveText(probe);

    // The remote probe has no instance: it is offline and has not reported.
    await expect(locations.nth(1).getByTestId('monitor-location-offline')).toHaveText('Probe offline');
    await expect(locations.nth(1).getByTestId('monitor-location-status')).toHaveText('No result yet');
    await expect(locations.nth(0).getByTestId('monitor-location-offline')).toHaveCount(0);

    // Local checks the monitor, and its row follows with a status and a response time.
    await expect(locations.nth(0).getByTestId('monitor-location-status')).toHaveText('Up', { timeout: 45000 });
    await expect(locations.nth(0).getByTestId('monitor-location-ping')).toHaveText(/Response time: \d+ ms/);

    // The chart can be narrowed to one location.
    const filtered = page.waitForRequest(
      (request) =>
        request.url().includes(`/monitors/${id}/stats/points`) && request.url().includes(`probe_id=${probeID}`),
    );
    await page.getByTestId('chart-location-select').click();
    await page.getByRole('option', { name: probe, exact: true }).click();
    await filtered;
    await expect(page.getByTestId('chart-location-select')).toHaveText(probe);
  } finally {
    await cleanUp(page, { probe, monitor });
  }
});

test('Two probes picked in the form are both saved', async ({ page }) => {
  const { probe, monitor } = names();

  await page.goto('/monitors');
  try {
    const api = await adminAPI(page);
    await createProbe(api, probe);
    await api.dispose();

    await page.goto('/monitors/new');
    await page.locator('input[name="name"]').fill(monitor);
    await page.locator('input[name="url"]').fill(targetURL('monitor-probes'));

    // A monitor needs a probe to run on.
    await chooseProbes(page, []);
    await submit(page);
    await expect(page.getByText('Choose at least one probe')).toBeVisible();
    await expect(page).toHaveURL(/\/monitors\/new$/);

    await chooseProbes(page, ['Local', probe]);
    await expect(page.getByText('Choose at least one probe')).toHaveCount(0);
    await submit(page);

    await expect(page).toHaveURL(monitorPageURL, { timeout: 10000 });
    const locations = page.getByTestId('monitor-location');
    await expect(locations).toHaveCount(2);
    await expect(locations.nth(0).getByTestId('monitor-location-name')).toHaveText('Local');
    await expect(locations.nth(1).getByTestId('monitor-location-name')).toHaveText(probe);

    await page.goto(listFor(monitor));
    await expect(cardOf(page, monitor).getByTestId('monitor-probe-badge')).toHaveText(['Local', probe]);
  } finally {
    await cleanUp(page, { probe, monitor });
  }
});
