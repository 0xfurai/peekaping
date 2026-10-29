import { describe, expect, it, vi } from "vitest";
import { Routes } from "react-router-dom";
import { renderAs, WORKSPACE_ID } from "@/test-support/render-with-me";
import { protectedRoutes } from "./protected-routes";

// The websocket context reads the page's config when it loads
vi.mock("@/lib/config", () => ({ getConfig: () => ({ API_URL: "" }) }));

// A probe page reads the browser's storage for the timezone, which a static render doesn't have
vi.mock("@/context/timezone-context", () => ({
  useTimezone: () => ({ timezone: "UTC", setTimezone: () => {} }),
}));

// A static render runs no effects, so a redirect would show nothing. Show where it goes.
vi.mock("react-router-dom", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react-router-dom")>()),
  Navigate: ({ to }: { to: string }) => <span data-redirect={to} />,
}));

const at = (access: Parameters<typeof renderAs>[0], path: string) =>
  renderAs(access, <Routes>{protectedRoutes}</Routes>, `/w/${WORKSPACE_ID}${path}`);

describe("protected routes", () => {
  it.each(["/probes", "/probes/22222222-2222-4222-8222-222222222222"])(
    "send a user who isn't an instance admin from %s to the monitors page",
    (path) => {
      const html = at({ instanceAdmin: false, role: "owner" }, path);
      expect(html).toContain(`data-redirect="/w/${WORKSPACE_ID}/monitors"`);
    }
  );

  it.each(["/probes", "/probes/22222222-2222-4222-8222-222222222222"])(
    "show an instance admin %s",
    (path) => {
      const html = at({ instanceAdmin: true, role: "owner" }, path);
      expect(html).not.toContain("data-redirect");
    }
  );

  it("sends a user who just signed in from an invite back to it", () => {
    const html = renderAs(
      { instanceAdmin: false, role: "member" },
      <Routes>{protectedRoutes}</Routes>,
      "/login?next=%2Finvite%2Ftok_abc"
    );
    expect(html).toContain('data-redirect="/invite/tok_abc"');
  });

  it("sends a user who just signed in, with no or a foreign next address, to their workspace", () => {
    for (const search of ["", "?next=https%3A%2F%2Fevil.example%2F", "?next=%2Fmonitors"]) {
      const html = renderAs(
        { instanceAdmin: false, role: "member" },
        <Routes>{protectedRoutes}</Routes>,
        `/login${search}`
      );
      // The workspace's own address, which goes on to its monitors
      expect(html).toContain(`data-redirect="/w/${WORKSPACE_ID}`);
    }
  });
});
