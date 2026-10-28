import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import "@/i18n";
import { text } from "@/test-support/render-with-me";
import { DefaultProbeCard } from "./default-probe-switch";

const card = (checked: boolean) =>
  renderToStaticMarkup(<DefaultProbeCard checked={checked} onChange={() => {}} />);

describe("DefaultProbeCard", () => {
  it("explains what a default probe is and shows the switch on for a default probe", () => {
    const html = card(true);
    expect(html).toContain(text("probes.default.title"));
    expect(html).toContain(text("probes.default.label"));
    expect(html).toMatch(/<button[^>]*role="switch"[^>]*aria-checked="true"/);
  });

  it("shows the switch off for a probe that isn't", () => {
    expect(card(false)).toMatch(/<button[^>]*role="switch"[^>]*aria-checked="false"/);
  });
});
