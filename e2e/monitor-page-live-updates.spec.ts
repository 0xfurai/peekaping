import { randomUUID } from 'node:crypto';
import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import { adminAPI, deleteCreated, randomID } from './support/api';

// The server handles the packets of one client in whatever order its goroutines run them, so a
// `leave_room` sent right before a `join_room` for the same room can be applied last and leave
// the socket outside the room (PP-366). The monitor page's room effect ran again whenever the
// heartbeats query refetched (window focus, for one), and each run sent that pair for a room the
// page stays in. The page now stays in its room.
//
// The test opens a monitor page, makes the heartbeats query refetch, and reads what the page
// wrote to its socket. A push through the real stack must still reach the page afterwards.

const frameFor = (event: 'join_room' | 'leave_room', room: string) =>
  new RegExp(`^42\\d*\\["${event}","${room}"\\]$`);

// Passes every socket.io connection through to the real server and records the frames the page
// writes to it. Must run before the page opens its socket.
async function watchSockets(page: Page) {
  const connections: string[][] = [];

  await page.routeWebSocket(/socket\.io/, (ws) => {
    const server = ws.connectToServer();
    const sent: string[] = [];
    connections.push(sent);
    // Setting a handler turns off the automatic forwarding of the page's frames, so it has to
    // pass them on itself. The server's frames still reach the page on their own.
    ws.onMessage((message) => {
      sent.push(String(message));
      server.send(message);
    });
  });

  return connections;
}

// The "Current status" on the monitor page.
const pageStatusOf = (page: Page) =>
  page.locator('[data-slot="card"]').filter({ hasText: 'Current status' }).locator('.text-2xl');

// Max retries 0 makes a failed push Down straight away, and the long check interval keeps the
// timeout check of push monitors from adding a Down heartbeat of its own while the test runs.
async function createPushMonitor(api: APIRequestContext, name: string) {
  const pushToken = randomUUID().replace(/-/g, '');
  const response = await api.post('monitors', {
    data: {
      type: 'push',
      name,
      active: true,
      interval: 3600,
      timeout: 16,
      max_retries: 0,
      retry_interval: 60,
      resend_interval: 0,
      notification_ids: [],
      tag_ids: [],
      config: JSON.stringify({ pushToken }),
      push_token: pushToken,
    },
  });
  expect(response.status(), await response.text()).toBe(201);
  return { id: (await response.json()).data.id as string, pushToken };
}

async function pushHeartbeat(api: APIRequestContext, pushToken: string, status: 'up' | 'down') {
  const response = await api.get(`push/${pushToken}`, { params: { status } });
  expect(response.status(), await response.text()).toBe(200);
}

// A new monitor gets a first check straight away, and a push monitor with nothing pushed yet
// answers it with a Down heartbeat. Waiting for it keeps that heartbeat from landing in the
// middle of the test.
async function waitForFirstCheck(api: APIRequestContext, id: string) {
  await expect
    .poll(
      async () => {
        const response = await api.get(`monitors/${id}/heartbeats`, { params: { limit: 1 } });
        return (await response.json()).data?.[0]?.status;
      },
      { message: 'the first check should record a Down heartbeat', timeout: 20_000 },
    )
    .toBe(0);
}

// The heartbeat travels through a queue, so one push can arrive before the page's socket is in
// the room. Pushing again on every attempt keeps the check free of fixed sleeps.
async function pushUntilPageShows(
  page: Page,
  api: APIRequestContext,
  pushToken: string,
  status: 'up' | 'down',
) {
  const shown = status === 'up' ? 'Up' : 'Down';
  await expect
    .poll(
      async () => {
        await pushHeartbeat(api, pushToken, status);
        return (await pageStatusOf(page).innerText()).trim();
      },
      { message: `the page should show ${shown} after a ${status} push`, timeout: 20_000, intervals: [1_000] },
    )
    .toBe(shown);
}

test('a monitor page stays in its room when its heartbeats refetch, and keeps getting live heartbeats', async ({
  page,
}) => {
  test.setTimeout(90_000);
  const api = await adminAPI(page);
  let id: string | undefined;
  try {
    const monitor = await createPushMonitor(api, `Room stay ${randomID()}`);
    id = monitor.id;
    await waitForFirstCheck(api, id);
    const room = `monitor:${id}`;
    const connections = await watchSockets(page);

    await page.goto(`/monitors/${id}`);
    await expect(pageStatusOf(page)).toHaveText('Down');
    await expect
      .poll(() => connections[0]?.some((frame) => frameFor('join_room', room).test(frame)), {
        message: 'the page should join its room',
      })
      .toBe(true);

    // A live heartbeat changes what the server returns, so the refetch below brings new data
    // and the page's room effect would run again.
    await pushUntilPageShows(page, api, monitor.pushToken, 'up');

    // react-query refetches stale queries when the window gets focus again, and the heartbeats
    // query is always stale.
    const refetched = page.waitForResponse(
      (response) =>
        response.request().method() === 'GET' &&
        response.url().includes(`/monitors/${id}/heartbeats`) &&
        response.url().includes('limit=150'),
    );
    await page.evaluate(() => window.dispatchEvent(new Event('visibilitychange')));
    await refetched;

    // The page still gets live heartbeats. This also gives a render that follows the refetch
    // time to run before the frames are read.
    await pushUntilPageShows(page, api, monitor.pushToken, 'down');

    const frames = connections.flat();
    // The first frame carries the access token, so the messages show only the room frames
    const roomFrames = JSON.stringify(frames.filter((frame) => frame.includes('_room')));
    expect(connections, 'the page should keep its one socket').toHaveLength(1);
    expect(
      frames.filter((frame) => frameFor('leave_room', room).test(frame)),
      `the page sent leave_room for ${room}; room frames sent: ${roomFrames}`,
    ).toEqual([]);
    expect(
      frames.filter((frame) => frameFor('join_room', room).test(frame)),
      `the page joined ${room} more than once; room frames sent: ${roomFrames}`,
    ).toHaveLength(1);
  } finally {
    if (id) await deleteCreated(api, `monitors/${id}`);
    await api.dispose();
  }
});
