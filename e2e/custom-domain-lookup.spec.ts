import { test, expect, type Page, type Route } from '@playwright/test';

// Every page load asks the API whether the current host is a custom status-page domain. On a
// normal host the answer is 404, so the query keeps no data, and a background refetch (a
// reconnect after a laptop wakes, say) puts it back to pending. The router used to unmount
// every route while that refetch ran, which wiped whatever was typed into an open form
// (#161, #228).
const LOOKUP = '**/api/v1/status-pages/domain/**';

const NOT_FOUND = {
  status: 404,
  contentType: 'application/json',
  body: JSON.stringify({ message: 'Status page not found', data: null }),
};

// The chromium project runs logged in; these tests are about the screens a visitor sees.
test.use({ storageState: { cookies: [], origins: [] } });

// Answers the lookup with a 404, and can hold requests open so a test can look at the app
// while one is in flight. `release` opens the gate for good: requests that arrive afterwards
// are answered straight away.
async function stubDomainLookup(page: Page) {
  let holding = false;
  let held = 0;
  let fulfilled = 0;
  const waiting: Array<() => void> = [];

  await page.route(LOOKUP, async (route: Route) => {
    if (holding) {
      held += 1;
      await new Promise<void>((resolve) => waiting.push(resolve));
    }
    await route.fulfill(NOT_FOUND);
    fulfilled += 1;
  });

  return {
    hold: () => {
      holding = true;
    },
    held: () => held,
    release: () => {
      holding = false;
      waiting.splice(0).forEach((resolve) => resolve());
    },
    fulfilled: () => fulfilled,
  };
}

test('a reconnect refetch leaves the half-filled login form alone', async ({
  page,
  context,
}) => {
  const lookup = await stubDomainLookup(page);

  await page.goto('/login');
  const email = page.getByRole('textbox', { name: 'Email' });
  await expect(email).toBeVisible();
  await email.fill('visitor@example.com');
  // Marks the input, so the assertions below tell the form the visitor was filling in apart
  // from a fresh one: a remount loses the mark and everything typed.
  await email.evaluate((input) =>
    input.setAttribute('data-e2e-half-filled', 'true')
  );
  const halfFilled = page.locator('[data-e2e-half-filled="true"]');

  lookup.hold();
  await context.setOffline(true);
  await context.setOffline(false);

  await expect
    .poll(() => lookup.held(), {
      message: 'a reconnect should refetch the domain lookup',
    })
    .toBeGreaterThan(0);

  // The refetch is in flight, and with the old router this is where the routes unmounted.
  await expect(halfFilled).toHaveCount(1);
  await expect(email).toHaveValue('visitor@example.com');

  const settled = lookup.fulfilled();
  lookup.release();
  await expect
    .poll(() => lookup.fulfilled(), {
      message: 'the refetched domain lookup should finish',
    })
    .toBeGreaterThan(settled);

  // ... and after it finishes, the form is still the one that was being filled in.
  await expect(halfFilled).toHaveCount(1);
  await expect(email).toBeVisible();
  await expect(email).toHaveValue('visitor@example.com');
});

test('the login page waits for the first lookup', async ({ page }) => {
  const lookup = await stubDomainLookup(page);
  lookup.hold();

  await page.goto('/login');
  const email = page.getByRole('textbox', { name: 'Email' });

  await expect
    .poll(() => lookup.held(), {
      message: 'the first domain lookup should be in flight',
    })
    .toBeGreaterThan(0);
  await expect(email).toBeHidden();

  lookup.release();
  await expect(email).toBeVisible();
});
