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
    expect(html).toContain("The Docker Container type is turned off on this install");
  });

  it("shows a monitor past its workspace's limit as paused, with the reason, even though it is switched on", () => {
    const html = renderAs(owner, <MonitorCard monitor={monitor({ type: "http", active: true, over_limit: true })} />);

    expect(html).toContain(text("monitors.view.status.paused"));
    expect(html).toContain('data-testid="monitor-over-limit"');
    expect(html).toContain(text("monitors.over_limit"));
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

  it("shows what an HTTP monitor checks, as text rather than a link, since the card opens the monitor", () => {
    const html = renderAs(
      owner,
      <MonitorCard monitor={monitor({ type: "http", config: JSON.stringify({ url: "https://example.com/health" }) })} />
    );

    expect(html).toMatch(/<p [^>]*data-testid="monitor-target"[^>]*>https:\/\/example.com\/health<\/p>/);
    expect(html).not.toContain("href=");
  });

  it("shows the host and port of a TCP monitor", () => {
    const html = renderAs(
      owner,
      <MonitorCard monitor={monitor({ type: "tcp", config: JSON.stringify({ host: "1.1.1.1", port: 443 }) })} />
    );

    expect(html).toContain(">1.1.1.1:443<");
  });

  it("shows the record and resolver of a DNS monitor", () => {
    const config = { host: "example.com", resolve_type: "A", resolver_server: "8.8.8.8", port: 53 };
    const html = renderAs(owner, <MonitorCard monitor={monitor({ type: "dns", config: JSON.stringify(config) })} />);

    expect(html).toContain(">example.com (A @ 8.8.8.8:53)<");
  });

  it("shows no target for a monitor that has none, such as a push monitor", () => {
    const html = renderAs(owner, <MonitorCard monitor={monitor({ type: "push", config: JSON.stringify({}) })} />);

    expect(html).not.toContain('data-testid="monitor-target"');
  });
});
