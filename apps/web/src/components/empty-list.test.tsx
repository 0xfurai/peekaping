import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { MemoryRouter } from "react-router-dom";
import EmptyList from "./empty-list";

const list = (extra: Partial<React.ComponentProps<typeof EmptyList>> = {}) =>
  renderToStaticMarkup(
    <MemoryRouter>
      <EmptyList title="Nothing" text="Add one" actionText="Create" onClick={() => {}} {...extra} />
    </MemoryRouter>
  );

describe("EmptyList", () => {
  it("has one button, as before, when it is given one action", () => {
    const html = list();
    expect(html.match(/<button/g)).toHaveLength(1);
    expect(html).toContain(">Create<");
  });

  it("has a link to a second page beside the button when it is given a second action", () => {
    const html = list({ secondaryActionText: "Also this", secondaryTo: "/elsewhere" });
    expect(html.match(/<button/g)).toHaveLength(1);
    expect(html).toMatch(/<a [^>]*href="\/elsewhere"[^>]*>Also this<\/a>/);
    expect(html.indexOf(">Create<")).toBeLessThan(html.indexOf(">Also this<"));
  });
});
