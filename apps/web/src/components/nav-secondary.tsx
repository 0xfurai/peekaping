"use client";

import * as React from "react";
import { type LucideIcon } from "lucide-react";

import {
    SidebarGroup,
    SidebarGroupContent,
    SidebarMenu,
    SidebarMenuButton,
    SidebarMenuItem,
    SidebarSeparator,
} from "@/components/ui/sidebar";
import { ModeToggle } from "./mode-toggle";
import { Link } from "react-router-dom";
import { LanguageSelector } from "./LanguageSelector";

export function NavSecondary({
    items,
    children,
    ...props
}: {
    items: {
        title: string;
        url: string;
        icon: LucideIcon;
        target?: string;
    }[];
    children?: React.ReactNode;
} & React.ComponentPropsWithoutRef<typeof SidebarGroup>) {
    return (
        <SidebarGroup {...props}>
            <SidebarGroupContent>
                <SidebarMenu>
                    <SidebarMenuItem>
                        <SidebarMenuButton asChild>
                            <LanguageSelector />
                        </SidebarMenuButton>
                    </SidebarMenuItem>

                    {items.map((item) => (
                        <SidebarMenuItem key={item.title}>
                            <SidebarMenuButton asChild>
                                <Link to={item.url} target={item.target}>
                                    {item.icon && <item.icon />}
                                    <span>{item.title}</span>
                                </Link>
                            </SidebarMenuButton>
                        </SidebarMenuItem>
                    ))}

                    <ModeToggle />
                    {children && (
                        <>
                            <SidebarSeparator className="my-2" />
                            {children}
                        </>
                    )}
                </SidebarMenu>
            </SidebarGroupContent>
        </SidebarGroup>
    );
}
