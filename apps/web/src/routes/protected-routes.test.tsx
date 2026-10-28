import { describe, expect, it, vi } from "vitest";
import { Routes } from "react-router-dom";
import type { QueryClient } from "@tanstack/react-query";
import { getMeQueryKey, getMonitorsQueryKey } from "@/api/@tanstack/react-query.gen";
import { renderAs, text, WORKSPACE_ID } from "@/test-support/render-with-me";
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
  it.each([
    ["/probes", "/admin/probes"],
    ["/probes/22222222-2222-4222-8222-222222222222", "/admin/probes/22222222-2222-4222-8222-222222222222"],
  ])("sends an instance admin from the old workspace address %s to %s", (path, target) => {
    const html = at({ instanceAdmin: true, role: "owner" }, path);
    expect(html).toContain(`data-redirect="${target}"`);
  });

  it("shows an instance admin the admin area's pages", () => {
    for (const path of ["/admin/users", "/admin/probes", "/admin/probes/22222222-2222-4222-8222-222222222222"]) {
      const html = renderAs({ instanceAdmin: true, role: "owner" }, <Routes>{protectedRoutes}</Routes>, path);
      expect(html).not.toContain("data-redirect");
      expect(html).toContain(text("admin.nav.users"));
    }
  });

  it("opens the admin area on its Users page", () => {
    for (const path of ["/admin", "/admin/nothing-here"]) {
      const html = renderAs({ instanceAdmin: true, role: "owner" }, <Routes>{protectedRoutes}</Routes>, path);
      expect(html).toContain('data-redirect="/admin/users"');
    }
  });

  it("opens the admin area with no workspace's data in the cache, so that the way back to another workspace shows none of it", () => {
    let cache: QueryClient | undefined;
    renderAs({ instanceAdmin: true, role: "owner" }, <Routes>{protectedRoutes}</Routes>, "/admin/users", (queryClient) => {
      cache = queryClient;
      queryClient.setQueryData(getMonitorsQueryKey(), { data: [{ id: "m1" }] });
    });
    expect(cache?.getQueryData(getMonitorsQueryKey())).toBeUndefined();
    // Who is signed in is not a workspace's data
    expect(cache?.getQueryData(getMeQueryKey())).toBeDefined();
  });

  it.each(["/admin", "/admin/users", "/admin/probes", "/admin/probes/22222222-2222-4222-8222-222222222222"])(
    "sends a user who isn't an instance admin from %s to their workspace's monitors, showing nothing of the area",
    (path) => {
      for (const role of ["member", "owner"] as const) {
        const html = renderAs({ instanceAdmin: false, role }, <Routes>{protectedRoutes}</Routes>, path);
        expect(html).toContain(`data-redirect="/w/${WORKSPACE_ID}/monitors"`);
        expect(html).not.toContain(text("admin.nav.users"));
      }
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
