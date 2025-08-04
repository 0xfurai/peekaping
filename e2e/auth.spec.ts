import { test, expect } from '@playwright/test';

test('Register new user', async ({ page }) => {
  await page.goto('/register');

  await page.getByRole('textbox', { name: 'Email' }).click();
  await page.getByRole('textbox', { name: 'Email' }).fill('test@test.com');

  const passwordInput = page.getByRole('textbox', { name: 'Password', exact: true });
  const toggleButton = page.getByRole('button', { name: /show password|hide password/i });

  await expect(passwordInput).toHaveAttribute('type', 'password');
  await passwordInput.fill('TestPassword1234!');

  await toggleButton.click();
  await expect(passwordInput).toHaveAttribute('type', 'text');

  await toggleButton.click();
  await expect(passwordInput).toHaveAttribute('type', 'password');

  await page.getByRole('textbox', { name: 'Confirm Password' }).fill('TestPassword1234!');
  await page.getByRole('button', { name: 'Create' }).click();

  await page.waitForURL('**/monitors', { timeout: 10000 });
  await expect(page).toHaveURL(/.*\/monitors$/);
});
