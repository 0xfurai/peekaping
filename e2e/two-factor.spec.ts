import { test, expect } from '@playwright/test';
import { admin, apiBaseURL } from './support/admin';
import { adminAPI } from './support/api';
import { totp } from './support/totp';

// 2FA goes on the one admin every spec shares, so it must be off again
// however the test ends, or the next run's setup can't log in. An afterEach
// hook still runs when the test times out.
test.afterEach(async ({ page }) => {
  const api = await adminAPI(page);
  const disabled = await api.post('auth/2fa/disable', { data: admin });
  expect(disabled.ok(), `turn 2FA off: ${await disabled.text()}`).toBeTruthy();
  await api.dispose();
});

test('With 2FA on, login asks for the authenticator code', async ({ page, browser }) => {
  await page.goto('/security');
  await page.getByLabel('Current Password', { exact: true }).fill(admin.password);
  await page.getByRole('button', { name: 'Continue' }).click();
  const secret = (
    await page.getByText('Or enter this secret manually').locator('xpath=following-sibling::div').textContent()
  )?.trim();
  expect(secret, 'the 2FA secret shown for manual entry').toBeTruthy();
  await page.locator('input[name="code"]').fill(totp(secret!));
  await page.getByRole('button', { name: 'Enable 2FA' }).click();
  await expect(page.getByText('Two-Factor Authentication Enabled')).toBeVisible();

  const visitor = await browser.newContext({
    baseURL: test.info().project.use.baseURL,
    storageState: { cookies: [], origins: [] },
  });
  const loginPage = await visitor.newPage();
  await loginPage.goto('/login');
  await loginPage.getByRole('textbox', { name: 'Email' }).fill(admin.email);
  await loginPage.locator('input[name="password"]').fill(admin.password);
  await loginPage.getByRole('button', { name: 'Login' }).click();
  await expect(loginPage.getByText('Two-Factor Authentication Required')).toBeVisible();

  // A code from five minutes ahead is outside the window the server accepts.
  const code = loginPage.locator('input[name="code"]');
  await code.fill(totp(secret!, Date.now() + 5 * 60_000));
  await loginPage.getByRole('button', { name: 'Verify 2FA' }).click();
  await expect(loginPage.getByRole('alert').filter({ hasText: 'invalid 2FA token' })).toBeVisible();

  await code.fill(totp(secret!));
  await loginPage.getByRole('button', { name: 'Verify 2FA' }).click();
  await expect(loginPage).toHaveURL(/\/monitors$/);
  await visitor.close();

  await page.reload();
  await page.getByRole('button', { name: 'Disable 2FA' }).click();
  await page.getByPlaceholder('Enter your password to disable 2FA').fill(admin.password);
  await page.getByRole('button', { name: 'Disable 2FA' }).click();
  await expect(page.getByText('2FA disabled successfully')).toBeVisible();

  const login = await page.request.post(`${await apiBaseURL(page)}/auth/login`, { data: admin });
  expect(login.ok(), 'password-only login works again').toBeTruthy();
});

test('A wrong setup code is refused and 2FA stays off', async ({ page }) => {
  await page.goto('/security');
  await page.getByLabel('Current Password', { exact: true }).fill(admin.password);
  await page.getByRole('button', { name: 'Continue' }).click();
  const secret = (
    await page.getByText('Or enter this secret manually').locator('xpath=following-sibling::div').textContent()
  )?.trim();
  expect(secret, 'the 2FA secret shown for manual entry').toBeTruthy();

  // A code from five minutes ahead is outside the window the server accepts.
  await page.locator('input[name="code"]').fill(totp(secret!, Date.now() + 5 * 60_000));
  await page.getByRole('button', { name: 'Enable 2FA' }).click();
  await expect(page.getByRole('alert').filter({ hasText: 'invalid 2FA code' })).toBeVisible();

  // The code step stays open and 2FA is not marked as on.
  await expect(page.getByRole('button', { name: 'Enable 2FA' })).toBeVisible();
  await expect(page.getByText('Two-Factor Authentication Enabled')).toBeHidden();
  await page.reload();
  await expect(page.getByRole('button', { name: 'Disable 2FA' })).toBeHidden();
  const login = await page.request.post(`${await apiBaseURL(page)}/auth/login`, { data: admin });
  expect(login.ok(), 'password-only login still works').toBeTruthy();
});
