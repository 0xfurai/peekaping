import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import { apiBaseURL } from './support/admin';
import { adminAPI, deleteCreated, randomID } from './support/api';
import { setTargetStatus, targetURL } from './support/fake-target';
import { linkIn, waitForMail } from './support/mail';

// A visitor to an open-signup install signs up, is walked through Get started, and verifies their
// email address (PP-370). The workspace of a new signup is suspended until then: the monitor is
// saved, but is not checked and sends nothing, so the test follows it across the verification.
//
// It needs an install with SIGNUP_MODE=open and a sender for account emails that a mail catcher
// receives (Mailpit in docker-compose.e2e.yml, E2E_MAILPIT_URL), and skips on any other. The first user
// of a fresh install is covered by auth.setup.ts.
test.use({ storageState: { cookies: [], origins: [] } });

const UUID = '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}';
const password = 'OnboardingPassword1234!';

// The "Current status" on the monitor page.
const statusCard = (page: Page) =>
  page.locator('[data-slot="card"]').filter({ hasText: 'Current status' }).locator('.text-2xl');

test('@cloud a new signup is walked through Get started and verifies their email', async ({ page }) => {
  await page.goto('/login');
  const config = await page.request.get(`${await apiBaseURL(page)}/auth/config`);
  expect(config.ok(), `GET auth/config: ${await config.text()}`).toBeTruthy();
  const { signupMode, accountEmails } = (await config.json()).data;
  test.skip(
    signupMode !== 'open' || accountEmails !== true,
    'needs an install with open signup and a sender for account emails (SIGNUP_MODE=open, ACCOUNT_SMTP_HOST=mailpit)',
  );

  const run = randomID();
  const email = `onboarding-${run}@e2e.test`;
  const workspaceName = `Onboarding ${run}`;
  const targetID = `onboarding-${run}`;
  const created: string[] = [];
  let api: APIRequestContext | undefined;

  try {
    await test.step('sign up and land on Get started with the verification notice', async () => {
      const signedUpAt = new Date();
      await page.goto('/register');
      await page.getByLabel('Email').fill(email);
      await page.getByLabel('Password', { exact: true }).fill(password);
      await page.getByLabel('Confirm Password').fill(password);
      await page.getByRole('button', { name: 'Create' }).click();

      await expect(page).toHaveURL(new RegExp(`/w/${UUID}/get-started$`));
      await expect(page.getByText('Your monitors start once you verify your email.')).toBeVisible();
      api = await adminAPI(page);
      // The mail that carries the link is sent with the signup
      await waitForMail(email, /Verify your email/, { since: signedUpAt });
    });

    await test.step('1. name the workspace', async () => {
      await expect(page.getByText('Name your workspace')).toBeVisible();
      await page.getByRole('textbox', { name: 'Name' }).fill(workspaceName);
      await page.getByRole('button', { name: 'Continue' }).click();
    });

    await test.step('2. add the monitor', async () => {
      await expect(page.getByText('Add your first monitor')).toBeVisible();
      await page.getByRole('textbox', { name: 'Address to monitor' }).fill(targetURL(targetID));
      await page.getByRole('button', { name: 'Create monitor' }).click();
    });

    await test.step('3. choose where alerts go: Email, to the signed-up address', async () => {
      await expect(page.getByText('Get notified')).toBeVisible();
      await expect(page.getByRole('combobox', { name: 'Notifier Type' })).toHaveText('Email');
      await expect(page.getByRole('combobox', { name: 'Recipient' })).toHaveValue(email);
      await page.getByRole('textbox', { name: 'Friendly name' }).fill(`Email ${run}`);
      await page.getByRole('button', { name: 'Save', exact: true }).click();
    });

    // 4. the last step says that the monitor waits for the address to be verified
    await expect(page.getByText("You're all set")).toBeVisible();
    await expect(page.getByText('Your monitor runs once your email address is verified.')).toBeVisible();
    const monitorLink = page.getByRole('link', { name: 'Open your monitor' });
    const monitorPath = await monitorLink.getAttribute('href');
    expect(monitorPath).toMatch(new RegExp(`^/w/${UUID}/monitors/${UUID}$`));
    const monitorID = monitorPath!.split('/').pop()!;
    created.push(`monitors/${monitorID}`);

    await test.step('verify the address with the link in the mail', async () => {
      const mail = await waitForMail(email, /Verify your email/);
      await page.goto(linkIn(mail, '/verify-email/'));
      await expect(page.getByText('Your email is verified.')).toBeVisible();
      await page.getByRole('link', { name: 'Open Peekaping' }).click();
      await expect(page).toHaveURL(new RegExp(`/w/${UUID}/monitors$`));
      await expect(page.getByText('Your monitors start once you verify your email.')).toHaveCount(0);
      await expect(page.getByText(/^Verify your email: we sent a link to /)).toHaveCount(0);
    });

    await test.step('the monitor has the Email channel and, now the workspace runs, is checked', async () => {
      const channels = await api!.get(`monitors/${monitorID}`);
      expect(channels.ok(), await channels.text()).toBeTruthy();
      const notificationIDs: string[] = (await channels.json()).data.notification_ids;
      expect(notificationIDs, 'the channel of step 3 is linked to the monitor').toHaveLength(1);
      created.push(`notification-channels/${notificationIDs[0]}`);
      const channel = await api!.get(`notification-channels/${notificationIDs[0]}`);
      expect(channel.ok(), await channel.text()).toBeTruthy();
      const { type, config: channelConfig } = (await channel.json()).data;
      expect(type).toBe('install_email');
      expect(JSON.parse(channelConfig).to).toBe(email);

      await page.goto(monitorPath!);
      await expect(page.getByText('Current status')).toBeVisible();
      // The first check may take until the next scheduler round
      await expect(statusCard(page)).toHaveText('Up', { timeout: 60_000 });
    });

    await test.step('a Down monitor emails the signed-up address', async () => {
      const downAt = new Date();
      await setTargetStatus(page.request, targetID, 503);
      await expect(statusCard(page)).toHaveText('Down', { timeout: 60_000 });
      const mail = await waitForMail(email, /Peekaping Notification/, { since: downAt, timeout: 60_000 });
      expect(mail.to).toEqual([email]);
    });
  } finally {
    // Back up, so that the target doesn't keep failing for another run, then the monitor before its
    // channel: a monitor that is gone can't send. The signed-up user stays, as no API deletes users.
    await setTargetStatus(page.request, targetID, 200).catch(() => undefined);
    if (api) {
      for (const path of created) await deleteCreated(api, path);
      await api.dispose();
    }
  }
});
