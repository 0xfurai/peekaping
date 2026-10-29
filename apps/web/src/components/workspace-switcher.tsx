import { useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { CheckIcon, ChevronsUpDownIcon, PlusIcon } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from "@/components/ui/sidebar";
import { NewWorkspaceDialog } from "@/components/new-workspace-dialog";
import { useMe } from "@/hooks/useMe";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { workspaceIdFromPathname, inWorkspace } from "@/lib/workspace-path";

// Switches between the user's workspaces, and makes a new one. It shows only for a user in two
// or more, so a single-workspace install looks as it always did. A switch is only an address
// change: the workspace layout empties the cache (PP-147).
export function WorkspaceSwitcher() {
  const { t } = useLocalizedTranslation();
  const { isMobile } = useSidebar();
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const { data } = useMe();
  const [creating, setCreating] = useState(false);

  const workspaces = data?.data?.workspaces ?? [];
  if (workspaces.length < 2) return null;

  const currentId = workspaceIdFromPathname(pathname)?.toLowerCase();
  const current = workspaces.find((w) => w.id.toLowerCase() === currentId);

  return (
    <>
      <SidebarMenu>
        <SidebarMenuItem>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <SidebarMenuButton
                aria-label={t("workspaces.switcher_label")}
                className="data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground"
              >
                <span className="truncate font-medium">{current?.name}</span>
                <ChevronsUpDownIcon className="ml-auto size-4" />
              </SidebarMenuButton>
            </DropdownMenuTrigger>
            <DropdownMenuContent
              className="min-w-56 rounded-lg"
              side={isMobile ? "bottom" : "right"}
              align="start"
              sideOffset={4}
            >
              {workspaces.map((workspace) => (
                <DropdownMenuItem
                  key={workspace.id}
                  onClick={() => navigate(inWorkspace(workspace.id, "/monitors"))}
                >
                  <span className="truncate">{workspace.name}</span>
                  {workspace.id.toLowerCase() === currentId && (
                    <CheckIcon className="ml-auto size-4" />
                  )}
                </DropdownMenuItem>
              ))}
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={() => setCreating(true)}>
                <PlusIcon />
                {t("workspaces.new")}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </SidebarMenuItem>
      </SidebarMenu>
      <NewWorkspaceDialog open={creating} onOpenChange={setCreating} />
    </>
  );
}
