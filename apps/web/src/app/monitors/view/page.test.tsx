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
// The tag carries the attribute `disabled=""`; its classes mention `disabled:` too, so look for the attribute
const isOff = (tag: string) => tag.includes(' disabled=""');

describe("the monitor page", () => {
  it("shows a monitor of a turned-off type as paused, with the reason, and turns Resume off", () => {
    const html = page(monitor({ active: true, type_disabled: true }));

    expect(html).toContain(`>${text("monitors.view.status.paused")}<`);
    expect(html).toContain('data-testid="monitor-type-disabled"');
    expect(html).toContain("Docker Container monitors are turned off on this install");
    expect(toggle(html)).toContain(text("monitors.view.buttons.resume"));
    expect(isOff(toggle(html))).toBe(true);
    expect(toggle(html)).toContain('title="Docker Container monitors are turned off on this install"');
  });

  it("turns Resume off for a turned-off type that was also paused by hand", () => {
    const html = page(monitor({ active: false, type_disabled: true }));

    expect(toggle(html)).toContain(text("monitors.view.buttons.resume"));
    expect(isOff(toggle(html))).toBe(true);
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
