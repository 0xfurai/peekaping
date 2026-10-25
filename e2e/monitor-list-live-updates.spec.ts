import { randomUUID } from 'node:crypto';
import {
  test,
  expect,
  type APIRequestContext,
  type BrowserContext,
  type Locator,
  type Page,
} from '@playwright/test';

// The monitor list used to subscribe to the `monitor:all` room once, behind a ref, and its
// cleanup removed the heartbeat listener whenever the socket dropped. After a reconnect the
// effect ran again but the ref said it had already subscribed, so it never joined the room
// again and the cards stopped updating until the page was reloaded (PP-205).
//
// The test opens the monitor list, drops the page's socket the way a restarting API would, lets
// socket.io-client reconnect by itself, and then pushes a heartbeat to a push monitor. The
// push goes through the real stack (push endpoint, ingest, event bus, websocket), so
// the only way the card can change is through the room subscription. It never reloads or
// navigates, because a fresh page load subscribes again and would hide the bug.

const JOIN_ALL_ROOM = /^42\d*\["join_room","monitor:all"\]$/;

// The browser talks to the API directly (API_URL in env.js), so the test has to as well.
interface Api {
  baseUrl: string;
  token: string;
}

interface PushMonitor {
  id: string;
  name: string;
  pushToken: string;
}

// One socket.io websocket the page opened. `sent` holds every frame the page wrote to it.
interface SocketConnection {
  sent: string[];
  drop: () => Promise<void>;
}

const randomMonitorName = () =>
  `Live Updates ${Math.random().toString(36).substring(2, 15)}`;

// Passes every socket.io connection through to the real server, and keeps a record of each one
// so a test can drop it and see what the page said on the next one. Must run before the page
// opens its socket.
async function watchSockets(page: Page) {
  const connections: SocketConnection[] = [];

  await page.routeWebSocket(/socket\.io/, (ws) => {
    const server = ws.connectToServer();
    const sent: string[] = [];
    // Setting a handler turns off the automatic forwarding of the page's frames, so it has
    // to pass them on itself. The server's frames still reach the page on their own.
    ws.onMessage((message) => {
      sent.push(String(message));
      server.send(message);
    });
    connections.push({
      sent,
      drop: async () => {
        await ws.close();
        await server.close();
      },
    });
  });

  return connections;
}

// What the page said on each socket, for the failure message: it shows at a glance whether the
// page joined the room again after reconnecting.
function describeSockets(connections: SocketConnection[]) {
  const lines = connections.map((connection, index) => {
    const joined = connection.sent.some((frame) => JOIN_ALL_ROOM.test(frame));
    return `  connection #${index + 1}: ${
      joined ? 'sent' : 'did NOT send'
    } join_room monitor:all, frames sent: ${JSON.stringify(connection.sent)}`;
  });
  return `sockets opened by the page: ${connections.length}\n${lines.join('\n')}`;
}

// The access token lives in the page's local storage once the app has loaded (and refreshed it
// if the saved one had expired), so a throwaway page reads it. The monitor has to exist before
// the page under test opens the list, because the test is not allowed to reload it.
async function connectToApi(context: BrowserContext): Promise<Api> {
  const page = await context.newPage();
  try {
    await page.goto('/monitors');
    await expect(page.getByTestId('create-entity')).toBeVisible();
    return await page.evaluate(() => {
      const stored = JSON.parse(localStorage.getItem('auth-storage') ?? '{}');
      const config = (
        window as unknown as { __CONFIG__?: { API_URL?: string } }
      ).__CONFIG__;
      return {
        baseUrl: config?.API_URL || window.location.origin,
        token: stored.state.accessToken as string,
      };
    });
  } finally {
    await page.close();
  }
}

