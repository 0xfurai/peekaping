import { createHmac } from 'node:crypto';
import { test, expect, type APIRequestContext, type Locator, type Page } from '@playwright/test';
import { adminAPI, deleteCreated, randomID } from './support/api';
import { receivedWebhooks, webhookURL } from './support/fake-target';

// The API shows a saved secret as this, and the edit form hides it behind
// a hint instead of showing it in the field.
const savedHint = 'Saved — type to replace';
const reenterHint = "Re-enter this secret: where it's sent changed";
// What saving or testing says while a secret still holds the placeholder after its endpoint changed
const reenterError = "Re-enter this secret or remove it: where it's sent changed";
// What the form says, as soon as the URL is edited, about a URL that still holds masked credentials
const reenterURLError = "Type this URL's credentials again: the URL changed since it was saved";

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

test('Changing where a channel sends blocks saving until each saved secret is re-entered or removed', async ({ page, request }) => {
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
    const saves: string[] = [];
    const tests: string[] = [];
    page.on('request', (r) => {
      if (r.method() === 'PUT' && r.url().endsWith(`/notification-channels/${channelID}`)) saves.push(r.url());
      if (r.method() === 'POST' && r.url().endsWith('/notification-channels/test')) tests.push(r.url());
    });

    await page.goto(`/notification-channels/${channelID}/edit`);
    const signing = secretItem(page, 'webhook_signing_secret');
    const headersItem = secretItem(page, 'webhook_additional_headers');
    await expect(signing.getByText(savedHint)).toBeVisible();
    await expect(headersItem.getByText(savedHint)).toBeVisible();

    // A new URL can't keep the old secrets. They stay in the form, empty, and say why
    await page.locator('input[name="webhook_url"]').fill(webhookURL(newTarget));
    await expect(signing.locator('input')).toHaveValue('');
    await expect(signing.getByText(reenterHint)).toBeVisible();
    await expect(signing.getByText(savedHint)).toHaveCount(0);
    await expect(headersItem.locator('textarea')).toHaveValue('');
    await expect(headersItem.getByText(reenterHint)).toBeVisible();
    await expect(signing.getByText(reenterError)).toHaveCount(0);

    // Testing is blocked, with an error on each of them
    await page.getByRole('button', { name: 'Test', exact: true }).click();
    await expect(signing.getByText(reenterError)).toBeVisible();
    await expect(headersItem.getByText(reenterError)).toBeVisible();
    expect(tests).toHaveLength(0);

    // So is saving, and nothing changes in the saved channel
    await page.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(signing.getByText(reenterError)).toBeVisible();
    await expect(headersItem.getByText(reenterError)).toBeVisible();
    expect(saves).toHaveLength(0);
    expect(await savedConfig(api, channelID)).toMatchObject({ webhook_url: webhookURL(id) });

    // Typing the secret again clears its error. The other one still blocks saving
    await signing.locator('input').fill(newSecret);
    await expect(signing.getByText(reenterError)).toHaveCount(0);
    await expect(signing.getByText(reenterHint)).toHaveCount(0);
    await page.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(headersItem.getByText(reenterError)).toBeVisible();
    expect(saves).toHaveLength(0);

    // Removing the headers on purpose lets the form save, without them
    await headersItem.getByRole('button', { name: 'Remove', exact: true }).click();
    await expect(headersItem.getByText(reenterError)).toHaveCount(0);
    await expect(headersItem.getByText(reenterHint)).toHaveCount(0);
    const updated = page.waitForResponse(
      (response) => response.url().endsWith(`/notification-channels/${channelID}`) && response.request().method() === 'PUT',
    );
    await page.getByRole('button', { name: 'Save', exact: true }).click();
    const updatedResponse = await updated;
    expect(updatedResponse.status(), await updatedResponse.text()).toBe(200);
    expect(await savedConfig(api, channelID)).toMatchObject({
      webhook_url: webhookURL(newTarget),
      webhook_additional_headers: '',
      webhook_signing_secret: '********',
    });

    // The new target gets a notification signed with the secret that was typed, and no old header
    await page.goto(`/notification-channels/${channelID}/edit`);
    await expect(signing.getByText(savedHint)).toBeVisible();
    await pressTest(page);

    const [webhook] = await expectWebhooks(request, newTarget, 1);
    expect(webhook.headers['x-webhook-signature']).toBe(signature(newSecret, webhook));
    expect(webhook.headers['x-old-header']).toBeUndefined();
    expect(await receivedWebhooks(request, id)).toHaveLength(0);
  } finally {
    for (const path of created) await deleteCreated(api, path);
    await api.dispose();
  }
});

