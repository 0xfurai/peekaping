import { test, expect, request, type Page } from '@playwright/test';
import { apiBaseURL } from './support/admin';
import { adminAPI, createHTTPMonitor, randomID } from './support/api';
import { targetHost, targetPort, targetURL } from './support/fake-target';
import { listFor } from './support/monitor-list';
import { cleanUp, createProbe } from './support/probe';

// The names are lowercase: on Postgres the monitor list search matches only a lowercase term
// (PP-72), and cleanUp and listFor search for them.
const names = () => {
  const id = randomID();
  return { probe: `e2e probe ${id}`, monitor: `e2e-probe-monitor-${id}` };
};

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

test('A probe is picked with the keyboard alone', async ({ page }) => {
  const { probe, monitor } = names();

  await page.goto('/monitors');
  try {
    const api = await adminAPI(page);
    await createProbe(api, probe);
    await api.dispose();

    await page.goto('/monitors/new');
    await page.locator('input[name="name"]').fill(monitor);
    await page.locator('input[name="url"]').fill(targetURL('monitor-probes'));

    // Enter on the focused select opens the list, with the search field focused. The search keeps
    // the selected Local probe in the list, so the probe is the second option.
    await probeSelect(page).focus();
    await page.keyboard.press('Enter');
    const search = page.getByPlaceholder('Search...');
    await expect(search).toBeFocused();
    await search.fill(probe);
    await page.keyboard.press('ArrowDown');
    await page.keyboard.press('ArrowDown');
    const option = page.getByRole('option', { name: probe, exact: true });
    await expect(search).toHaveAttribute('aria-activedescendant', (await option.getAttribute('id')) ?? 'no id');
    await page.keyboard.press('Enter');
    await expect(option).toHaveAttribute('aria-selected', 'true');

    // A space picks too, but only while the search is empty: before that it is a character of
    // the search. The probe stays the active option when the search clears.
    await search.fill('');
    await page.keyboard.press('Space');
    await expect(option).toHaveAttribute('aria-selected', 'false');
    await page.keyboard.press('Enter');
    await expect(option).toHaveAttribute('aria-selected', 'true');
    await page.keyboard.press('Escape');
    await expect(probeSelect(page).getByText(probe, { exact: true })).toBeVisible();

    await submit(page);
    await expect(page).toHaveURL(monitorPageURL, { timeout: 10000 });
    const locations = page.getByTestId('monitor-location');
    await expect(locations).toHaveCount(2);
    await expect(locations.nth(1).getByTestId('monitor-location-name')).toHaveText(probe);
  } finally {
    await cleanUp(page, { probe, monitor });
  }
});

const ruleSelect = (page: Page) => page.getByTestId('monitor-location-rule');
const thresholdInput = (page: Page) => page.getByTestId('monitor-location-threshold');
const tooHigh = "Can't be more than the number of probes you chose";

test("A monitor's location rule is chosen once it has two probes, and saved with its number", async ({ page }) => {
  const { probe, monitor } = names();
  const second = `${probe} two`;

  await page.goto('/monitors');
  try {
    const api = await adminAPI(page);
    await createProbe(api, probe);
    await createProbe(api, second);
    const { id } = await createHTTPMonitor(api, monitor, targetURL('monitor-probes'));
    await api.dispose();

    await page.goto(`/monitors/${id}/edit`);
    // One probe is one location: there is no rule to choose.
    await expect(probeSelect(page)).toHaveText('Local');
    await expect(ruleSelect(page)).toHaveCount(0);

    await chooseProbes(page, ['Local', probe, second]);
    await expect(ruleSelect(page)).toHaveText('Any location is Down');
    await expect(thresholdInput(page)).toHaveCount(0);

    await ruleSelect(page).click();
    await page.getByRole('option', { name: 'At least N locations are Down', exact: true }).click();
    await expect(ruleSelect(page)).toHaveText('At least N locations are Down');
    // The number starts at 2 and can't be more than the three probes chosen.
    await expect(thresholdInput(page)).toHaveValue('2');
    await thresholdInput(page).fill('4');
    await page.getByRole('button', { name: 'Update' }).click();
    await expect(page.getByText(tooHigh)).toBeVisible();
    await expect(page).toHaveURL(/\/edit$/);
    await thresholdInput(page).fill('2');
    await expect(page.getByText(tooHigh)).toHaveCount(0);
    await page.getByRole('button', { name: 'Update' }).click();
    await page.waitForURL(monitorPageURL, { timeout: 10000 });

    const check = await adminAPI(page);
    const saved = (await (await check.get(`monitors/${id}`)).json()).data;
    await check.dispose();
    expect(saved.probe_ids).toHaveLength(3);
    expect(saved.location_rule).toBe('at_least');
    expect(saved.location_threshold).toBe(2);

    // The saved rule and number come back in the form.
    await page.goto(`/monitors/${id}/edit`);
    await expect(ruleSelect(page)).toHaveText('At least N locations are Down');
    await expect(thresholdInput(page)).toHaveValue('2');

    // Dropping a probe under the number is flagged; the number can come down with it.
    await thresholdInput(page).fill('3');
    await chooseProbes(page, ['Local', probe]);
    await expect(page.getByText(tooHigh)).toBeVisible();
    await thresholdInput(page).fill('2');
    await expect(page.getByText(tooHigh)).toHaveCount(0);

    // Back to one probe, the rule goes back to any: a number over the one location can't be saved.
    await chooseProbes(page, ['Local']);
    await expect(ruleSelect(page)).toHaveCount(0);
    await page.getByRole('button', { name: 'Update' }).click();
    await page.waitForURL(monitorPageURL, { timeout: 10000 });
    const after = await adminAPI(page);
    const reset = (await (await after.get(`monitors/${id}`)).json()).data;
    await after.dispose();
    expect(reset.location_rule).toBe('any');
    expect(reset.location_threshold).toBeNull();
  } finally {
    await cleanUp(page, { probe: [probe, second], monitor });
  }
});
