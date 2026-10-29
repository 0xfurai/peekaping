import { ArrowLeftIcon, Building2Icon, HelpCircleIcon, Radar, ShieldIcon, UsersIcon } from "lucide-react";
import { Link } from "react-router-dom";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar";
import { NavMain } from "./nav-main";
import { NavSecondary } from "./nav-secondary";
import { NavUser } from "./nav-user";
import { useAuthStore } from "@/store/auth";
import { useMe } from "@/hooks/useMe";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { inWorkspace } from "@/lib/workspace-path";
import { VERSION } from "../version";

// The sidebar of the admin area (/admin), which is outside every workspace: the install's own
// pages, and a way back to the user's workspace. Settings (PP-373) joins the list when its page
// exists.
export function AdminSidebar(props: React.ComponentProps<typeof Sidebar>) {
  const user = useAuthStore((state) => state.user);
  const { t } = useLocalizedTranslation();
  const me = useMe().data?.data;
  // The user's default workspace, which is where /w-less addresses lead
  const workspace = me?.workspaces?.find((w) => w.id === me.defaultWorkspaceId);

  const navMain = [
    { title: t("admin.nav.users"), url: "/admin/users", icon: UsersIcon, absolute: true },
    { title: t("admin.nav.workspaces"), url: "/admin/workspaces", icon: Building2Icon, absolute: true },
    { title: t("admin.nav.probes"), url: "/admin/probes", icon: Radar, absolute: true },
  ];
  const navSecondary = [
    {
      title: "Get Help",
      url: "https://docs.peekaping.com",
      icon: HelpCircleIcon,
      target: "_blank",
    },
  ];

  return (
    <Sidebar collapsible="offcanvas" {...props}>
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              asChild
              className="data-[slot=sidebar-menu-button]:!p-1.5"
            >
              <Link to="/admin">
                <ShieldIcon className="h-5 w-5" />
                <span className="text-base font-semibold">{t("admin.nav.title")}</span>
              </Link>
            </SidebarMenuButton>
          </SidebarMenuItem>
          {workspace && (
            <SidebarMenuItem>
              <SidebarMenuButton asChild>
                <Link to={inWorkspace(workspace.id, "/monitors")}>
                  <ArrowLeftIcon />
                  <span className="truncate">
                    {t("admin.nav.back_to_workspace", { name: workspace.name })}
                  </span>
                </Link>
              </SidebarMenuButton>
            </SidebarMenuItem>
          )}
        </SidebarMenu>
      </SidebarHeader>

      <SidebarContent>
        <NavMain items={navMain} />
        <NavSecondary items={navSecondary} className="mt-auto" />
        <div className="text-xs text-muted-foreground w-full mb-2 select-none px-4">
          <a
            href={`https://github.com/0xfurai/peekaping/releases/tag/${VERSION}`}
            target="_blank"
            rel="noopener noreferrer"
            className="hover:underline focus-visible:underline"
          >
            v{VERSION}
          </a>
        </div>
      </SidebarContent>

      <SidebarFooter>
        {user && <NavUser user={{ name: user.email!, email: user.email! }} />}
      </SidebarFooter>
    </Sidebar>
  );
}
