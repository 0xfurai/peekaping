import { describe, expect, it } from "vitest";
import { SidebarProvider } from "@/components/ui/sidebar";
import { renderAs, text, WORKSPACE_ID } from "@/test-support/render-with-me";
import { AppSidebar } from "./app-sidebar";

const sidebar = (access: Parameters<typeof renderAs>[0]) =>
  renderAs(
    access,
    <SidebarProvider>
      <AppSidebar />
    </SidebarProvider>
  );

describe("AppSidebar", () => {
  it("has no Admin entry, and no Probes link, for a user who isn't an instance admin", () => {
    const html = sidebar({ instanceAdmin: false, role: "member" });
    expect(html).not.toContain('href="/admin');
    expect(html).not.toContain(text("navigation.admin"));
    expect(html).not.toContain("/probes");
    expect(html).not.toContain(text("navigation.probes"));
    // The rest of the navigation is still there
    expect(html).toContain(`/w/${WORKSPACE_ID}/monitors`);
    expect(html).toContain(`/w/${WORKSPACE_ID}/proxies`);
  });

  it("has no Admin entry for a workspace owner who isn't an instance admin", () => {
    const html = sidebar({ instanceAdmin: false, role: "owner" });
    expect(html).not.toContain('href="/admin');
  });

  it("links an instance admin to the admin area, outside the workspace, in place of a Probes link", () => {
    const html = sidebar({ instanceAdmin: true, role: "owner" });
    expect(html).toContain('href="/admin"');
    expect(html).toContain(text("navigation.admin"));
    expect(html).not.toContain(`/w/${WORKSPACE_ID}/admin`);
    expect(html).not.toContain(`/w/${WORKSPACE_ID}/probes`);
  });
});

describe("AppSidebar workspace switcher", () => {
  const OTHER = { id: "22222222-2222-4222-8222-222222222222", name: "Side project", role: "admin" as const };

  it("is not there for a user with one workspace, so the sidebar looks as it did", () => {
    const html = sidebar({ instanceAdmin: false, role: "owner" });
    expect(html).not.toContain(text("workspaces.switcher_label"));
    expect(html).not.toContain(">Main<");
  });

  it("is there for a user with two workspaces, showing the current one", () => {
    const html = sidebar({ instanceAdmin: false, role: "owner", otherWorkspaces: [OTHER] });
    expect(html).toContain(`aria-label="${text("workspaces.switcher_label")}"`);
    expect(html).toContain(">Main<");
  });
});