test('A saved secret the form does not show cannot block saving a new endpoint', async ({ page }) => {
  const id = randomID();
  const created: string[] = [];
  const api = await adminAPI(page);

  try {
    // Signing in with a password, the channel still has the token it was saved with
    const channelID = await createChannel(api, created, `Secrets ${id}`, 'ntfy', {
      server_url: webhookURL(id),
      topic: 'alerts',
      authentication_type: 'basic',
      username: 'ntfy-user',
      password: `password-${id}`,
      token: `token-${id}`,
      priority: 3,
    });

    await page.goto(`/notification-channels/${channelID}/edit`);
    const password = secretItem(page, 'password');
    await expect(password.getByText(savedHint)).toBeVisible();
    await expect(page.locator('[name="token"]')).toHaveCount(0);

    // A new server asks for the password that is shown. Nothing is asked for the token that isn't
    await page.locator('input[name="server_url"]').fill(webhookURL(`${id}-new`));
    await expect(password.getByText(reenterHint)).toBeVisible();
    await page.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(password.getByText(reenterError)).toBeVisible();
    await expect(page.getByText(reenterError)).toHaveCount(1);

    await password.locator('input').fill(`new-password-${id}`);
    const updated = page.waitForResponse(
      (response) => response.url().endsWith(`/notification-channels/${channelID}`) && response.request().method() === 'PUT',
    );
    await page.getByRole('button', { name: 'Save', exact: true }).click();
    const updatedResponse = await updated;
    expect(updatedResponse.status(), await updatedResponse.text()).toBe(200);
    expect(await savedConfig(api, channelID)).toMatchObject({
      server_url: webhookURL(`${id}-new`),
      password: '********',
      token: '',
    });
  } finally {
    for (const path of created) await deleteCreated(api, path);
    await api.dispose();
  }
});

test('A saved secret the form stops showing keeps its placeholder until the form sends', async ({ page }) => {
  const id = randomID();
  const created: string[] = [];
  const api = await adminAPI(page);

  try {
    const channelID = await createChannel(api, created, `Secrets ${id}`, 'ntfy', {
      server_url: webhookURL(id),
      topic: 'alerts',
      authentication_type: 'basic',
      username: 'ntfy-user',
      password: `password-${id}`,
      token: `token-${id}`,
      priority: 3,
    });

    await page.goto(`/notification-channels/${channelID}/edit`);
    const password = secretItem(page, 'password');
    const token = secretItem(page, 'token');
    const server = page.locator('input[name="server_url"]');
    await expect(password.getByText(savedHint)).toBeVisible();
    await expect(page.locator('[name="token"]')).toHaveCount(0);

    // A new server asks for the password that is shown
    await server.fill(webhookURL(`${id}-new`));
    await expect(password.getByText(reenterHint)).toBeVisible();

    // The token's field comes up with the secret it was saved with, and asks for it again
    await selectAuthType(page, 'Token Authentication');
    await expect(token.getByText(reenterHint)).toBeVisible();
    await expect(token.getByRole('button', { name: 'Remove', exact: true })).toBeVisible();
    await expect(page.locator('[name="password"]')).toHaveCount(0);

    // And back: the password was not emptied while its field was gone
    await selectAuthType(page, 'Basic Authentication');
    await expect(password.getByText(reenterHint)).toBeVisible();
    await expect(password.getByRole('button', { name: 'Remove', exact: true })).toBeVisible();

    // The server as it was saved: both are the saved secrets again
    await server.fill(webhookURL(id));
    await expect(password.getByText(savedHint)).toBeVisible();
    await expect(password.getByText(reenterHint)).toHaveCount(0);

    // The test request keeps both as placeholders, for the server to resolve
    await page.route('**/notification-channels/test', (route) =>
      route.fulfill({ status: 200, json: { message: 'intercepted', data: null } }),
    );
    const testRequest = page.waitForRequest((r) => r.url().endsWith('/notification-channels/test'));
    await page.getByRole('button', { name: 'Test', exact: true }).click();
    const body = (await testRequest).postDataJSON();
    expect(body.id).toBe(channelID);
    expect(JSON.parse(body.config)).toMatchObject({ password: '********', token: '********' });

    // So does saving, and the server finds both
    const updated = page.waitForResponse(
      (response) => response.url().endsWith(`/notification-channels/${channelID}`) && response.request().method() === 'PUT',
    );
    await page.getByRole('button', { name: 'Save', exact: true }).click();
    const updatedResponse = await updated;
    expect(updatedResponse.status(), await updatedResponse.text()).toBe(200);
    expect(await savedConfig(api, channelID)).toMatchObject({
      server_url: webhookURL(id),
      password: '********',
      token: '********',
    });
  } finally {
    for (const path of created) await deleteCreated(api, path);
    await api.dispose();
  }
});

