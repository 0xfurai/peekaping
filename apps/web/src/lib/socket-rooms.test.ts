import { describe, expect, it } from "vitest";
import {
  heartbeatEvent,
  monitorRoom,
  subscribeToRoom,
  workspaceRoom,
  type RoomSocket,
} from "./socket-rooms";

// A socket that records what the client sends and lets a test play the server's side:
// connect() and disconnect() fire the "connect" listeners the way socket.io-client does.
class FakeSocket implements RoomSocket {
  connected: boolean;
  sent: string[] = [];
  private listeners = new Map<string, Set<(...args: unknown[]) => void>>();

  constructor(connected: boolean) {
    this.connected = connected;
  }

  emit(event: string, ...args: unknown[]) {
    this.sent.push(`${event} ${args.join(" ")}`);
    return this;
  }

  on(event: string, listener: (...args: unknown[]) => void) {
    if (!this.listeners.has(event)) this.listeners.set(event, new Set());
    this.listeners.get(event)!.add(listener);
    return this;
  }

  off(event: string, listener: (...args: unknown[]) => void) {
    this.listeners.get(event)?.delete(listener);
    return this;
  }

  receive(event: string, ...args: unknown[]) {
    for (const listener of [...(this.listeners.get(event) ?? [])]) listener(...args);
  }

  connect() {
    this.connected = true;
    this.receive("connect");
  }

  disconnect() {
    this.connected = false;
    this.receive("disconnect");
  }
}

// Lets the microtask that sends a deferred leave run.
const nextTick = () => Promise.resolve();

describe("subscribeToRoom", () => {
  it("joins the room when the first subscriber arrives and delivers its events", () => {
    const socket = new FakeSocket(true);
    const received: unknown[] = [];

    subscribeToRoom(socket, "monitor:1", "monitor:1:heartbeat", (hb) => received.push(hb));
    socket.receive("monitor:1:heartbeat", { id: "a" });

    expect(socket.sent).toEqual(["join_room monitor:1"]);
    expect(received).toEqual([{ id: "a" }]);
  });

  it("joins a room once for several subscribers and leaves after the last one goes", async () => {
    const socket = new FakeSocket(true);
    const first = subscribeToRoom(socket, "monitor:1", "monitor:1:heartbeat", () => {});
    const second = subscribeToRoom(socket, "monitor:1", "monitor:1:heartbeat", () => {});

    first();
    await nextTick();
    expect(socket.sent).toEqual(["join_room monitor:1"]);

    second();
    expect(socket.sent).toEqual(["join_room monitor:1"]);
    await nextTick();
    expect(socket.sent).toEqual(["join_room monitor:1", "leave_room monitor:1"]);
  });

  it("sends no leave_room and no second join_room when a subscriber is replaced in the same tick", async () => {
    const socket = new FakeSocket(true);
    const received: string[] = [];
    const unsubscribe = subscribeToRoom(socket, "monitor:1", "monitor:1:heartbeat", () => received.push("old"));

    // What React does when an effect runs again: the cleanup, then the next run, synchronously.
    unsubscribe();
    subscribeToRoom(socket, "monitor:1", "monitor:1:heartbeat", () => received.push("new"));
    await nextTick();

    expect(socket.sent).toEqual(["join_room monitor:1"]);
    socket.receive("monitor:1:heartbeat");
    expect(received).toEqual(["new"]);
  });

  it("stops delivering events to an unsubscribed handler", () => {
    const socket = new FakeSocket(true);
    const received: unknown[] = [];
    const unsubscribe = subscribeToRoom(socket, "monitor:1", "monitor:1:heartbeat", (hb) => received.push(hb));

    unsubscribe();
    socket.receive("monitor:1:heartbeat", { id: "a" });

    expect(received).toEqual([]);
  });

  it("keeps the room for a subscriber that stays when another leaves", async () => {
    const socket = new FakeSocket(true);
    const leaving = subscribeToRoom(socket, "monitor:1", "monitor:1:heartbeat", () => {});
    subscribeToRoom(socket, "monitor:1", "monitor:1:heartbeat", () => {});

    leaving();
    await nextTick();

    expect(socket.sent).toEqual(["join_room monitor:1"]);
  });

  it("joins a room that was left and subscribed again after the leave went out", async () => {
    const socket = new FakeSocket(true);
    subscribeToRoom(socket, "monitor:1", "monitor:1:heartbeat", () => {})();
    await nextTick();
    subscribeToRoom(socket, "monitor:1", "monitor:1:heartbeat", () => {});

    expect(socket.sent).toEqual(["join_room monitor:1", "leave_room monitor:1", "join_room monitor:1"]);
  });

  it("sends nothing while disconnected and joins every subscribed room on connect", () => {
    const socket = new FakeSocket(false);
    subscribeToRoom(socket, "monitor:all", "monitor:all:heartbeat", () => {});
    subscribeToRoom(socket, "monitor:1", "monitor:1:heartbeat", () => {});
    expect(socket.sent).toEqual([]);

    socket.connect();

    expect(socket.sent).toEqual(["join_room monitor:all", "join_room monitor:1"]);
  });

  it("joins every subscribed room again after a reconnect", () => {
    const socket = new FakeSocket(true);
    subscribeToRoom(socket, "monitor:all", "monitor:all:heartbeat", () => {});
    subscribeToRoom(socket, "monitor:1", "monitor:1:heartbeat", () => {});
    socket.sent.length = 0;

    socket.disconnect();
    socket.connect();

    expect(socket.sent).toEqual(["join_room monitor:all", "join_room monitor:1"]);
  });

  it("does not join a room again after a reconnect once its last subscriber is gone", async () => {
    const socket = new FakeSocket(true);
    const unsubscribe = subscribeToRoom(socket, "monitor:1", "monitor:1:heartbeat", () => {});
    unsubscribe();
    await nextTick();
    socket.sent.length = 0;

    socket.disconnect();
    socket.connect();

    expect(socket.sent).toEqual([]);
  });

  it("sends no leave_room when the socket dropped before the deferred leave ran", async () => {
    const socket = new FakeSocket(true);
    const unsubscribe = subscribeToRoom(socket, "monitor:1", "monitor:1:heartbeat", () => {});
    socket.sent.length = 0;

    unsubscribe();
    socket.disconnect();
    await nextTick();

    expect(socket.sent).toEqual([]);
  });

  it("keeps a separate registry for each socket", () => {
    const first = new FakeSocket(true);
    const second = new FakeSocket(true);

    subscribeToRoom(first, "monitor:1", "monitor:1:heartbeat", () => {});
    subscribeToRoom(second, "monitor:1", "monitor:1:heartbeat", () => {});

    expect(first.sent).toEqual(["join_room monitor:1"]);
    expect(second.sent).toEqual(["join_room monitor:1"]);
  });
});

