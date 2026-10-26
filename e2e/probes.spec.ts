import { test, expect, type Page } from '@playwright/test';
import { adminAPI, createWebhookChannel, deleteCreated, randomID } from './support/api';
import { webhookURL } from './support/fake-target';

// Deletes the probes a spec made, whatever name they ended up with, so reruns
// start clean and a spec that failed halfway leaves nothing behind.
async function deleteProbes(page: Page, names: string[]) {
  const api = await adminAPI(page);
  const { data: probes } = await (await api.get('probes')).json();
  for (const probe of probes.filter((p: { name: string }) => names.includes(p.name))) {
    await deleteCreated(api, `probes/${probe.id}`);
  }
  await api.dispose();
}

test('A probe is created, its token rotated and the probe deleted', async ({ page }) => {
  const probeName = `E2E probe ${randomID()}`;

  await page.goto('/probes');

  // The local probe is Peekaping itself: it can be edited, but not rotated or deleted.
  const local = page.getByTestId('probe-row-local');
  await expect(local).toBeVisible();
  await expect(local.getByRole('button', { name: 'Edit probe' })).toBeVisible();
  await expect(local.getByRole('button', { name: /rotate|delete/i })).toHaveCount(0);

  try {
    await page.getByTestId('create-entity').click();
    const createDialog = page.getByRole('dialog', { name: 'Create probe' });
    // A new probe asks only for a name and a location: with no channels of its own it notifies
    // the channels of its monitors.
    await expect(createDialog.locator('input[name="name"]')).toBeVisible();
    await expect(createDialog.locator('input[name="location"]')).toBeVisible();
    await expect(createDialog.getByText('Notification channels')).toHaveCount(0);
    await expect(createDialog.getByRole('combobox')).toHaveCount(0);
    await createDialog.locator('input[name="name"]').fill(probeName);
    await createDialog.getByRole('button', { name: 'Create probe' }).click();

    // The token shows once, with the docker line that starts a probe with it.
    const alert = page.getByRole('alert').filter({ hasText: probeName });
    const token = (await alert.getByTestId('probe-token').textContent())!;
    expect(token).toMatch(/^pkp_/);
    await expect(alert.getByTestId('probe-docker-command')).toContainText(`PROBE_TOKEN=${token}`);
    await expect(alert.getByTestId('probe-docker-command')).toContainText('0xfurai/peekaping-probe');
    await expect(alert.getByText('shown only once')).toBeVisible();

    const row = page.getByRole('row', { name: new RegExp(probeName) });
    await expect(row).toBeVisible();

    await row.getByRole('button', { name: 'Rotate token' }).click();
    await page.getByRole('dialog', { name: 'Rotate token' }).getByRole('button', { name: 'Rotate token' }).click();
    await expect(alert.getByTestId('probe-token')).not.toHaveText(token);
    const rotated = (await alert.getByTestId('probe-token').textContent())!;
    expect(rotated).toMatch(/^pkp_/);
    expect(rotated).not.toBe(token);
    await expect(alert.getByTestId('probe-docker-command')).toContainText(`PROBE_TOKEN=${rotated}`);

    await row.getByRole('button', { name: 'Delete probe' }).click();
    await page.getByRole('dialog', { name: 'Delete probe' }).getByRole('button', { name: 'Delete', exact: true }).click();
    await expect(page.getByText('Probe deleted successfully')).toBeVisible();
    await expect(row).toHaveCount(0);
    // The token of a deleted probe goes with it.
    await expect(alert).toHaveCount(0);
  } finally {
    // Delete the probe here if the test failed before the UI deleted it.
    await deleteProbes(page, [probeName]);
  }
});

test('A probe is edited and disabled', async ({ page }) => {
  const probeName = `E2E probe ${randomID()}`;
  const renamed = `${probeName} renamed`;

  await page.goto('/probes');
  try {
    const api = await adminAPI(page);
    const created = await api.post('probes', { data: { name: probeName } });
    expect(created.status(), await created.text()).toBe(201);
    await api.dispose();
    await page.reload();

    await page.getByRole('row', { name: new RegExp(probeName) }).getByRole('button', { name: 'Edit probe' }).click();
    const dialog = page.getByRole('dialog', { name: 'Edit probe' });
    await dialog.locator('input[name="name"]').fill(renamed);
    await dialog.locator('input[name="location"]').fill('Frankfurt');
    await dialog.getByRole('switch', { name: 'Disabled' }).click();
    await dialog.getByRole('button', { name: 'Save' }).click();
    await expect(page.getByText('Probe updated successfully')).toBeVisible();

    const row = page.getByRole('row', { name: new RegExp(renamed) });
    await expect(row).toContainText('Frankfurt');
    await expect(row.getByText('Disabled')).toBeVisible();
  } finally {
    await deleteProbes(page, [probeName, renamed]);
  }
});

test("A probe's channels are set and cleared in the edit form", async ({ page }) => {
  const id = randomID();
  const probeName = `E2E probe ${id}`;
  const channelName = `E2E probe channel ${id}`;
  const hint = "Leave empty to notify the channels of this probe's monitors.";
  const api = await adminAPI(page);
  const created: string[] = [];

  try {
    const channel = await createWebhookChannel(api, channelName, webhookURL(id));
    created.push(`notification-channels/${channel.id}`);
    const response = await api.post('probes', { data: { name: probeName } });
    expect(response.status(), await response.text()).toBe(201);
    const probe: { id: string } = (await response.json()).data;
    created.push(`probes/${probe.id}`);

    const channelIDs = async () => {
      const read = await api.get(`probes/${probe.id}`);
      expect(read.status(), await read.text()).toBe(200);
      return (await read.json()).data.notification_ids;
    };
    expect(await channelIDs()).toEqual([]);

    await page.goto('/probes');
    const openEditDialog = async () => {
      await page.getByRole('row', { name: new RegExp(probeName) }).getByRole('button', { name: 'Edit probe' }).click();
      const dialog = page.getByRole('dialog', { name: 'Edit probe' });
      await expect(dialog.getByText('Notification channels')).toBeVisible();
      await expect(dialog.getByText(hint)).toBeVisible();
      return dialog;
    };

    // Add the channel
    let dialog = await openEditDialog();
    await dialog.getByRole('combobox', { name: 'Add Notifier' }).click();
    await page.getByRole('option', { name: channelName }).click();
    await expect(dialog.getByText(channelName)).toBeVisible();
    await dialog.getByRole('button', { name: 'Save' }).click();
    await expect(page.getByText('Probe updated successfully')).toBeVisible();
    await expect(dialog).toHaveCount(0);
    await expect.poll(channelIDs).toEqual([channel.id]);

    // Remove it again
    dialog = await openEditDialog();
    await expect(dialog.getByText(channelName)).toBeVisible();
    await dialog.getByRole('button', { name: `Remove ${channelName}` }).click();
    await dialog.getByRole('button', { name: 'Save' }).click();
    await expect(dialog).toHaveCount(0);
    await expect.poll(channelIDs).toEqual([]);
  } finally {
    // The probe goes first, then the channel it pointed at.
    for (const path of created.reverse()) await deleteCreated(api, path);
    await api.dispose();
  }
});
