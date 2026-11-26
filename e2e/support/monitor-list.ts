import type { Page } from '@playwright/test';

// The monitor list narrowed to one test's monitors. The id is lowercase on
// purpose: on Postgres the list search matches only a lowercase term (PP-72).
export const listFor = (id: string) => `/monitors?search=${id}`;

// The room of whichever workspace the page opens in. The test doesn't know its workspace's ID
// before the address redirects to it, so the list waits for a join of any workspace room.
export const WORKSPACE_ROOM = /^workspace:[0-9a-f-]{36}$/i;

// Opens the page and waits until the app has joined the websocket room, so a
// heartbeat recorded from then on reaches the page without a reload.
export async function openLive(page: Page, url: string, room: string | RegExp) {
  const isRoom = (name: string) => (typeof room === 'string' ? name === room : room.test(name));
  const joined = page.waitForEvent('console', (message) => {
    const text = message.text();
    const prefix = 'Subscribed to heartbeat ';
    return text.startsWith(prefix) && isRoom(text.slice(prefix.length));
  });
  await Promise.all([joined, page.goto(url)]);
}

// The status name beside the icon on the monitor's card in the list. It's matched
// as an element of its own, since the card's text runs on from the status into the name.
export const cardStatusOf = (page: Page, name: string) =>
  page
    .getByTestId('monitor-card')
    .filter({ hasText: name })
    .getByText(/^\s*(Up|Down|Pending|Maintenance|Paused)\s*$/);