describe("room names", () => {
  const workspaceId = "0d6f1e0e-6b1c-4d49-8c37-0b5f4a1b3a01";

  it("names the room of a workspace and of a monitor the way the server does", () => {
    expect(workspaceRoom(workspaceId)).toBe(`workspace:${workspaceId}`);
    expect(monitorRoom("m-1")).toBe("monitor:m-1");
  });

  it("names the event a room delivers its heartbeats as", () => {
    expect(heartbeatEvent(workspaceRoom(workspaceId))).toBe(`workspace:${workspaceId}:heartbeat`);
    expect(heartbeatEvent(monitorRoom("m-1"))).toBe("monitor:m-1:heartbeat");
  });
});

describe("a workspace's room", () => {
  const first = workspaceRoom("0d6f1e0e-6b1c-4d49-8c37-0b5f4a1b3a01");
  const second = workspaceRoom("6a1c9d52-3b7e-4f0a-9d1e-5c2b8e7f4a02");

  it("is left, and the next one joined, when the address moves to another workspace", async () => {
    const socket = new FakeSocket(true);
    const received: unknown[] = [];

    const unsubscribe = subscribeToRoom(socket, first, heartbeatEvent(first), (hb) => received.push(hb));
    // The effect's cleanup and its next run happen in the same commit
    unsubscribe();
    subscribeToRoom(socket, second, heartbeatEvent(second), (hb) => received.push(hb));
    await nextTick();

    expect(socket.sent).toEqual([`join_room ${first}`, `join_room ${second}`, `leave_room ${first}`]);
    socket.receive(heartbeatEvent(first), { id: "stale" });
    socket.receive(heartbeatEvent(second), { id: "fresh" });
    expect(received).toEqual([{ id: "fresh" }]);
  });

  it("is joined again after a reconnect", () => {
    const socket = new FakeSocket(true);
    subscribeToRoom(socket, first, heartbeatEvent(first), () => {});

    socket.disconnect();
    socket.connect();

    expect(socket.sent).toEqual([`join_room ${first}`, `join_room ${first}`]);
  });
});
