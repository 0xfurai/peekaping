import { describe, expect, it, vi } from "vitest";
import { Route, Routes } from "react-router-dom";
import type { MonitorMonitorResponseDto } from "@/api";
import { getMonitorsByIdQueryKey } from "@/api/@tanstack/react-query.gen";
import { renderAs, text, WORKSPACE_ID } from "@/test-support/render-with-me";
import MonitorPage from "./page";

vi.mock("@/context/timezone-context", () => ({
  useTimezone: () => ({ timezone: "UTC", setTimezone: () => {} }),
}));
vi.mock("@/context/websocket-context", () => ({
  useSocketRoom: () => {},
}));

const MONITOR_ID = "22222222-2222-4222-8222-222222222222";

const monitor = (over: Partial<MonitorMonitorResponseDto>): MonitorMonitorResponseDto =>
  ({
    id: MONITOR_ID,
    name: "Containers",
    type: "docker",
    interval: 60,
    timeout: 16,
    active: true,
    type_disabled: false,
    config: "{}",
    probe_ids: ["local"],
    locations: [],
    notification_ids: [],
    tag_ids: [],
    ...over,
  }) as MonitorMonitorResponseDto;

const page = (saved: MonitorMonitorResponseDto) =>
  renderAs(
    { instanceAdmin: false, role: "owner" },
    <Routes>
      <Route path="/w/:workspaceId/monitors/:id" element={<MonitorPage />} />
    </Routes>,
    `/w/${WORKSPACE_ID}/monitors/${MONITOR_ID}`,
    (queryClient) => {
      queryClient.setQueryData(getMonitorsByIdQueryKey({ path: { id: MONITOR_ID } }), { data: saved });
    }
  );

// The button that pauses or resumes, as the markup has it
const toggle = (html: string) => html.match(/<button[^>]*data-testid="monitor-pause-toggle"[^>]*>.*?<\/button>/)?.[0] ?? "";
// The button that clones, as the markup has it: the one that holds the Clone label
const clone = (html: string) =>
  [...html.matchAll(/<button[^>]*>.*?<\/button>/g)].map((m) => m[0]).find((b) => b.includes(text("monitors.view.buttons.clone"))) ?? "";
// The tag carries the attribute `disabled=""`; its classes mention `disabled:` too, so look for the attribute
const isOff = (tag: string) => tag.includes(' disabled=""');

describe("the monitor page", () => {
  it("shows a monitor of a turned-off type as paused, with the reason, and turns Resume off", () => {
    const html = page(monitor({ active: true, type_disabled: true }));

    expect(html).toContain(`>${text("monitors.view.status.paused")}<`);
    expect(html).toContain('data-testid="monitor-type-disabled"');
    expect(html).toContain("The Docker Container type is turned off on this install");
    expect(toggle(html)).toContain(text("monitors.view.buttons.resume"));
    expect(isOff(toggle(html))).toBe(true);
    expect(toggle(html)).toContain('title="The Docker Container type is turned off on this install"');
  });

  it("turns Resume off for a turned-off type that was also paused by hand", () => {
    const html = page(monitor({ active: false, type_disabled: true }));

    expect(toggle(html)).toContain(text("monitors.view.buttons.resume"));
    expect(isOff(toggle(html))).toBe(true);
  });

  it("turns Clone off for a monitor of a turned-off type, with the reason, since the copy would have no type", () => {
    const html = page(monitor({ active: true, type_disabled: true }));

    expect(isOff(clone(html))).toBe(true);
    expect(clone(html)).toContain('title="The Docker Container type is turned off on this install"');
  });

  it("leaves Clone on for a monitor of a type that is on", () => {
    const html = page(monitor({ type: "http" }));

    expect(clone(html)).not.toBe("");
    expect(isOff(clone(html))).toBe(false);
  });

  it("shows a monitor past its workspace's limit as paused, with the reason, and leaves Pause on to choose which stay", () => {
    const html = page(monitor({ type: "http", active: true, over_limit: true }));

    expect(html).toContain(`>${text("monitors.view.status.paused")}<`);
    expect(html).toContain('data-testid="monitor-over-limit"');
    expect(html).toContain(text("monitors.over_limit"));
    expect(toggle(html)).toContain(text("monitors.view.buttons.pause"));
    expect(isOff(toggle(html))).toBe(false);
  });

  it("leaves Pause on for a running monitor", () => {
    const html = page(monitor({ type: "http", active: true }));

    expect(toggle(html)).toContain(text("monitors.view.buttons.pause"));
    expect(html).not.toContain('data-testid="monitor-type-disabled"');
    expect(isOff(toggle(html))).toBe(false);
  });

  it("leaves Resume on for a monitor paused by hand", () => {
    const html = page(monitor({ type: "http", active: false }));

    expect(toggle(html)).toContain(text("monitors.view.buttons.resume"));
    expect(html).not.toContain('data-testid="monitor-type-disabled"');
    expect(isOff(toggle(html))).toBe(false);
  });
});
