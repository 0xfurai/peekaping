import { test, expect, type Page } from '@playwright/test';
import { adminAPI, createHTTPMonitor, deleteCreated, randomID } from './support/api';
import { targetURL } from './support/fake-target';

// The heartbeat bar works out how many segments fit from the width of its container, and pads
// with empty ones up to that count. It used to recount only when the browser window resized,
// so opening or closing the sidebar, which changes the container's width without a window
// resize, left the bar with the old count: too short beside a closed sidebar, cut off beside
// an open one.
//
// The bar renders exactly as many segments as it counts fit, with empty ones where there is no
// heartbeat yet, so a new monitor's segments show the count whatever its heartbeats are. The
// monitor page has one bar.

// Wider than the 768px at which the sidebar becomes a sheet over the page, so it takes room
// from the content instead of covering it.
const desktop = { width: 1280, height: 720 };

const segmentsOn = (page: Page) => page.getByTestId('heartbeat-bar-segment');
const segmentCount = (page: Page) => () => segmentsOn(page).count();
const toggleSidebar = (page: Page) => page.locator('[data-slot="sidebar-trigger"]').click();

test('opening and closing the sidebar changes how many bars the heartbeat bar shows', async ({ page }) => {
  const id = randomID();
  const created: string[] = [];

  await page.setViewportSize(desktop);
  const api = await adminAPI(page);
  try {
    const monitor = await createHTTPMonitor(api, `Bar ${id}`, targetURL(id));
    created.push(`monitors/${monitor.id}`);

    await page.goto(`/monitors/${monitor.id}`);
    // The sidebar starts open.
    await expect.poll(segmentCount(page), 'bars beside the open sidebar').toBeGreaterThan(0);
    const withSidebar = await segmentsOn(page).count();

    // The sidebar slides away over 200 ms, so the count settles a moment later.
    await toggleSidebar(page);
    await expect.poll(segmentCount(page), 'bars once the sidebar is closed').toBeGreaterThan(withSidebar);

    await toggleSidebar(page);
    await expect.poll(segmentCount(page), 'bars once the sidebar is open again').toBe(withSidebar);
  } finally {
    for (const path of created.reverse()) await deleteCreated(api, path);
    await api.dispose();
  }
});

test('resizing the window changes how many bars the heartbeat bar shows', async ({ page }) => {
  const id = randomID();
  const created: string[] = [];

  await page.setViewportSize(desktop);
  const api = await adminAPI(page);
  try {
    const monitor = await createHTTPMonitor(api, `Bar ${id}`, targetURL(id));
    created.push(`monitors/${monitor.id}`);

    await page.goto(`/monitors/${monitor.id}`);
    await expect.poll(segmentCount(page), 'bars in the wide window').toBeGreaterThan(0);
    const wide = await segmentsOn(page).count();

    await page.setViewportSize({ ...desktop, width: 1000 });
    await expect.poll(segmentCount(page), 'bars in the narrower window').toBeLessThan(wide);
  } finally {
    for (const path of created.reverse()) await deleteCreated(api, path);
    await api.dispose();
  }
});
