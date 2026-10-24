import { test, expect, type Page } from '@playwright/test';
import { adminAPI, createHTTPMonitor, randomID } from './support/api';
import { targetURL } from './support/fake-target';
import { cardStatusOf, listFor } from './support/monitor-list';
import {
  cleanUp,
  createProbe,
  probeContainerLogs,
  removeProbeContainer,
  startProbeContainer,
  stopProbeContainer,
} from './support/probe';

// These specs run a real probe: a peekaping-probe container, started with docker on the machine
// that runs the suite and joined to the stack's network (e2e/support/probe.ts). The stack sets
// PROBE_OFFLINE_AFTER=2m, the shortest the server takes, which the offline spec waits out.

// A MongoDB install has no probes, not even the local one (ADR 0001), so there is nothing to
// run. The workflow runs every spec on Mongo and no variable names the database, so the empty
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
  return { probe: `e2e probe ${id}`, monitor: `e2e-remote-probe-${id}`, container: `e2e-probe-${id}` };
};

// A monitor on Local and on a remote probe, whose probe runs as a container. Local is there for
// the monitor to have two locations: a monitor on one probe shows no locations (its page has a
// single heartbeat bar).
async function startMonitorOnProbe(page: Page, { probe, monitor, container }: ReturnType<typeof names>) {
  const api = await adminAPI(page);
  try {
    const { id: probeID, token } = await createProbe(api, probe);
    startProbeContainer(container, token);
    const { id } = await createHTTPMonitor(api, monitor, targetURL('remote-probe'), { probeIds: ['local', probeID] });
    return id;
  } finally {
    await api.dispose();
  }
}

// The probe's row on the monitor's page: the second location, after Local.
const remoteLocation = (page: Page) => page.getByTestId('monitor-location').nth(1);

// A location row that has not shown a result in time says why the probe stayed quiet.
async function expectRemoteUp(page: Page, probe: string, container: string) {
  try {
    await expect(remoteLocation(page).getByTestId('monitor-location-status')).toHaveText('Up', { timeout: 90_000 });
  } catch (error) {
    throw new Error(`${probe} never showed Up. Its container said:\n${probeContainerLogs(container)}`, { cause: error });
  }
}

test('A monitor on a running remote probe shows the probe up', async ({ page }) => {
  test.setTimeout(150_000);
  const n = names();

  await page.goto('/monitors');
  try {
    const id = await startMonitorOnProbe(page, n);

    await page.goto(`/monitors/${id}`);
    await expect(page.getByTestId('monitor-location')).toHaveCount(2);
    await expect(remoteLocation(page).getByTestId('monitor-location-name')).toHaveText(n.probe);

    // Only the probe's own check can make its row Up, so the heartbeat came from the container.
    await expectRemoteUp(page, n.probe, n.container);
    await expect(remoteLocation(page).getByTestId('monitor-location-ping')).toHaveText(/Response time: \d+ ms/);
    await expect(remoteLocation(page).getByTestId('monitor-location-offline')).toHaveCount(0);
  } finally {
    removeProbeContainer(n.container);
    await cleanUp(page, n);
  }
});

test('A stopped remote probe is reported offline and its monitor stays up', async ({ page }) => {
  // The probe says goodbye when it stops, which leaves it last seen a minute ago. It is offline
  // once PROBE_OFFLINE_AFTER (2m) has passed, at the next sweep, which runs every 30 seconds.
  test.setTimeout(360_000);
  const n = names();

  await page.goto('/monitors');
  try {
    const id = await startMonitorOnProbe(page, n);

    await page.goto(`/monitors/${id}`);
    await expectRemoteUp(page, n.probe, n.container);
    await expect(remoteLocation(page).getByTestId('monitor-location-offline')).toHaveCount(0);

    stopProbeContainer(n.container);

    // The page learns that a probe went offline when it loads the monitor, so it is reloaded.
    await expect(async () => {
      await page.reload();
      await expect(remoteLocation(page).getByTestId('monitor-location-offline')).toHaveText('Probe offline', {
        timeout: 5000,
      });
    }).toPass({ timeout: 240_000, intervals: [10_000] });

    // The offline probe doesn't count toward the monitor's status, and Local still checks it.
    await page.goto(listFor(n.monitor));
    await expect(cardStatusOf(page, n.monitor)).toHaveText('Up');
  } finally {
    removeProbeContainer(n.container);
    await cleanUp(page, n);
  }
});
