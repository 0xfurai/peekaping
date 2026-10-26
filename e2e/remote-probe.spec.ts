import { test, expect, type Page } from '@playwright/test';
import { adminAPI, createHTTPMonitor, createWebhookChannel, deleteCreated, randomID } from './support/api';
import { receivedWebhooks, targetURL, webhookURL } from './support/fake-target';
import { cardStatusOf, listFor } from './support/monitor-list';
import {
  createProbe,
  probeContainerLogs,
  startProbeContainer,
  stopProbeContainer,
  withTearDown,
} from './support/probe';

// These specs run a real probe: a peekaping-probe container, started with docker on the machine
// that runs the suite and joined to the stack's network (e2e/support/probe.ts). The stack sets
// PROBE_OFFLINE_AFTER=2m, the shortest the server takes, which the offline spec waits out.

// The names are lowercase: on Postgres the monitor list search matches only a lowercase term
// (PP-72), and cleanUp and listFor search for them.
const names = () => {
  const id = randomID();
  return { probe: `e2e probe ${id}`, monitor: `e2e-remote-probe-${id}`, container: `e2e-probe-${id}` };
};

// A monitor on Local and on a remote probe, whose probe runs as a container. Local is there for
// the monitor to have two locations: a monitor on one probe shows no locations (its page has a
// single heartbeat bar).
async function startMonitorOnProbe(
  page: Page,
  { probe, monitor, container }: ReturnType<typeof names>,
  { notificationIds }: { notificationIds?: string[] } = {},
) {
  const api = await adminAPI(page);
  try {
    const { id: probeID, token } = await createProbe(api, probe);
    startProbeContainer(container, token);
    const { id } = await createHTTPMonitor(api, monitor, targetURL('remote-probe'), {
      probeIds: ['local', probeID],
      notificationIds,
    });
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
  await withTearDown(page, n, async () => {
    const id = await startMonitorOnProbe(page, n);

    await page.goto(`/monitors/${id}`);
    await expect(page.getByTestId('monitor-location')).toHaveCount(2);
    await expect(remoteLocation(page).getByTestId('monitor-location-name')).toHaveText(n.probe);

    // Only the probe's own check can make its row Up, so the heartbeat came from the container.
    await expectRemoteUp(page, n.probe, n.container);
    await expect(remoteLocation(page).getByTestId('monitor-location-ping')).toHaveText(/Response time: \d+ ms/);
    await expect(remoteLocation(page).getByTestId('monitor-location-offline')).toHaveCount(0);
  });
});

test('A stopped remote probe is reported offline and its monitor stays up', async ({ page, request }) => {
  // The probe says goodbye when it stops, which leaves it last seen a minute ago. It is offline
  // once PROBE_OFFLINE_AFTER (2m) has passed, at the next sweep, which runs every 30 seconds.
  test.setTimeout(360_000);
  const n = names();

  await page.goto('/monitors');
  // The probe has no channels of its own, so its offline notification goes to the channels of its
  // monitors: this monitor's channel.
  const api = await adminAPI(page);
  try {
    const channel = await createWebhookChannel(api, `e2e channel ${n.monitor}`, webhookURL(n.monitor));
    try {
      await withTearDown(page, n, async () => {
        const id = await startMonitorOnProbe(page, n, { notificationIds: [channel.id] });

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

        // The monitor's channel got one "offline" notification for the probe. The monitor's own
        // Up notification may be there too, so the requests are filtered, not counted.
        await expect
          .poll(
            async () =>
              (await receivedWebhooks(request, n.monitor)).filter((webhook) =>
                webhook.rawBody.includes(`Probe ${n.probe} has been offline since`),
              ).length,
            { timeout: 60_000, intervals: [2000] },
          )
          .toBe(1);
      });
    } finally {
      await deleteCreated(api, `notification-channels/${channel.id}`);
    }
  } finally {
    await api.dispose();
  }
});
