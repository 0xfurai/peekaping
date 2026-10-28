import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import EmptyList from "./empty-list";

const list = (extra: Partial<React.ComponentProps<typeof EmptyList>> = {}) =>
  renderToStaticMarkup(
    <EmptyList title="Nothing" text="Add one" actionText="Create" onClick={() => {}} {...extra} />
  );

describe("EmptyList", () => {
  it("has one button, as before, when it is given one action", () => {
    const html = list();
    expect(html.match(/<button/g)).toHaveLength(1);
    expect(html).toContain(">Create<");
  });

  it("has a second button beside the first when it is given a second action", () => {
    const html = list({ secondaryActionText: "Also this", onSecondaryClick: () => {} });
    expect(html.match(/<button/g)).toHaveLength(2);
    expect(html.indexOf(">Create<")).toBeLessThan(html.indexOf(">Also this<"));
  });
});
