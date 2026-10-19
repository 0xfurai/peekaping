import { test as setup, expect, type Page } from '@playwright/test';
import { admin, apiBaseURL } from './support/admin';

// Signs in as the e2e admin and saves the session for every other spec. A
// fresh database gets the admin through the register form; a database from an
// earlier run already has it, so the setup logs in instead.
setup('Sign in as the e2e admin', async ({ page }) => {
  await page.goto('/login');
  const login = await page.request.post(`${await apiBaseURL(page)}/auth/login`, { data: admin });

  if (login.ok()) {
    await logIn(page);
  } else {
    const { message } = await login.json();
    expect(
      message,
      message === '2FA token required'
        ? `2FA is on for ${admin.email}, probably left by an interrupted run. Turn it off on the Security page, or reset the database.`
        : `Unexpected login answer for ${admin.email}`,
    ).toBe('invalid credentials');
    await register(page);
  }

  await expect(page).toHaveURL(/\/monitors$/);
  await page.context().storageState({ path: 'storageState.json' });
});

async function logIn(page: Page) {
  await page.getByRole('textbox', { name: 'Email' }).fill(admin.email);
  await page.locator('input[name="password"]').fill(admin.password);
  await page.getByRole('button', { name: 'Login' }).click();
}

async function register(page: Page) {
  await page.goto('/register');

  await page.getByRole('textbox', { name: 'Email' }).fill(admin.email);

  // Get the password field container and locate the input and toggle button
  const passwordContainer = page.locator('div:has(> label:text("Password"))').first();
  const passwordField = passwordContainer.locator('input[type="password"], input[type="text"]').first();
  const passwordToggle = passwordContainer.locator('button[aria-label*="password"]').first();

  // Initially password should be hidden (type="password")
  await expect(passwordField).toHaveAttribute('type', 'password');
  await passwordField.fill(admin.password);

  // Toggle to show password
  await passwordToggle.click();
  await expect(passwordField).toHaveAttribute('type', 'text');

  // Toggle back to hide password
  await passwordToggle.click();
  await expect(passwordField).toHaveAttribute('type', 'password');

  const confirmPasswordContainer = page.locator('div:has(> label:text("Confirm Password"))').first();
  const confirmPasswordField = confirmPasswordContainer.locator('input[type="password"], input[type="text"]').first();
  await confirmPasswordField.fill(admin.password);

  const registered = page.waitForResponse((response) => response.url().endsWith('/auth/register'));
  await page.getByRole('button', { name: 'Create' }).click();
  // Login said "invalid credentials", so a 400 here means an admin with other credentials exists.
  expect(
    (await registered).status(),
    `Registering ${admin.email} failed. If the database has an admin with other credentials, set E2E_EMAIL and E2E_PASSWORD or reset the database.`,
  ).toBe(201);
}
