// Room subscriptions on a socket.io client socket.
//
// The server applies the packets of one client in whatever order its goroutines run them, so a
// `leave_room` followed at once by a `join_room` for the same room can end with the socket out
// of the room. The client therefore never sends such a pair: a room is left only when its last
// subscriber is gone, and the leave waits a microtask. A React effect's cleanup and its next run
// happen in the same synchronous commit, so a subscriber that is only being replaced is back
// before the leave goes out.

/**
 * The room of a workspace: the server lets a member of the workspace join it. The server names the
 * room by the lower-case ID and delivers its heartbeats as an event of that name, so an ID typed
 * in the address in upper case is lower-cased here.
 */
export const workspaceRoom = (workspaceId: string) => `workspace:${workspaceId.toLowerCase()}`;

/** The room of a monitor: the server lets a member of the monitor's workspace join it. */
export const monitorRoom = (monitorId: string) => `monitor:${monitorId}`;

/** The event a room delivers its heartbeats as. */
export const heartbeatEvent = (room: string) => `${room}:heartbeat`;

/** What the registry needs from a socket.io-client `Socket`. */
export interface RoomSocket {
  connected: boolean;
  emit(event: string, ...args: unknown[]): unknown;
  on(event: string, listener: (...args: unknown[]) => void): unknown;
  off(event: string, listener: (...args: unknown[]) => void): unknown;
}

interface Registry {
  // How many subscribers each room has
  subscribers: Map<string, number>;
  // The rooms this connection has told the server it joined
  joined: Set<string>;
}

const registries = new WeakMap<RoomSocket, Registry>();

function registryOf(socket: RoomSocket): Registry {
  const existing = registries.get(socket);
  if (existing) return existing;

  const registry: Registry = { subscribers: new Map(), joined: new Set() };
  registries.set(socket, registry);

  // A new connection starts outside every room, so each room with subscribers is joined again.
  // Emits made while disconnected are buffered by socket.io-client and sent on connect, which
  // is why nothing is emitted before then.
  socket.on("connect", () => {
    registry.joined.clear();
    for (const [room, count] of registry.subscribers) {
      if (count > 0) join(socket, registry, room);
    }
  });

  return registry;
}

function join(socket: RoomSocket, registry: Registry, room: string) {
  if (registry.joined.has(room)) return;
  registry.joined.add(room);
  socket.emit("join_room", room);
  // The e2e specs wait for this line (e2e/support/monitor-list.ts) to know the page is in the room
  console.log("Subscribed to heartbeat", room);
}

function leaveWhenUnused(socket: RoomSocket, registry: Registry, room: string) {
  queueMicrotask(() => {
    if ((registry.subscribers.get(room) ?? 0) > 0) return;
    registry.subscribers.delete(room);
    if (!registry.joined.delete(room)) return;
    if (socket.connected) socket.emit("leave_room", room);
  });
}

/**
 * Subscribes `handler` to `event` and keeps the socket in `room` while any subscriber needs it.
 * The room is joined when its first subscriber arrives (or on connect, while disconnected), and
 * again after every reconnect. Returns the unsubscribe function.
 */
export function subscribeToRoom<A extends unknown[]>(
  socket: RoomSocket,
  room: string,
  event: string,
  handler: (...args: A) => void,
): () => void {
  const registry = registryOf(socket);
  const listener = handler as (...args: unknown[]) => void;

  socket.on(event, listener);
  registry.subscribers.set(room, (registry.subscribers.get(room) ?? 0) + 1);
  if (socket.connected) join(socket, registry, room);

  let subscribed = true;
  return () => {
    if (!subscribed) return;
    subscribed = false;
    socket.off(event, listener);
    const count = (registry.subscribers.get(room) ?? 1) - 1;
    registry.subscribers.set(room, count);
    if (count === 0) leaveWhenUnused(socket, registry, room);
  };
}
