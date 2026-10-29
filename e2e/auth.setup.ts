import { test as setup, expect, type Page } from '@playwright/test';
import { admin, apiBaseURL } from './support/admin';
import { linkIn, waitForMail } from './support/mail';

// Signs in as the e2e admin and saves the session for every other spec. A
// fresh database gets the admin through the register form, which opens Get
// started; the setup skips it so that every later spec starts from the monitors
// list. A database from an earlier run already has the admin, so the setup logs
// in instead. On an install with a sender for account emails (the cloud suite's, with the mail
// catcher), a new admin's workspace waits for their email to be verified, so the setup opens the
// link from the catcher before it skips Get started.
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
    // Mail from an earlier database's admin, left in the catcher, has a link that no longer works
    const registeringAt = new Date();
    await register(page);
    await getStarted(page, (await sendsAccountEmails(page)) ? registeringAt : null);
  }

  await expect(page).toHaveURL(/\/monitors$/);
  await page.context().storageState({ path: 'storageState.json' });
});

async function sendsAccountEmails(page: Page): Promise<boolean> {
  const config = await page.request.get(`${await apiBaseURL(page)}/auth/config`);
  expect(config.ok(), `GET auth/config: ${await config.text()}`).toBeTruthy();
  return (await config.json()).data.accountEmails === true;
}

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

// A new install's first user lands on Get started for their workspace. Continue with its name as it
// is, then skip the rest of the setup. With a sender for account emails, the address is verified
// first, with the link in the mail the install sent.
async function getStarted(page: Page, verifyEmailSince: Date | null) {
  await expect(page).toHaveURL(/\/w\/[0-9a-f-]+\/get-started$/);
  if (verifyEmailSince) {
    const mail = await waitForMail(admin.email, /Verify your email/, { since: verifyEmailSince });
    const verification = await page.context().newPage();
    await verification.goto(linkIn(mail, '/verify-email/'));
    await expect(verification.getByText('Your email is verified.')).toBeVisible();
    await verification.close();
  }
  await expect(page.getByText('Name your workspace')).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'Name' })).toHaveValue('My workspace');
  await page.getByRole('button', { name: 'Continue' }).click();
  await page.getByRole('link', { name: 'Skip setup' }).click();
}
