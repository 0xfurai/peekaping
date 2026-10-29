import { test, expect, type APIRequestContext } from '@playwright/test';
import { admin } from './support/admin';
import { adminAPI, randomID } from './support/api';
import { mailpitSMTP, mailpitURL, waitForMail } from './support/mail';

// An instance admin sets up the sender of account emails in Admin → Settings, with Mailpit as its
// SMTP server, sends a test email, and then a password reset email arrives (PP-373).
//
// It needs Mailpit (docker-compose.e2e.yml, E2E_MAILPIT_URL) and a sender the environment doesn't
// set, since a variable that is set locks its field: so the cloud suite, which sets one, skips it.
// The sender it saves is removed at the end, however the test ends: the other specs expect an
// install with no sender (admin-users.spec.ts makes a reset link with none).
test.describe.configure({ mode: 'serial' });

type Setting = { value: unknown; source: string; locked: boolean };
type InstallSettings = {
  signupMode: Setting;
  accountSmtp: Record<'host' | 'port' | 'user' | 'from' | 'tls', Setting> & {
    password: { set: boolean; source: string; locked: boolean };
  };
  accountEmails: boolean;
};

async function installSettings(api: APIRequestContext): Promise<InstallSettings> {
  const response = await api.get('admin/settings');
  expect(response.ok(), `GET admin/settings: ${await response.text()}`).toBeTruthy();
  return (await response.json()).data;
}

const senderLocked = ({ accountSmtp }: InstallSettings) =>
  Object.values(accountSmtp).some((setting) => setting.locked);

test('an instance admin saves an email sender, sends a test email, and a password reset email arrives', async ({
  page,
}) => {
  test.skip(!mailpitURL, 'needs Mailpit (docker-compose.e2e.yml) and E2E_MAILPIT_URL');
  // Made before signing out, for the cleanup: signing out clears the page's session
  const api = await adminAPI(page);
  try {
    const before = await installSettings(api);
    test.skip(senderLocked(before), "the server's environment sets the sender, so Admin → Settings can't change it");
    test.skip(before.signupMode.value === 'open', 'open signup needs its sender, so the test could not remove it');

    // 1. Admin → Settings: Mailpit as the sender, saved here
    await page.goto('/admin/users');
    await page.getByRole('link', { name: 'Settings', exact: true }).click();
    await expect(page).toHaveURL(/\/admin\/settings$/);

    const host = page.getByLabel('SMTP Host', { exact: true });
    await host.fill(mailpitSMTP.host);
    await page.getByLabel('Port', { exact: true }).fill(String(mailpitSMTP.port));
    await page.getByLabel('Security', { exact: true }).selectOption('none');
    await page.getByLabel('From address', { exact: true }).fill('peekaping@e2e.test');
    await page.getByRole('button', { name: 'Save', exact: true }).click();

    await expect(page.getByText('Settings saved')).toBeVisible();
    await expect(host).toHaveAccessibleDescription('Saved here');
    await expect(page.getByLabel('From address', { exact: true })).toHaveAccessibleDescription('Saved here');
    await expect(page.getByText('Account emails are on.')).toBeVisible();
    // It is stored, not just shown
    await page.reload();
    await expect(host).toHaveValue(mailpitSMTP.host);
    await expect(host).toHaveAccessibleDescription('Saved here');

    // 2. A test email to an address of its own, which Mailpit receives
    const to = `settings-${randomID()}@e2e.test`;
    await page.getByLabel('Send to', { exact: true }).fill(to);
    await page.getByRole('button', { name: 'Send a test email' }).click();
    await expect(page.getByRole('status').filter({ hasText: `Test email sent to ${to}.` })).toBeVisible();
    await waitForMail(to, /^Peekaping test email$/);

    // 3. Signed out, "Forgot password" for the admin's address: the reset email arrives. Its link
    //    is left alone, since using it would end every session of the admin the other specs share.
    await page.getByRole('button', { name: admin.email }).click();
    await page.getByRole('menuitem', { name: 'Log out' }).click();
    await expect(page).toHaveURL(/\/login$/);
    // Mailpit's clock and this one may be a little apart
    const asked = new Date(Date.now() - 5_000);
    await page.getByRole('link', { name: 'Forgot password?' }).click();
    await page.getByRole('textbox', { name: 'Email' }).fill(admin.email);
    await page.getByRole('button', { name: 'Send link' }).click();
    await expect(page.getByText('If an account exists for this email, we sent a link.')).toBeVisible();
    await waitForMail(admin.email, /^Reset your Peekaping password$/, { since: asked, body: /\/reset-password\// });
  } finally {
    // 4. The sender goes again, so the specs after this one see an install with no sender
    const settings = await installSettings(api);
    if (!senderLocked(settings) && settings.signupMode.value !== 'open') {
      const cleared = await api.put('admin/settings', {
        data: { accountSmtp: { host: '', port: 0, user: '', password: '', from: '', tls: '' } },
      });
      expect(cleared.ok(), `remove the saved sender: ${await cleared.text()}`).toBeTruthy();
      const after = await installSettings(api);
      expect(after.accountEmails, 'no sender after the cleanup').toBe(false);
      for (const name of ['host', 'port', 'user', 'from', 'tls'] as const) {
        expect(after.accountSmtp[name].source, `accountSmtp.${name} after the cleanup`).toBe('default');
      }
      expect(after.accountSmtp.password).toMatchObject({ set: false, source: 'default' });
    }
    await api.dispose();
  }
});
