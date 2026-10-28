import { describe, expect, it, vi } from "vitest";
import type { MonitorMonitorResponseDto } from "@/api";
import { renderAs, text } from "@/test-support/render-with-me";
import MonitorCard from "./monitor-card";

vi.mock("@/context/timezone-context", () => ({
  useTimezone: () => ({ timezone: "UTC", setTimezone: () => {} }),
}));

const owner = { instanceAdmin: false, role: "owner" } as const;

const monitor = (over: Partial<MonitorMonitorResponseDto>): MonitorMonitorResponseDto =>
  ({
    id: "m1",
    name: "Containers",
    type: "docker",
    interval: 60,
    active: true,
    type_disabled: false,
    ...over,
  }) as MonitorMonitorResponseDto;

describe("MonitorCard", () => {
  it("shows a monitor of a turned-off type as paused, with the reason, even though it is switched on", () => {
    const html = renderAs(owner, <MonitorCard monitor={monitor({ active: true, type_disabled: true })} />);

    expect(html).toContain(text("monitors.view.status.paused"));
    expect(html).toContain('data-testid="monitor-type-disabled"');
    expect(html).toContain("Docker Container monitors are turned off on this install");
  });

  it("shows no reason for a monitor that is paused by hand", () => {
    const html = renderAs(owner, <MonitorCard monitor={monitor({ active: false })} />);

    expect(html).toContain(text("monitors.view.status.paused"));
    expect(html).not.toContain('data-testid="monitor-type-disabled"');
  });

  it("shows a running monitor as it was, with no reason", () => {
    const html = renderAs(owner, <MonitorCard monitor={monitor({ type: "http" })} />);

    expect(html).not.toContain(text("monitors.view.status.paused"));
    expect(html).not.toContain('data-testid="monitor-type-disabled"');
  });
});