// Max retries 0 makes a failed push Down straight away (otherwise it would be Pending, which
// the card shows as Up), and the long check interval keeps the timeout check of push monitors
// from adding a Down heartbeat of its own, for a push that did not arrive in time, while the
// test runs.
async function createPushMonitor(
  request: APIRequestContext,
  api: Api,
  name: string
): Promise<PushMonitor> {
  const pushToken = randomUUID().replace(/-/g, '');
  const response = await request.post(`${api.baseUrl}/api/v1/monitors`, {
    headers: { Authorization: `Bearer ${api.token}` },
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

  const { data } = await response.json();
  return { id: data.id, name, pushToken: JSON.parse(data.config).pushToken };
}

async function deletePushMonitor(
  request: APIRequestContext,
  api: Api,
  monitor: PushMonitor
) {
  const response = await request.delete(
    `${api.baseUrl}/api/v1/monitors/${monitor.id}`,
    { headers: { Authorization: `Bearer ${api.token}` } }
  );
  expect(response.ok(), await response.text()).toBe(true);
}

// The status of the monitor's latest heartbeat as the server has it (0 is Down, 1 is Up), or
// undefined before the first one.
async function latestHeartbeatStatus(
  request: APIRequestContext,
  api: Api,
  monitor: PushMonitor
): Promise<number | undefined> {
  const response = await request.get(
    `${api.baseUrl}/api/v1/monitors/${monitor.id}/heartbeats`,
    {
      headers: { Authorization: `Bearer ${api.token}` },
      params: { limit: 1 },
    }
  );
  expect(response.ok(), await response.text()).toBe(true);
  const { data } = await response.json();
  return data?.[0]?.status;
}

// A new monitor gets a first check straight away, and a push monitor with nothing pushed yet
// answers it with a Down heartbeat. Waiting for it gives the test a fixed starting point
// (the card says Down), so a late first heartbeat cannot land in the middle of the test.
async function waitForFirstCheck(
  request: APIRequestContext,
  api: Api,
  monitor: PushMonitor
) {
  await expect
    .poll(() => latestHeartbeatStatus(request, api, monitor), {
      message: 'the first check should record a Down heartbeat',
      timeout: 20_000,
    })
    .toBe(0);
}

async function pushHeartbeat(
  request: APIRequestContext,
  api: Api,
  monitor: PushMonitor,
  status: 'up' | 'down'
) {
  const response = await request.get(
    `${api.baseUrl}/api/v1/push/${monitor.pushToken}`,
    { params: { status } }
  );
  expect(response.status(), await response.text()).toBe(200);
}

// The status the card shows for its monitor's latest heartbeat, or 'none' before the first one.
// The label's text has a leading space (it follows an icon), and a regex matches the raw text.
async function cardStatus(card: Locator) {
  const label = card.getByText(/^\s*(Up|Down)\s*$/);
  if ((await label.count()) === 0) return 'none';
  return (await label.first().innerText()).trim();
}

// The heartbeat travels through a queue and the page may not have joined the room yet, so one
// push can be missed. Pushing again on every attempt keeps the check free of fixed sleeps.
async function pushUntilCardShows(
  request: APIRequestContext,
  api: Api,
  monitor: PushMonitor,
  card: Locator,
  status: 'up' | 'down'
) {
  const shown = status === 'up' ? 'Up' : 'Down';
  await expect
    .poll(
      async () => {
        await pushHeartbeat(request, api, monitor, status);
        return cardStatus(card);
      },
      {
        message: `the card should show ${shown} after a ${status} push`,
        timeout: 20_000,
        intervals: [1_000],
      }
    )
    .toBe(shown);
}

let api: Api | undefined;
let monitor: PushMonitor | undefined;

test.afterEach(async ({ request }) => {
  if (!api || !monitor) return;
  const created = monitor;
  monitor = undefined;
  await deletePushMonitor(request, api, created);
});

test('a card on the monitor list keeps updating after the socket reconnects', async ({
  page,
  context,
  request,
}) => {
  // Setup, a push to see the card change, a reconnect, and a push to see it change again.
  test.setTimeout(90_000);

  api = await connectToApi(context);
  monitor = await createPushMonitor(request, api, randomMonitorName());
  await waitForFirstCheck(request, api, monitor);
  const connections = await watchSockets(page);

  await page.goto('/monitors');
  const card = page
    .getByTestId('monitor-card')
    .filter({ hasText: monitor.name });
  await expect(card).toBeVisible();
  await expect.poll(() => cardStatus(card)).toBe('Down');

  // Baseline: on the first connection the list gets live updates.
  await pushUntilCardShows(request, api, monitor, card, 'up');
  expect(connections).toHaveLength(1);
  expect(connections[0].sent.some((frame) => JOIN_ALL_ROOM.test(frame))).toBe(
    true
  );

  // The API goes away: the page's socket drops and socket.io-client opens a new one.
  await connections[0].drop();
  await expect
    .poll(() => connections.length, {
      message: 'the page should open a second socket after the first one drops',
      timeout: 15_000,
    })
    .toBe(2);

  // The next heartbeat has to reach the card without a reload.
  try {
    await pushUntilCardShows(request, api, monitor, card, 'down');
  } catch (error) {
    // Tells a page that stopped listening from a push that never made it to the server.
    const latest = await latestHeartbeatStatus(request, api, monitor);
    throw new Error(
      [
        describeSockets(connections),
        `latest heartbeat on the server: ${latest === 0 ? 'Down' : latest}`,
        '',
        (error as Error).message,
      ].join('\n')
    );
  }
});
