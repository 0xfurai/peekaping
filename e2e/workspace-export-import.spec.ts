import { readFileSync } from 'node:fs';
import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import { adminAPI, createHTTPMonitor, createWebhookChannel, deleteCreated, randomID } from './support/api';
import { targetURL, webhookURL } from './support/fake-target';

// An owner downloads their workspace's setup as one file, which holds saved secrets in full, and
// imports it into another workspace, which adds the records beside the ones there (PP-170). The
// heartbeat history is a file of its own.

const UUID = '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}';

async function createWorkspace(api: APIRequestContext, name: string): Promise<string> {
  const response = await api.post('workspaces', { data: { name } });
  expect(response.status(), await response.text()).toBe(201);
  return (await response.json()).data.id;
}

// The id of the workspace the admin lands in
async function defaultWorkspaceId(page: Page): Promise<string> {
  await page.goto('/');
  await expect(page).toHaveURL(new RegExp(`/w/${UUID}/monitors$`));
  return new URL(page.url()).pathname.split('/')[2];
}

// Runs every step even when an earlier one throws, so a failed cleanup step can't leave the ones
// after it undone, then reports the failures together.
async function runAll(steps: (() => Promise<void>)[]) {
  const failures: unknown[] = [];
  for (const step of steps) {
    try {
      await step();
    } catch (error) {
      failures.push(error);
    }
  }
  if (failures.length === 1) throw failures[0];
  if (failures.length > 1) throw new AggregateError(failures, `${failures.length} cleanup steps failed`);
}

type ExportedChannel = { name: string; config: Record<string, unknown>; sensitiveFields: string[] };
type ExportedMonitor = { name: string; tagRefs: string[]; notificationChannelRefs: string[] };

test('@critical an owner downloads the workspace, imports the file into another workspace and sees what came in', async ({
  page,
}, testInfo) => {
  const id = randomID();
  const tagName = `Export tag ${id}`;
  const channelName = `Export channel ${id}`;
  const monitorName = `Export monitor ${id}`;
  const signingSecret = `signing-${id}`;
  const api = await adminAPI(page);
  const cleanup: (() => Promise<void>)[] = [];
  const later = (path: string) => cleanup.unshift(() => deleteCreated(api, path));

  try {
    // The target workspace, which the owner makes; deleting it deletes what the import adds
    const targetId = await createWorkspace(api, `Import target ${id}`);
    cleanup.push(() => deleteCreated(api, `admin/workspaces/${targetId}`));

    // A tag, a webhook channel with a saved secret, and an HTTP monitor that uses both
    const tag = await api.post('tags', { data: { name: tagName, color: '#3B82F6', description: 'e2e' } });
    expect(tag.status(), await tag.text()).toBe(201);
    const tagId = (await tag.json()).data.id as string;
    later(`tags/${tagId}`);
    const channel = await api.post('notification-channels', {
      data: {
        name: channelName,
        type: 'webhook',
        active: true,
        is_default: false,
        config: JSON.stringify({
          webhook_url: webhookURL(id),
          webhook_content_type: 'json',
          webhook_signing_secret: signingSecret,
        }),
      },
    });
    expect(channel.status(), await channel.text()).toBe(201);
    const channelId = (await channel.json()).data.id as string;
    later(`notification-channels/${channelId}`);
    const monitor = await createHTTPMonitor(api, monitorName, targetURL(id), {
      notificationIds: [channelId],
      tagIds: [tagId],
    });
    later(`monitors/${monitor.id}`);

    // The export says first that the file holds secrets, and downloads one JSON file
    const sourceId = await defaultWorkspaceId(page);
    await page.goto(`/w/${sourceId}/settings`);
    await expect(page.getByText('The file holds saved secrets')).toBeVisible();
    await expect(page.getByText('Store the file like a password')).toBeVisible();
    const workspaceDownload = page.waitForEvent('download');
    await page.getByRole('button', { name: 'Download workspace' }).click();
    const download = await workspaceDownload;
    expect(download.suggestedFilename()).toMatch(/^peekaping-workspace-\d{4}-\d{2}-\d{2}\.json$/);
    const filePath = testInfo.outputPath('workspace.json');
    await download.saveAs(filePath);

    const document = JSON.parse(readFileSync(filePath, 'utf8'));
    expect(document.format).toBe('peekaping-workspace');
    expect(document.containsSecrets).toBe(true);
    const exportedMonitor = (document.monitors as ExportedMonitor[]).find((m) => m.name === monitorName);
    expect(exportedMonitor, 'the monitor is in the file').toBeTruthy();
    expect(exportedMonitor?.tagRefs).toHaveLength(1);
    expect(exportedMonitor?.notificationChannelRefs).toHaveLength(1);
    expect((document.tags as { name: string }[]).map((t) => t.name)).toContain(tagName);
    // The secret is in the file in full, listed as sensitive, not masked
    const exportedChannel = (document.notificationChannels as ExportedChannel[]).find((c) => c.name === channelName);
    expect(exportedChannel?.config.webhook_signing_secret).toBe(signingSecret);
    expect(exportedChannel?.sensitiveFields).toContain('webhook_signing_secret');

    // The heartbeat history is a file of its own: a header line first
    const heartbeatDownload = page.waitForEvent('download');
    await page.getByRole('button', { name: 'Download heartbeat history' }).click();
    const heartbeats = await heartbeatDownload;
    expect(heartbeats.suggestedFilename()).toMatch(/^peekaping-heartbeats-\d{4}-\d{2}-\d{2}\.ndjson$/);
    const heartbeatsPath = testInfo.outputPath('heartbeats.ndjson');
    await heartbeats.saveAs(heartbeatsPath);
    const header = JSON.parse(readFileSync(heartbeatsPath, 'utf8').split('\n')[0]);
    expect(header.format).toBe('peekaping-heartbeats');
    expect(header.version).toBe(1);

    // The other workspace imports the setup: the owner confirms what an import does, then reads the answer
    await page.goto(`/w/${targetId}/settings`);
    await page.locator('input[type="file"]').setInputFiles(filePath);
    const confirm = page.getByRole('dialog');
    await expect(confirm).toContainText('importing the same file twice adds them twice');
    await confirm.getByRole('button', { name: 'Import', exact: true }).click();

    const result = page.getByRole('dialog').filter({ hasText: 'Import finished' });
    await expect(result).toBeVisible();
    // At least this test's records came in, on top of whatever else the source workspace holds
    await expect(result.getByTestId('import-created')).toContainText(/Monitors: [1-9]/);
    await expect(result.getByTestId('import-created')).toContainText(/Tags: [1-9]/);
    await expect(result.getByTestId('import-created')).toContainText(/Notification channels: [1-9]/);
    await result.getByRole('button', { name: 'Close' }).first().click();
    await expect(result).toHaveCount(0);

    // The monitor is in the other workspace's list, and not a second time in the first one
    await page.goto(`/w/${targetId}/monitors?search=${id}`);
    await expect(page.getByTestId('monitor-card').filter({ hasText: monitorName })).toHaveCount(1);
    await page.goto(`/w/${sourceId}/monitors?search=${id}`);
    await expect(page.getByTestId('monitor-card').filter({ hasText: monitorName })).toHaveCount(1);
  } finally {
    try {
      await runAll(cleanup);
    } finally {
      await api.dispose();
    }
  }
});

