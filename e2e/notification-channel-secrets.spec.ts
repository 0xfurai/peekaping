import { createHmac } from 'node:crypto';
import { test, expect, type APIRequestContext, type Locator, type Page } from '@playwright/test';
import { adminAPI, deleteCreated, randomID } from './support/api';
import { receivedWebhooks, webhookURL } from './support/fake-target';

// The API shows a saved secret as this, and the edit form hides it behind
// a hint instead of showing it in the field.
const savedHint = 'Saved — type to replace';
const reenterHint = "Re-enter this secret: where it's sent changed";

test('A saved webhook secret stays hidden in the edit form and survives a rename', async ({ page, request }) => {
  const id = randomID();
  const name = `Secrets ${id}`;
  const signingSecret = `signing-${id}`;
  const headers = JSON.stringify({ 'X-Saved-Header': `kept-${id}` });
  const created: string[] = [];
  const api = await adminAPI(page);

  try {
    // Create the channel through the form, with both of the webhook's secrets
    await page.goto('/notification-channels/new');
    await page.getByRole('combobox', { name: 'Notifier Type' }).click();
    await page.getByRole('option', { name: 'Webhook', exact: true }).click();
    await page.locator('input[name="name"]').fill(name);
    await page.locator('input[name="webhook_url"]').fill(webhookURL(id));
    await page.getByRole('combobox', { name: 'Request Body' }).click();
    await page.getByRole('option', { name: 'application/json', exact: true }).click();
    await page.getByRole('switch').click();
    await page.locator('textarea[name="webhook_additional_headers"]').fill(headers);
    await page.locator('input[name="webhook_signing_secret"]').fill(signingSecret);

    const createdResponse = page.waitForResponse(
      (response) => response.url().endsWith('/notification-channels') && response.request().method() === 'POST',
    );
    await page.getByRole('button', { name: 'Save', exact: true }).click();
    const response = await createdResponse;
    expect(response.status(), await response.text()).toBe(201);
    const channelID: string = (await response.json()).data.id;
    created.push(`notification-channels/${channelID}`);
    await expect(page).toHaveURL(/\/notification-channels$/);

    // The saved secrets show as empty fields with the hint
    await page.goto(`/notification-channels/${channelID}/edit`);
    const signing = secretItem(page, 'webhook_signing_secret');
    await expect(signing.locator('input')).toHaveValue('');
    await expect(signing.getByText(savedHint)).toBeVisible();
    const headersItem = secretItem(page, 'webhook_additional_headers');
    await expect(headersItem.locator('textarea')).toHaveValue('');
    await expect(headersItem.getByText(savedHint)).toBeVisible();

    // Renaming without touching them keeps them
    await page.locator('input[name="name"]').fill(`${name} renamed`);
    const updated = page.waitForResponse(
      (response) => response.url().endsWith(`/notification-channels/${channelID}`) && response.request().method() === 'PUT',
    );
    await page.getByRole('button', { name: 'Save', exact: true }).click();
    const updatedResponse = await updated;
    expect(updatedResponse.status(), await updatedResponse.text()).toBe(200);
    await expect(page).toHaveURL(/\/notification-channels$/);

    // Testing without touching them uses the saved ones: the target gets a request
    // signed with the signing secret, and carrying the saved header
    await page.goto(`/notification-channels/${channelID}/edit`);
    await expect(page.locator('input[name="name"]')).toHaveValue(`${name} renamed`);
    await expect(signing.getByText(savedHint)).toBeVisible();
    await pressTest(page);

    const [webhook] = await expectWebhooks(request, id, 1);
    expect(webhook.headers['x-saved-header']).toBe(`kept-${id}`);
    expect(webhook.headers['x-webhook-signature']).toBe(signature(signingSecret, webhook));
  } finally {
    for (const path of created) await deleteCreated(api, path);
    await api.dispose();
  }
});

test('Changing where a channel sends asks for its saved secrets again', async ({ page, request }) => {
  const id = randomID();
  const newTarget = `${id}-new`;
  const newSecret = `new-signing-${id}`;
  const created: string[] = [];
  const api = await adminAPI(page);

  try {
    const channelID = await createWebhookChannel(api, created, `Secrets ${id}`, webhookURL(id), {
      webhook_additional_headers: JSON.stringify({ 'X-Old-Header': 'old' }),
      webhook_signing_secret: `old-signing-${id}`,
    });

    await page.goto(`/notification-channels/${channelID}/edit`);
    const signing = secretItem(page, 'webhook_signing_secret');
    const headersItem = secretItem(page, 'webhook_additional_headers');
    await expect(signing.getByText(savedHint)).toBeVisible();
    await expect(headersItem.getByText(savedHint)).toBeVisible();

    // A new URL can't keep the old secrets: the fields empty out and say why
    await page.locator('input[name="webhook_url"]').fill(webhookURL(newTarget));
    await expect(signing.locator('input')).toHaveValue('');
    await expect(signing.getByText(reenterHint)).toBeVisible();
    await expect(signing.getByText(savedHint)).toHaveCount(0);
    await expect(headersItem.locator('textarea')).toHaveValue('');
    await expect(headersItem.getByText(reenterHint)).toBeVisible();

    // Typing the secret again saves, and the new target gets it
    await signing.locator('input').fill(newSecret);
    await expect(signing.getByText(reenterHint)).toHaveCount(0);
    const updated = page.waitForResponse(
      (response) => response.url().endsWith(`/notification-channels/${channelID}`) && response.request().method() === 'PUT',
    );
    await page.getByRole('button', { name: 'Save', exact: true }).click();
    const updatedResponse = await updated;
    expect(updatedResponse.status(), await updatedResponse.text()).toBe(200);

    await page.goto(`/notification-channels/${channelID}/edit`);
    await expect(signing.getByText(savedHint)).toBeVisible();
    await pressTest(page);

    const [webhook] = await expectWebhooks(request, newTarget, 1);
    expect(webhook.headers['x-webhook-signature']).toBe(signature(newSecret, webhook));
    expect(await receivedWebhooks(request, id)).toHaveLength(0);
  } finally {
    for (const path of created) await deleteCreated(api, path);
    await api.dispose();
  }
});

