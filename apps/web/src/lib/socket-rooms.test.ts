import { describe, expect, it } from "vitest";
import { subscribeToRoom, type RoomSocket } from "./socket-rooms";

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
