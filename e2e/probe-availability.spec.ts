import { test, expect, type Page } from '@playwright/test';
import { adminAPI, randomID } from './support/api';
import {
  createProbe,
  probeContainerLogs,
  removeProbeContainer,
  startProbeContainer,
  stopProbeContainer,
  withTearDown,
} from './support/probe';

// A probe's availability: the bar and uptime on the Probes page, and the probe's page with its
// instances. This spec runs a real probe container, so that a probe can go offline and come back
// (see remote-probe.spec.ts). The stack sets PROBE_OFFLINE_AFTER=2m, which the spec waits out.

const names = () => {
  const id = randomID();
  // Lowercase, like the names in remote-probe.spec.ts. There is no monitor: cleanUp finds none.
  return { probe: `e2e probe availability ${id}`, monitor: `e2e-probe-availability-${id}`, container: `e2e-probe-${id}` };
};

const UPTIME = /^\d+\.\d%$/;

const percentOf = async (text: string | null) => {
  expect(text).toMatch(UPTIME);
  return parseFloat(text!);
};

// The probe's row says online once its container has connected. A probe that never did says why.
async function expectOnline(page: Page, probeID: string, probe: string, container: string) {
  try {
    await expect(page.getByTestId(`probe-row-${probeID}`).getByText('Online', { exact: true })).toBeVisible({
      timeout: 90_000,
    });
  } catch (error) {
    throw new Error(`${probe} never showed Online. Its container said:\n${probeContainerLogs(container)}`, { cause: error });
  }
}

test('A probe that went offline and came back shows the gap and two instances', async ({ page }) => {
  // About 30s to start the probe, 2m30s to 3m30s for it to be reported offline (PROBE_OFFLINE_AFTER
  // is 2m and the sweep runs every 30s, and the list refreshes every 10s), 30s to come back, and
  // the checks. Expect the spec to take about 5 minutes.
  test.setTimeout(480_000);
  const n = names();

  await page.goto('/probes');
  await withTearDown(page, n, async () => {
    const api = await adminAPI(page);
    const created = await createProbe(api, n.probe).finally(() => api.dispose());
    const probeID = created.id;
    startProbeContainer(n.container, created.token);

    await page.goto('/probes');
    await expectOnline(page, probeID, n.probe, n.container);

    // The row has the 24 hour bar and the uptime, from the one list request. A probe's history
    // starts when it is created, offline, so it is not 100% yet: the uptime is a percentage.
    const row = page.getByTestId(`probe-row-${probeID}`);
    await expect(row.getByTestId('availability-bar')).toBeVisible();
    await expect(row.getByTestId('probe-uptime')).toHaveText(UPTIME);
    await expect(row.getByTestId('probe-link')).toHaveText(n.probe);

    stopProbeContainer(n.container);
    await expect(row.getByText('Offline', { exact: true })).toBeVisible({ timeout: 240_000 });

    // A new container is a new instance of the same probe. It takes the old one's name, which the
    // teardown removes.
    removeProbeContainer(n.container);
    startProbeContainer(n.container, created.token);
    await expect(row.getByText('Online', { exact: true })).toBeVisible({ timeout: 90_000 });

    // The availability refreshes every 30s, so a reload gets it at once.
    await page.reload();
    await expect(row.getByTestId('probe-uptime')).toHaveText(UPTIME);
    expect(await percentOf(await row.getByTestId('probe-uptime').textContent())).toBeLessThan(100);

    await row.getByTestId('probe-link').click();
    await expect(page).toHaveURL(new RegExp(`/probes/${probeID}$`));

    // Offline from creation until the first instance connected, online, offline from when that
    // instance was last seen until the second came, online again.
    await expect(page.getByTestId('availability-span-offline')).toHaveCount(2);
    await expect(page.getByTestId('availability-span-online')).toHaveCount(2);
    expect(await percentOf(await page.getByTestId('probe-uptime').textContent())).toBeLessThan(100);

    await page.getByTestId('availability-span-offline').last().hover();
    await expect(page.getByRole('tooltip')).toContainText('Offline');

    // One bar per instance, each with its connection
    await expect(page.getByTestId('instance-row')).toHaveCount(2);
    for (const instance of await page.getByTestId('instance-row').all()) {
      await expect(instance.getByTestId('instance-connection')).toHaveCount(1);
    }

    // The first instance's connection ended before the second one's began: they are two
    // instances, not one that stayed connected.
    const checkAPI = await adminAPI(page);
    try {
      const response = await checkAPI.get(`probes/${probeID}/availability`);
      expect(response.ok(), await response.text()).toBeTruthy();
      const instances: { connections: { start: string; end: string; open: boolean }[] }[] = (await response.json()).data
        .instances;
      expect(instances).toHaveLength(2);
      const [first, second] = instances.sort((a, b) => Date.parse(a.connections[0].start) - Date.parse(b.connections[0].start));
      expect(first.connections.at(-1)!.open).toBe(false);
      expect(second.connections.at(-1)!.open).toBe(true);
      expect(Date.parse(first.connections.at(-1)!.end)).toBeLessThan(Date.parse(second.connections[0].start));
    } finally {
      await checkAPI.dispose();
    }

    // The range select asks for 7 days and the bar stays
    await page.getByTestId('probe-range').click();
    await page.getByRole('option', { name: 'Last 7 days' }).click();
    await expect(page.getByTestId('availability-bar')).toBeVisible();
  });
});

