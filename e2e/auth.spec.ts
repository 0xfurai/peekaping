import { test, expect } from '@playwright/test';

test('Register new user', async ({ page }) => {
  await page.goto('/register');
  await page.getByRole('textbox', { name: 'Email' }).click();
  await page.getByRole('textbox', { name: 'Email' }).fill('test@test.com');
  await page.getByRole('textbox', { name: 'Password', exact: true }).click();
  await page.getByRole('textbox', { name: 'Password', exact: true }).fill('TestPassword1234!');
  await page.getByRole('textbox', { name: 'Password', exact: true }).press('Tab');
  await page.getByRole('textbox', { name: 'Confirm Password' }).fill('TestPassword1234!');
  await page.getByRole('button', { name: 'Create' }).click();

  await page.waitForURL('**/monitors', { timeout: 10000 });
  await expect(page).toHaveURL(/.*\/monitors$/);
});
