import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
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

// The probe's id by name, once the UI has created it.
async function probeIDOf(api: APIRequestContext, name: string): Promise<string> {
  const probes: { id: string; name: string }[] = (await (await api.get('probes')).json()).data;
  const probe = probes.find((p) => p.name === name);
  expect(probe, `probe ${name} exists`).toBeDefined();
  return probe!.id;
}

// The probes a monitor runs on, as the API has them.
async function probeIDsOf(api: APIRequestContext, monitorID: string): Promise<string[]> {
  const response = await api.get(`monitors/${monitorID}`);
  expect(response.ok(), await response.text()).toBeTruthy();
  return (await response.json()).data.probe_ids;
}

test('A probe created with Assign to all monitors is added to an existing monitor when it connects', async ({ page }) => {
  test.setTimeout(180_000);
  const n = names();

  await page.goto('/monitors');
  await withTearDown(page, n, async () => {
    const api = await adminAPI(page);
    try {
      // The monitor runs on Local only, and was there before the probe.
      const { id } = await createHTTPMonitor(api, n.monitor, targetURL('assign-all-create'));
      expect(await probeIDsOf(api, id)).toEqual(['local']);

      await page.goto('/probes');
      await page.getByTestId('create-entity').click();
      const dialog = page.getByRole('dialog', { name: 'Create probe' });
      // The box is checked by default, and warns what the token reads.
      const assignAll = dialog.getByRole('checkbox', { name: 'Assign to all monitors' });
      await expect(assignAll).toBeChecked();
      await expect(dialog.getByText('The token reads the config of every monitor assigned to the probe')).toBeVisible();
      const docs = dialog.getByRole('link', { name: 'What the token can read' });
      await expect(docs).toHaveAttribute(
        'href',
        'https://docs.peekaping.com/self-hosting/remote-probes#what-the-token-can-read',
      );
      await expect(docs).toHaveAttribute('target', '_blank');

      await dialog.locator('input[name="name"]').fill(n.probe);
      await dialog.getByRole('button', { name: 'Create probe' }).click();
      const alert = page.getByRole('alert').filter({ hasText: n.probe });
      const token = (await alert.getByTestId('probe-token').textContent())!;

      // Nothing is assigned until the first instance says hello: only then does the server know
      // which monitor types the probe runs. The probe's page says it waits.
      const probeID = await probeIDOf(api, n.probe);
      expect(await probeIDsOf(api, id)).toEqual(['local']);
      await page.goto(`/probes/${probeID}`);
      await expect(page.getByTestId('probe-assign-all-pending')).toContainText('Waiting for its first instance');

      startProbeContainer(n.container, token);

      await expect
        .poll(() => probeIDsOf(api, id), { timeout: 90_000, intervals: [2000] })
        .toEqual(['local', probeID]);

      await page.goto(`/monitors/${id}`);
      await expect(page.getByTestId('monitor-location')).toHaveCount(2);
      await expect(remoteLocation(page).getByTestId('monitor-location-name')).toHaveText(n.probe);
      await expectRemoteUp(page, n.probe, n.container);

      // The wait is over once the monitors are assigned.
      await page.goto(`/probes/${probeID}`);
      await expect(page.getByTestId('probe-assign-all-pending')).toHaveCount(0);
    } finally {
      await api.dispose();
    }
  });
});

test('Assign to all monitors on the Probes page adds a probe to the monitors it can run', async ({ page }) => {
  test.setTimeout(180_000);
  const n = names();

  await page.goto('/monitors');
  await withTearDown(page, n, async () => {
    const api = await adminAPI(page);
    try {
      // A probe that no instance has connected to yet.
      const { id: probeID, token } = await createProbe(api, n.probe);
      await page.goto('/probes');
      const row = page.getByRole('row', { name: new RegExp(n.probe) });
      const assignAll = async () => {
        await row.getByRole('button', { name: `Assign probe ${n.probe} to all monitors` }).click();
        const dialog = page.getByRole('dialog', { name: 'Assign to all monitors' });
        await expect(dialog.getByText('The token reads the config of every monitor assigned to the probe')).toBeVisible();
        await expect(dialog.getByRole('link', { name: 'What the token can read' })).toHaveAttribute(
          'href',
          'https://docs.peekaping.com/self-hosting/remote-probes#what-the-token-can-read',
        );
        await dialog.getByRole('button', { name: 'Assign to all monitors' }).click();
        await expect(dialog).toHaveCount(0);
      };

      // The local probe is Peekaping itself: it has no such action.
      await expect(page.getByTestId('probe-row-local').getByRole('button', { name: /Assign probe/ })).toHaveCount(0);

      // With no instance on record, the action only notes the request.
      await assignAll();
      await expect(page.getByText('It will be added to every monitor it runs when it connects')).toBeVisible();
      await page.goto(`/probes/${probeID}`);
      await expect(page.getByTestId('probe-assign-all-pending')).toBeVisible();

      // The first hello makes the assignment, and the probe stops waiting.
      startProbeContainer(n.container, token);
      await expect
        .poll(async () => (await (await api.get(`probes/${probeID}`)).json()).data, { timeout: 90_000, intervals: [2000] })
        .toMatchObject({ assign_all_pending: false });

      // A monitor made after the probe connected is not on the probe until the action runs.
      const { id } = await createHTTPMonitor(api, n.monitor, targetURL('assign-all-action'));
      expect(await probeIDsOf(api, id)).toEqual(['local']);

      await page.goto('/probes');
      await assignAll();
      // Other monitors of the install may be added too, so the count isn't asserted.
      await expect(page.getByText(/Monitors added: [1-9]\d*/)).toBeVisible();
      expect(await probeIDsOf(api, id)).toEqual(['local', probeID]);

      await page.goto(`/monitors/${id}`);
      await expect(remoteLocation(page).getByTestId('monitor-location-name')).toHaveText(n.probe);
      await expectRemoteUp(page, n.probe, n.container);
    } finally {
      await api.dispose();
    }
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
