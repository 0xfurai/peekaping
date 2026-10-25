import { test, expect, type APIRequestContext } from '@playwright/test';
import { adminAPI, createHTTPMonitor, deleteCreated, randomID, updateHTTPMonitor } from './support/api';
import { setTargetStatus, targetURL } from './support/fake-target';
import { cardStatusOf, listFor, openLive } from './support/monitor-list';

// A new monitor is checked at once and then every 20 seconds, the minimum
// check interval. If whatever runs the checks misses the create event, its next
// refresh (within about 30 seconds) picks the monitor up. The heartbeat then still has to reach
// the page.
const nextCheck = { timeout: 60_000 };

// A page that has loaded shows the list's cards, or its empty state, as soon as
// the API has answered the list's request.
const listLoads = { timeout: 10_000 };

// The list filters by the status stored on each monitor, while its cards show the
// latest heartbeat. A full update (PUT) used to overwrite the stored status with
// Up, so a Down monitor edited that way left the Down filter for the Up one (PP-189).
test('a Down monitor edited through the API is listed under Down and not under Up', async ({ page, request }) => {
  // If every wait runs out they add up to 105 seconds: 10 to join the room, 60 for
  // the card to show Down, 10 for the API to store it, 10 for the Down list, and 10
  // and 5 for the Up list. The timeout has to be longer, so a failing wait fails on
  // its own assertion and the finally block still runs.
  test.setTimeout(180_000);
  const id = randomID();
  const name = `Failing ${id}`;
  const editedName = `${name} (edited)`;
  const created: string[] = [];

  await setTargetStatus(request, id, 500);
  const api = await adminAPI(page);
  try {
    // With max retries 0 the first failed check is Down at once.
    const monitor = await createHTTPMonitor(api, name, targetURL(id));
    created.push(`monitors/${monitor.id}`);

    await openLive(page, listFor(id), 'monitor:all');
    await expect(cardStatusOf(page, name)).toHaveText('Down', nextCheck);

    // The card turns Down when the heartbeat reaches the page, which can be before
    // the API has stored the status. Edit only once it has, or the test would pass
    // even if an edit overwrote the status.
    await expect
      .poll(() => storedStatusOf(api, monitor.id), {
        message: 'the API should store the monitor as Down',
        timeout: 10_000,
      })
      .toBe(0);

    await updateHTTPMonitor(api, monitor.id, editedName, targetURL(id));

    const editedCard = page.getByTestId('monitor-card').filter({ hasText: editedName });
    await page.goto(`${listFor(id)}&status=down`);
    await expect(editedCard).toBeVisible(listLoads);

    // The empty state shows once the list has loaded without a match, so the card
    // can't be absent just because the list is still loading.
    await page.goto(`${listFor(id)}&status=up`);
    await expect(page.getByText('No monitors found')).toBeVisible(listLoads);
    await expect(editedCard).toHaveCount(0);
  } finally {
    for (const path of created.reverse()) await deleteCreated(api, path);
    await api.dispose();
  }
});

// The status stored on the monitor, which the list filters by: 0 is Down.
async function storedStatusOf(api: APIRequestContext, id: string): Promise<number> {
  const response = await api.get(`monitors/${id}`);
  expect(response.ok(), await response.text()).toBeTruthy();
  return (await response.json()).data.status;
}
