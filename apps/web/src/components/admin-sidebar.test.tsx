import { describe, expect, it } from "vitest";
import { SidebarProvider } from "@/components/ui/sidebar";
import { renderAs, text, WORKSPACE_ID } from "@/test-support/render-with-me";
import { AdminSidebar } from "./admin-sidebar";

const sidebar = (access: Parameters<typeof renderAs>[0]) =>
  renderAs(
    access,
    <SidebarProvider>
      <AdminSidebar />
    </SidebarProvider>,
    "/admin/users"
  );

describe("AdminSidebar", () => {
  it("links Users and Probes in the admin area", () => {
    const html = sidebar({ instanceAdmin: true, role: "owner" });
    expect(html).toContain('href="/admin/users"');
    expect(html).toContain('href="/admin/probes"');
    expect(html).toContain(text("admin.nav.users"));
    expect(html).toContain(text("admin.nav.probes"));
  });

  it("links the install settings last", () => {
    const html = sidebar({ instanceAdmin: true, role: "owner" });
    expect(html).toContain('href="/admin/settings"');
    expect(html).toContain(text("admin.nav.settings"));
    const links = [...html.matchAll(/href="(\/admin\/[^"]*)"/g)].map((match) => match[1]);
    expect(links.at(-1)).toBe("/admin/settings");
  });

  it("has a way back to the user's workspace, named", () => {
    const html = sidebar({ instanceAdmin: true, role: "owner" });
    expect(html).toContain(`href="/w/${WORKSPACE_ID}/monitors"`);
    expect(html).toContain("Back to Main");
  });

  it("holds no workspace's pages, so nothing there leans on a workspace in the address", () => {
    const html = sidebar({ instanceAdmin: true, role: "owner" });
    expect(html).not.toContain("/proxies");
    expect(html).not.toContain("/status-pages");
  });
});