test('@critical an import of a file that is not JSON sends nothing, and a refused file lists its problems', async ({
  page,
}) => {
  const id = randomID();
  const api = await adminAPI(page);
  let targetId: string | undefined;

  try {
    targetId = await createWorkspace(api, `Import refused ${id}`);
    await page.goto(`/w/${targetId}/settings`);

    // Not JSON: a message, no dialog, no request
    const requests: string[] = [];
    page.on('request', (request) => {
      if (request.url().endsWith('/workspace/import')) requests.push(request.url());
    });
    await page.locator('input[type="file"]').setInputFiles({
      name: 'notes.json',
      mimeType: 'application/json',
      buffer: Buffer.from('this is not json'),
    });
    await expect(page.getByText('This file is not valid JSON, so nothing was sent.')).toBeVisible();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    expect(requests).toHaveLength(0);

    // JSON that is not a workspace export: the server refuses it, writes nothing, and says why
    await page.locator('input[type="file"]').setInputFiles({
      name: 'other.json',
      mimeType: 'application/json',
      buffer: Buffer.from(JSON.stringify({ format: 'something-else', version: 1 })),
    });
    await page.getByRole('dialog').getByRole('button', { name: 'Import', exact: true }).click();
    const refused = page.getByRole('dialog').filter({ hasText: "The file can't be imported" });
    await expect(refused).toBeVisible();
    await expect(refused.getByTestId('import-problems').getByRole('listitem').first()).toContainText('file:');
    await refused.getByRole('button', { name: 'Close' }).first().click();

    const monitors = await api.get('monitors', { headers: { 'X-Workspace-ID': targetId } });
    expect(monitors.ok(), await monitors.text()).toBeTruthy();
    expect((await monitors.json()).data ?? []).toHaveLength(0);
  } finally {
    try {
      if (targetId) await deleteCreated(api, `admin/workspaces/${targetId}`);
    } finally {
      await api.dispose();
    }
  }
});
