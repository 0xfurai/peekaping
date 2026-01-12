import { ChevronsUpDown, Check, Plus } from "lucide-react"

import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuTrigger,
    DropdownMenuLabel,
    DropdownMenuSeparator,
} from "@/components/ui/dropdown-menu"
import {
    SidebarMenu,
    SidebarMenuButton,
    SidebarMenuItem,
    useSidebar,
} from "@/components/ui/sidebar"
import { useOrganizationStore } from "@/store/organization"
import { useNavigate } from "react-router-dom"
import { useLocalizedTranslation } from "@/hooks/useTranslation"


export function OrganizationSwitcher() {
    const { isMobile } = useSidebar()
    const { currentOrganization, organizations } = useOrganizationStore()
    const navigate = useNavigate()
    const { t } = useLocalizedTranslation()

    // Find the active organization object from the list to get any extra details if needed, 
    // or just use currentOrganization.
    // We expect 'organizations' to be populated.

    const handleOrgChange = (slug: string) => {
        navigate(`/${slug}/monitors`)
    }

    const handleCreateOrg = () => {
        navigate("/create-organization")
    }

    if (!currentOrganization) return null

    return (
        <SidebarMenu>
            <SidebarMenuItem>
                <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                        <SidebarMenuButton
                            size="lg"
                            className="data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground"
                        >
                            <div className="flex aspect-square size-8 items-center justify-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground">
                                {/* Replaced Logo with Initials or Icon */}
                                <span className="font-bold">{currentOrganization.name?.substring(0, 1).toUpperCase()}</span>
                            </div>
                            <div className="grid flex-1 text-left text-sm leading-tight">
                                <span className="truncate font-semibold">
                                    {currentOrganization.name}
                                </span>
                                <span className="truncate text-xs">
                                    {/* Plan or Role could go here */}
                                    {t("organization.switcher.label")}
                                </span>
                            </div>
                            <ChevronsUpDown className="ml-auto" />
                        </SidebarMenuButton>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent
                        className="w-[--radix-dropdown-menu-trigger-width] min-w-56 rounded-lg"
                        align="start"
                        side={isMobile ? "bottom" : "right"}
                        sideOffset={4}
                    >
                        <DropdownMenuLabel className="text-xs text-muted-foreground">
                            {t("organization.switcher.organizations_label")}
                        </DropdownMenuLabel>
                        {organizations.map((orgUser) => {
                            // Ensure we have an organization object
                            const org = orgUser.organization
                            if (!org || !org.slug) return null

                            return (
                                <DropdownMenuItem
                                    key={org.id}
                                    onClick={() => handleOrgChange(org.slug!)}
                                    className="gap-2 p-2"
                                >
                                    <div className="flex size-6 items-center justify-center rounded-sm border">
                                        {org.name?.substring(0, 1).toUpperCase()}
                                    </div>
                                    {org.name}
                                    {currentOrganization.id === org.id && <Check className="ml-auto h-4 w-4" />}
                                </DropdownMenuItem>
                            )
                        })}
                        <DropdownMenuSeparator />
                        <DropdownMenuItem className="gap-2 p-2" onClick={handleCreateOrg}>
                            <div className="flex size-6 items-center justify-center rounded-md border bg-background">
                                <Plus className="size-4" />
                            </div>
                            <div className="font-medium text-muted-foreground">{t("organization.switcher.add_organization")}</div>
                        </DropdownMenuItem>
                    </DropdownMenuContent>
                </DropdownMenu>
            </SidebarMenuItem>
        </SidebarMenu>
    )
}
