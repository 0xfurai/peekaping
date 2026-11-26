import { test, expect } from '@playwright/test';
import { apiBaseURL } from './support/admin';
import { adminAPI, deleteCreated, randomID } from './support/api';

test('@critical An API key authorizes API calls until it is deleted', async ({ page, request }) => {
  const keyName = `E2E key ${randomID()}`;

  await page.goto('/security');
  await page.getByRole('button', { name: 'Create API Key' }).first().click();
  const dialog = page.getByRole('dialog');
  await dialog.locator('input[name="name"]').fill(keyName);
  await dialog.getByRole('button', { name: 'Create API Key' }).click();
  const token = await page
    .getByRole('alert')
    .filter({ hasText: 'API Key Created Successfully' })
    .locator('code')
    .textContent();
  expect(token).toMatch(/^pk_/);

  const monitors = `${await apiBaseURL(page)}/monitors`;
  try {
    expect((await request.get(monitors, { headers: { 'X-API-Key': token! } })).status()).toBe(200);
    expect((await request.get(monitors)).status()).toBe(401);

    await page.getByRole('row', { name: new RegExp(keyName) }).getByRole('button').last().click();
    await page.getByRole('dialog', { name: 'Delete API Key' }).getByRole('button', { name: 'Delete' }).click();
    await expect(page.getByText('API key deleted successfully')).toBeVisible();

    const revoked = await request.get(monitors, { headers: { 'X-API-Key': token! } });
    expect(revoked.status()).toBe(401);
    expect((await revoked.json()).message).toBe('Invalid or expired API key');
  } finally {
    // Delete the key here if the test failed before the UI deleted it.
    const api = await adminAPI(page);
    const { data: keys } = await (await api.get('api-keys')).json();
    for (const key of keys.filter((k: { name: string }) => k.name === keyName)) {
      await deleteCreated(api, `api-keys/${key.id}`);
    }
    await api.dispose();
  }
});