test('A saved secret can be removed on purpose, and only that one', async ({ page, request }) => {
  const id = randomID();
  const signingSecret = `signing-${id}`;
  const created: string[] = [];
  const api = await adminAPI(page);

  try {
    const channelID = await createWebhookChannel(api, created, `Secrets ${id}`, webhookURL(id), {
      webhook_additional_headers: JSON.stringify({ 'X-Saved-Header': `kept-${id}` }),
      webhook_signing_secret: signingSecret,
    });

    await page.goto(`/notification-channels/${channelID}/edit`);
    const signing = secretItem(page, 'webhook_signing_secret');
    const headersItem = secretItem(page, 'webhook_additional_headers');
    await expect(headersItem.getByText(savedHint)).toBeVisible();

    await headersItem.getByRole('button', { name: 'Remove', exact: true }).click();
    await expect(headersItem.locator('textarea')).toHaveValue('');
    await expect(headersItem.getByText(savedHint)).toHaveCount(0);
    await expect(headersItem.getByRole('button', { name: 'Remove', exact: true })).toHaveCount(0);
    await expect(signing.getByText(savedHint)).toBeVisible();

    const updated = page.waitForResponse(
      (response) => response.url().endsWith(`/notification-channels/${channelID}`) && response.request().method() === 'PUT',
    );
    await page.getByRole('button', { name: 'Save', exact: true }).click();
    const updatedResponse = await updated;
    expect(updatedResponse.status(), await updatedResponse.text()).toBe(200);
    expect(await savedConfig(api, channelID)).toMatchObject({
      webhook_additional_headers: '',
      webhook_signing_secret: '********',
    });

    // The channel sends without the headers, and still signs with the secret it kept
    await page.goto(`/notification-channels/${channelID}/edit`);
    await expect(headersItem.getByText(savedHint)).toHaveCount(0);
    await expect(signing.getByText(savedHint)).toBeVisible();
    await pressTest(page);

    const [webhook] = await expectWebhooks(request, id, 1);
    expect(webhook.headers['x-saved-header']).toBeUndefined();
    expect(webhook.headers['x-webhook-signature']).toBe(signature(signingSecret, webhook));
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

test('A webhook URL with masked credentials must have them typed again once it is edited', async ({ page }) => {
  const id = randomID();
  const password = `hunter2-${id}`;
  const token = `tok-${id}`;
  const masked = `${webhookURL(id).replace('://', '://user:********@')}?token=********`;
  const created: string[] = [];
  const api = await adminAPI(page);

  try {
    const channelID = await createWebhookChannel(
      api,
      created,
      `Secrets ${id}`,
      `${webhookURL(id).replace('://', `://user:${password}@`)}?token=${token}`,
      {},
    );
    const saves: string[] = [];
    page.on('request', (r) => {
      if (r.method() === 'PUT' && r.url().endsWith(`/notification-channels/${channelID}`)) saves.push(r.url());
    });

    // The URL is shown with its credentials masked, and the page never holds them
    await page.goto(`/notification-channels/${channelID}/edit`);
    const url = page.locator('input[name="webhook_url"]');
    const urlItem = secretItem(page, 'webhook_url');
    await expect(url).toHaveValue(masked);
    await expect(urlItem.getByText(reenterURLError)).toHaveCount(0);
    const content = await page.content();
    expect(content).not.toContain(password);
    expect(content).not.toContain(token);

    // Another path with the masks still in it can't keep the credentials: the URL says so at
    // once, and saving is refused
    const moved = masked.replace(`/webhooks/${id}`, `/webhooks/${id}-new`);
    await url.fill(moved);
    await expect(urlItem.getByText(reenterURLError)).toBeVisible();
    await page.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(urlItem.getByText(reenterURLError)).toBeVisible();
    expect(saves).toHaveLength(0);
    expect(await savedConfig(api, channelID)).toMatchObject({ webhook_url: masked });

    // The URL as shown is the saved one again
    await url.fill(masked);
    await expect(urlItem.getByText(reenterURLError)).toHaveCount(0);

    // Typing the credentials again clears the error, and the form saves the new URL
    await url.fill(moved);
    await expect(urlItem.getByText(reenterURLError)).toBeVisible();
    await url.fill(`${webhookURL(`${id}-new`).replace('://', '://user:new-password@')}?token=new-token`);
    await expect(urlItem.getByText(reenterURLError)).toHaveCount(0);
    const updated = page.waitForResponse(
      (response) => response.url().endsWith(`/notification-channels/${channelID}`) && response.request().method() === 'PUT',
    );
    await page.getByRole('button', { name: 'Save', exact: true }).click();
    const updatedResponse = await updated;
    expect(updatedResponse.status(), await updatedResponse.text()).toBe(200);
    expect(await savedConfig(api, channelID)).toMatchObject({
      webhook_url: `${webhookURL(`${id}-new`).replace('://', '://user:********@')}?token=********`,
    });
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

// The config a channel is saved with, as the API shows it: saved secrets are the placeholder
async function savedConfig(api: APIRequestContext, channelID: string): Promise<Record<string, unknown>> {
  const response = await api.get(`notification-channels/${channelID}`);
  expect(response.status(), await response.text()).toBe(200);
  return JSON.parse((await response.json()).data.config);
}

// Picks an ntfy authentication type in the edit form
async function selectAuthType(page: Page, option: string) {
  await page.getByRole('combobox', { name: 'Authentication Type' }).click();
  await page.getByRole('option', { name: option, exact: true }).click();
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