test('Changing the type leaves no saved secret placeholder in the form', async ({ page }) => {
  const id = randomID();
  const created: string[] = [];
  const api = await adminAPI(page);

  try {
    const channelID = await createWebhookChannel(api, created, `Secrets ${id}`, webhookURL(id), {
      webhook_signing_secret: `signing-${id}`,
    });

    await page.goto(`/notification-channels/${channelID}/edit`);
    await expect(secretItem(page, 'webhook_signing_secret').getByText(savedHint)).toBeVisible();

    await page.getByRole('combobox', { name: 'Notifier Type' }).click();
    await page.getByRole('option', { name: 'Discord', exact: true }).click();
    await expect(page.locator('input[name="webhook_url"]')).toBeVisible();

    // The test request is what the form would send: look at it, without sending it on
    await page.route('**/notification-channels/test', (route) =>
      route.fulfill({ status: 200, json: { message: 'intercepted', data: null } }),
    );
    const testRequest = page.waitForRequest((r) => r.url().endsWith('/notification-channels/test'));
    await page.getByRole('button', { name: 'Test', exact: true }).click();
    const body = (await testRequest).postDataJSON();
    expect(body.type).toBe('discord');
    expect(JSON.stringify(body)).not.toContain('********');
  } finally {
    for (const path of created) await deleteCreated(api, path);
    await api.dispose();
  }
});

test('A saved webhook URL passes the form checks untouched', async ({ page, request }) => {
  const id = randomID();
  const name = `Secrets ${id}`;
  const created: string[] = [];
  const api = await adminAPI(page);

  try {
    // A Discord channel's webhook URL is a secret, and the form requires it to be a URL
    const channelID = await createChannel(api, created, name, 'discord', {
      webhook_url: webhookURL(id),
      bot_display_name: 'Peekaping',
      message_type: 'send_to_channel',
    });

    await page.goto(`/notification-channels/${channelID}/edit`);
    const urlItem = secretItem(page, 'webhook_url');
    await expect(urlItem.locator('input')).toHaveValue('');
    await expect(urlItem.getByText(savedHint)).toBeVisible();

    await page.locator('input[name="name"]').fill(`${name} renamed`);
    const updated = page.waitForResponse(
      (response) => response.url().endsWith(`/notification-channels/${channelID}`) && response.request().method() === 'PUT',
    );
    await page.getByRole('button', { name: 'Save', exact: true }).click();
    const updatedResponse = await updated;
    expect(updatedResponse.status(), await updatedResponse.text()).toBe(200);
    await expect(page).toHaveURL(/\/notification-channels$/);

    // The saved URL is the one the test notification goes to
    await page.goto(`/notification-channels/${channelID}/edit`);
    await pressTest(page);
    await expectWebhooks(request, id, 1);
  } finally {
    for (const path of created) await deleteCreated(api, path);
    await api.dispose();
  }
});

// A webhook channel made through the API, which keeps its config as it is given.
function createWebhookChannel(
  api: APIRequestContext,
  created: string[],
  name: string,
  url: string,
  secrets: { webhook_additional_headers?: string; webhook_signing_secret?: string },
): Promise<string> {
  return createChannel(api, created, name, 'webhook', { webhook_url: url, webhook_content_type: 'json', ...secrets });
}

async function createChannel(
  api: APIRequestContext,
  created: string[],
  name: string,
  type: string,
  config: Record<string, unknown>,
): Promise<string> {
  const response = await api.post('notification-channels', {
    data: { name, type, active: true, is_default: false, config: JSON.stringify(config) },
  });
  expect(response.status(), await response.text()).toBe(201);
  const channelID: string = (await response.json()).data.id;
  created.push(`notification-channels/${channelID}`);
  return channelID;
}

// The form item of one config field
function secretItem(page: Page, field: string): Locator {
  return page.locator('[data-slot="form-item"]').filter({ has: page.locator(`[name="${field}"]`) });
}

// Presses Test in the edit form and waits for the server to say it sent the notification
async function pressTest(page: Page) {
  const tested = page.waitForResponse(
    (response) => response.url().endsWith('/notification-channels/test') && response.request().method() === 'POST',
  );
  await page.getByRole('button', { name: 'Test', exact: true }).click();
  const response = await tested;
  expect(response.status(), await response.text()).toBe(200);
}

async function expectWebhooks(request: APIRequestContext, target: string, count: number) {
  await expect.poll(async () => (await receivedWebhooks(request, target)).length, { timeout: 15_000 }).toBe(count);
  return receivedWebhooks(request, target);
}

// The signature a receiver rebuilds from the secret, the timestamp header and the body
function signature(secret: string, webhook: { headers: Record<string, string>; rawBody: string }) {
  const timestamp = webhook.headers['x-webhook-timestamp'];
  return `sha256=${createHmac('sha256', secret).update(`${timestamp}.${webhook.rawBody}`).digest('hex')}`;
}
