import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import enUS from "@/i18n/locales/en-US.json";
import HeartbeatTooltipBody from "./heartbeat-tooltip-body";

// The English text of a key, as the page shows it
const t = (key: string): string =>
  key.split(".").reduce<unknown>((node, part) => (node as Record<string, unknown>)?.[part], enUS) as string;

const heartbeat = {
  id: "hb-1",
  time: "2026-10-24T00:00:00Z",
  end_time: "2026-10-24T00:00:00Z",
  ping: 12,
  status: 1,
  msg: "ok",
};

const render = (instance: string | null) =>
  renderToStaticMarkup(
    <HeartbeatTooltipBody value={heartbeat} prev={null} timeText="24 Oct 2026" instance={instance} t={t} />
  );

describe("HeartbeatTooltipBody", () => {
  it("names the instance with its hostname", () => {
    const html = render("fra-1");
    expect(html).toContain("Instance:");
    expect(html).toContain("fra-1");
  });

  it("names a forgotten instance by the first 8 characters of its id", () => {
    const html = render("deadbeef");
    expect(html).toContain("Instance:");
    expect(html).toContain("deadbeef");
  });

  it("has no instance line for a heartbeat without one", () => {
    const html = render(null);
    expect(html).not.toContain("Instance");
    // The rest of the tooltip is still there
    expect(html).toContain("Ping:");
  });
});
