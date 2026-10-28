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
  it("has no Probes link for a user who isn't an instance admin", () => {
    const html = sidebar({ instanceAdmin: false, role: "member" });
    expect(html).not.toContain(`/w/${WORKSPACE_ID}/probes`);
    expect(html).not.toContain(text("navigation.probes"));
    // The rest of the navigation is still there
    expect(html).toContain(`/w/${WORKSPACE_ID}/monitors`);
    expect(html).toContain(`/w/${WORKSPACE_ID}/proxies`);
  });

  it("has no Probes link for a workspace owner who isn't an instance admin", () => {
    const html = sidebar({ instanceAdmin: false, role: "owner" });
    expect(html).not.toContain(`/w/${WORKSPACE_ID}/probes`);
  });

  it("links to Probes for an instance admin who owns the workspace", () => {
    const html = sidebar({ instanceAdmin: true, role: "owner" });
    expect(html).toContain(`/w/${WORKSPACE_ID}/probes`);
    expect(html).toContain(text("navigation.probes"));
  });
});
