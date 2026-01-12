"use client";

import { LogOutIcon, MoreVerticalIcon, SettingsIcon, ShieldCheckIcon, MailIcon } from "lucide-react";

import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuGroup,
    DropdownMenuItem,
    DropdownMenuLabel,
    DropdownMenuSeparator,
    DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
    SidebarMenu,
    SidebarMenuButton,
    SidebarMenuItem,
    useSidebar,
} from "@/components/ui/sidebar";
import { useAuthStore } from "@/store/auth";
import { useNavigate } from "react-router-dom";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useQuery } from "@tanstack/react-query";
import { getUserInvitations } from "@/api/sdk.gen";

export function NavUser({
    user,
}: {
    user: {
        name: string;
        email: string;
    };
}) {
    const { isMobile } = useSidebar();
    const clearTokens = useAuthStore((state) => state.clearTokens);
    const navigate = useNavigate();
    const { t } = useLocalizedTranslation();
    const { accessToken } = useAuthStore();

    const { data: invitationsResponse } = useQuery({
        queryKey: ["user-invitations"],
        queryFn: () => getUserInvitations(),
        enabled: !!accessToken,
        // Don't refetch too aggressively in sidebar
        staleTime: 60 * 1000,
    });

    const invitationsCount = (invitationsResponse?.data?.data as any[])?.length || 0;

    const handleLogout = () => {
        clearTokens();
        navigate("/login");
    };

    const initial = user.email.charAt(0).toUpperCase();

    return (
        <SidebarMenu>
            <SidebarMenuItem>
                <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                        <SidebarMenuButton
                            size="lg"
                            className="data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground"
                        >
                            <Avatar className="h-8 w-8 rounded-lg grayscale relative">
                                {/* <AvatarImage src={user.avatar} alt={user.name} /> */}
                                <AvatarFallback className="rounded-lg">
                                    {initial}
                                </AvatarFallback>
                                {invitationsCount > 0 && (
                                    <span className="absolute -top-1 -right-1 flex h-3 w-3 rounded-full bg-red-500 border-2 border-sidebar-accent" />
                                )}
                            </Avatar>
                            <div className="grid flex-1 text-left text-sm leading-tight">
                                <span className="truncate font-medium">{user.name}</span>
                                <span className="truncate text-xs text-muted-foreground">
                                    {user.email}
                                </span>
                            </div>
                            {invitationsCount > 0 && (
                                <span className="ml-auto flex h-5 w-5 items-center justify-center rounded-full bg-red-500 text-[10px] text-white font-bold mr-2">
                                    {invitationsCount}
                                </span>
                            )}
                            <MoreVerticalIcon className="ml-auto size-4" />
                        </SidebarMenuButton>
                    </DropdownMenuTrigger>

                    <DropdownMenuContent
                        className="w-[--radix-dropdown-menu-trigger-width] min-w-56 rounded-lg"
                        side={isMobile ? "bottom" : "right"}
                        align="end"
                        sideOffset={4}
                    >
                        <DropdownMenuLabel className="p-0 font-normal">
                            <div className="flex items-center gap-2 px-1 py-1.5 text-left text-sm">
                                <Avatar className="h-8 w-8 rounded-lg">
                                    <AvatarFallback className="rounded-lg">
                                        {initial}
                                    </AvatarFallback>
                                </Avatar>
                                <div className="grid flex-1 text-left text-sm leading-tight">
                                    <span className="truncate font-medium">{user.name}</span>
                                    <span className="truncate text-xs text-muted-foreground">
                                        {user.email}
                                    </span>
                                </div>
                            </div>
                        </DropdownMenuLabel>

                        <DropdownMenuSeparator />

                        <DropdownMenuGroup>
                            <DropdownMenuItem onClick={() => navigate("/account/security")}>
                                <ShieldCheckIcon />
                                {t("common.security")}
                            </DropdownMenuItem>

                            <DropdownMenuItem onClick={() => navigate("/account/settings")}>
                                <SettingsIcon />
                                {t("common.settings")}
                            </DropdownMenuItem>
                            <DropdownMenuItem onClick={() => navigate("/account/invitations")}>
                                <MailIcon />
                                Invitations
                                {invitationsCount > 0 && (
                                    <span className="ml-auto flex h-5 w-5 items-center justify-center rounded-full bg-red-500 text-[10px] text-white font-bold">
                                        {invitationsCount}
                                    </span>
                                )}
                            </DropdownMenuItem>
                        </DropdownMenuGroup>

                        <DropdownMenuSeparator />

                        <DropdownMenuItem onClick={handleLogout}>
                            <LogOutIcon />
                            {t("common.logout")}
                        </DropdownMenuItem>
                    </DropdownMenuContent>
                </DropdownMenu>
            </SidebarMenuItem>
        </SidebarMenu>
    );
}
