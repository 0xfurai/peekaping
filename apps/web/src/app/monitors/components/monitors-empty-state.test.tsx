import { describe, expect, it } from "vitest";
import { renderAs, text, WORKSPACE_ID } from "@/test-support/render-with-me";
import { MonitorsEmptyState } from "./monitors-empty-state";

describe("MonitorsEmptyState", () => {
  it("offers to create the first monitor and to add a notification channel", () => {
    const html = renderAs({ instanceAdmin: false, role: "owner" }, <MonitorsEmptyState />);
    expect(html).toContain(`>${text("monitors.empty_state.action")}<`);
    expect(html).toContain(`>${text("monitors.empty_state.add_channel")}<`);
    expect(html).toContain("Create your first monitor");
    expect(html).toContain("Add a notification channel");
  });

  it("links to the new channel page, so that it can be opened in a tab of its own", () => {
    const html = renderAs({ instanceAdmin: false, role: "owner" }, <MonitorsEmptyState />);
    expect(html).toMatch(
      new RegExp(`<a [^>]*href="/w/${WORKSPACE_ID}/notification-channels/new"[^>]*>${text("monitors.empty_state.add_channel")}</a>`)
    );
  });
});
