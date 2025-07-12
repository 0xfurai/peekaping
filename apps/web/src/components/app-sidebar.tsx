import {
  Home,
  Network,
  // Vibrate,
  ArrowUpCircleIcon,
  HelpCircleIcon,
  SettingsIcon,
  Vibrate,
  ListCheckIcon,
  Tag,
} from "lucide-react";

import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar";
import { NavUser } from "./nav-user";
import { NavMain } from "./nav-main";
import { NavSecondary } from "./nav-secondary";
import { useAuthStore } from "@/store/auth";
import { VERSION } from "../version";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

export function AppSidebar(props: React.ComponentProps<typeof Sidebar>) {
  const user = useAuthStore((state) => state.user);
  const { t } = useLocalizedTranslation();

  const data = {
    user: {
      name: "shadcn",
      email: "m@example.com",
      avatar: "/avatars/shadcn.jpg",
    },
    navMain: [
      {
        title: t("navigation.monitors"),
        url: "/monitors",
        icon: Home,
      },
      {
        title: t("navigation.maintenance"),
        url: "/maintenances",
        icon: SettingsIcon,
      },
      {
        title: t("navigation.status_pages"),
        url: "/status-pages",
        icon: ListCheckIcon,
      },
      {
        title: "Tags",
        url: "/tags",
        icon: Tag,
      },
      {
        title: t("navigation.proxies"),
        url: "/proxies",
        icon: Network,
      },
      {
        title: t("navigation.notification_channels"),
        url: "/notification-channels",
        icon: Vibrate,
      },
    ],
    navSecondary: [
      {
        title: "Get Help",
        url: "https://docs.peekaping.com",
        icon: HelpCircleIcon,
        target: "_blank",
      },
    ],
  };

  return (
    <Sidebar collapsible="offcanvas" {...props}>
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              asChild
              className="data-[slot=sidebar-menu-button]:!p-1.5"
            >
              <a href="/">
                <ArrowUpCircleIcon className="h-5 w-5" />
                <span className="text-base font-semibold">Peekaping</span>
              </a>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>

      <SidebarContent>
        <NavMain items={data.navMain} />
        <NavSecondary items={data.navSecondary} className="mt-auto" />
        <div className="text-xs text-muted-foreground w-full mb-2 select-none px-4">
          v{VERSION}
        </div>
      </SidebarContent>

      <SidebarFooter>
        {user && (
          <NavUser
            user={{
              name: user.email!,
              email: user.email!,
            }}
          />
        )}
      </SidebarFooter>
    </Sidebar>
  );
}
