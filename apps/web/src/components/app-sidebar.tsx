import {
    Home,
    Network,
    HelpCircleIcon,
    SettingsIcon,
    Vibrate,
    ListCheckIcon,
    Tag,
    Users,
    Building2,
} from "lucide-react";

import {
    Sidebar,
    SidebarContent,
    SidebarFooter,
    SidebarHeader,
    SidebarMenuButton,
    SidebarMenuItem,
} from "@/components/ui/sidebar";
import { Link } from "react-router-dom";
import { NavUser } from "./nav-user";
import { NavMain } from "./nav-main";
import { NavSecondary } from "./nav-secondary";
import { useAuthStore } from "@/store/auth";
import { VERSION } from "../version";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

import { OrganizationSwitcher } from "./organization-switcher";
import { useOrganizationStore } from "@/store/organization";

export function AppSidebar(props: React.ComponentProps<typeof Sidebar>) {
    const user = useAuthStore((state) => state.user);
    const { currentOrganization, organizations } = useOrganizationStore();
    const { t } = useLocalizedTranslation();

    const slug = currentOrganization?.slug || organizations?.[0]?.organization?.slug;
    const prefix = slug ? `/${slug}` : "";

    const data = {
        user: {
            name: "shadcn",
            email: "m@example.com",
            avatar: "/avatars/shadcn.jpg",
        },
        navMain: [
            {
                title: t("navigation.monitors"),
                url: `${prefix}/monitors`,
                icon: Home,
            },
            {
                title: t("navigation.maintenance"),
                url: `${prefix}/maintenances`,
                icon: SettingsIcon,
            },
            {
                title: t("navigation.status_pages"),
                url: `${prefix}/status-pages`,
                icon: ListCheckIcon,
            },
            {
                title: "Tags",
                url: `${prefix}/tags`,
                icon: Tag,
            },
            {
                title: t("navigation.proxies"),
                url: `${prefix}/proxies`,
                icon: Network,
            },
            {
                title: t("navigation.notification_channels"),
                url: `${prefix}/notification-channels`,
                icon: Vibrate,
            },
        ],
        navSecondary: [
            {
                title: "Get Help",
                url: "https://docs.vigi.run",
                icon: HelpCircleIcon,
                target: "_blank",
            },
        ],
    };

    const menuItems = [
        {
            title: t("navigation.members"),
            url: `${prefix}/settings/members`,
            icon: Users,
        },
        {
            title: t("navigation.organization_settings"),
            url: `${prefix}/settings/organization`,
            icon: Building2,
        },
    ]

    return (
        <Sidebar collapsible="offcanvas" {...props}>
            <SidebarHeader>
                <OrganizationSwitcher />
            </SidebarHeader>

            <SidebarContent>
                <NavMain items={data.navMain} />
                <NavSecondary items={data.navSecondary} className="mt-auto">
                    {menuItems.map((item) => (
                        <SidebarMenuItem key={item.title}>
                            <SidebarMenuButton asChild>
                                <Link to={item.url}>
                                    {item.icon && <item.icon />}
                                    <span>{item.title}</span>
                                </Link>
                            </SidebarMenuButton>
                        </SidebarMenuItem>
                    ))}
                </NavSecondary>
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
