import { useState } from "react";
import { MoreHorizontalIcon } from "lucide-react";
import type { AdminWorkspaceResponse } from "@/api/types.gen";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useTimezone } from "@/context/timezone-context";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { limitsSummary, workspaceActions } from "@/lib/admin-workspaces";
import { formatDateToTimezone } from "@/lib/formatDateToTimezone";
import { WorkspaceActionDialog, type WorkspaceConfirmTarget } from "./workspace-action-dialog";
import { WorkspaceLimitsDialog } from "./workspace-limits-dialog";

const DATE_ONLY: Intl.DateTimeFormatOptions = {
  hour: undefined,
  minute: undefined,
  second: undefined,
};

// The status of a workspace: running, or suspended with the reason of the hold that shows
// ("admin" before "unverified" when both hold)
function WorkspaceStatus({ workspace }: { workspace: AdminWorkspaceResponse }) {
  const { t } = useLocalizedTranslation();
  if (!workspace.suspended) {
    return <Badge variant="secondary">{t("admin.workspaces.status.running")}</Badge>;
  }
  return workspace.suspendedReason === "admin" ? (
    <Badge variant="destructive">{t("admin.workspaces.status.suspended_admin")}</Badge>
  ) : (
    <Badge variant="outline">{t("admin.workspaces.status.suspended_unverified")}</Badge>
  );
}

// The limits a workspace is held to, in short, and where they come from: its own, the install's
// defaults, or none
function WorkspaceLimits({ workspace }: { workspace: AdminWorkspaceResponse }) {
  const { t } = useLocalizedTranslation();
  const { shown, more } = limitsSummary(workspace.limits);
  const summary =
    shown.length === 0
      ? t("admin.workspaces.limits.unlimited")
      : [
          ...shown.map(({ name, value }) => t("admin.workspaces.limits.item", { name: t(`limits.names.${name}`), value })),
          ...(more > 0 ? [t("admin.workspaces.limits.more", { number: more })] : []),
        ].join(", ");
  return (
    <div className="flex flex-col gap-0.5">
      <span>{summary}</span>
      <span className="text-xs text-muted-foreground">
        {t(`admin.workspaces.limits.source.${workspace.limits.source}`)}
      </span>
    </div>
  );
}

// The workspaces of the install, each with a menu of what an instance admin can do to it
// (lib/admin-workspaces.ts). Every action asks for a confirmation first, and Limits opens its
// dialog.
export function WorkspacesTable({ workspaces }: { workspaces: AdminWorkspaceResponse[] }) {
  const { t } = useLocalizedTranslation();
  const { timezone } = useTimezone();
  const [confirming, setConfirming] = useState<WorkspaceConfirmTarget | null>(null);
  const [limitsOf, setLimitsOf] = useState<AdminWorkspaceResponse | null>(null);

  return (
    <>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t("admin.workspaces.columns.name")}</TableHead>
            <TableHead>{t("admin.workspaces.columns.created")}</TableHead>
            <TableHead>{t("admin.workspaces.columns.owners")}</TableHead>
            <TableHead>{t("admin.workspaces.columns.members")}</TableHead>
            <TableHead>{t("admin.workspaces.columns.monitors")}</TableHead>
            <TableHead>{t("admin.workspaces.columns.status_pages")}</TableHead>
            <TableHead>{t("admin.workspaces.columns.custom_domains")}</TableHead>
            <TableHead>{t("admin.workspaces.columns.limits")}</TableHead>
            <TableHead>{t("admin.workspaces.columns.status")}</TableHead>
            <TableHead />
          </TableRow>
        </TableHeader>
        <TableBody>
          {workspaces.map((workspace) => (
            <TableRow key={workspace.id}>
              <TableCell className="break-all">{workspace.name}</TableCell>
              <TableCell>{formatDateToTimezone(workspace.createdAt, timezone, DATE_ONLY)}</TableCell>
              <TableCell>
                {workspace.owners.length === 0 ? (
                  <span className="text-muted-foreground">{t("admin.workspaces.none")}</span>
                ) : (
                  <ul className="flex flex-col gap-0.5 break-all">
                    {workspace.owners.map((email) => (
                      <li key={email}>{email}</li>
                    ))}
                  </ul>
                )}
              </TableCell>
              <TableCell>{workspace.memberCount}</TableCell>
              <TableCell>
                {t("admin.workspaces.monitors", {
                  active: workspace.monitors.active,
                  paused: workspace.monitors.paused,
                })}
              </TableCell>
              <TableCell>{workspace.statusPages}</TableCell>
              <TableCell>{workspace.customDomains}</TableCell>
              <TableCell>
                <WorkspaceLimits workspace={workspace} />
              </TableCell>
              <TableCell>
                <WorkspaceStatus workspace={workspace} />
              </TableCell>
              <TableCell className="text-right">
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      aria-label={t("admin.workspaces.actions_aria", { name: workspace.name })}
                    >
                      <MoreHorizontalIcon />
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end">
                    {workspaceActions(workspace).map((action) => (
                      <DropdownMenuItem
                        key={action}
                        variant={action === "delete" ? "destructive" : "default"}
                        onSelect={() =>
                          action === "limits" ? setLimitsOf(workspace) : setConfirming({ kind: action, workspace })
                        }
                      >
                        {t(`admin.workspaces.actions.${action}`)}
                      </DropdownMenuItem>
                    ))}
                  </DropdownMenuContent>
                </DropdownMenu>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>

      <WorkspaceActionDialog target={confirming} onClose={() => setConfirming(null)} />
      <WorkspaceLimitsDialog workspace={limitsOf} onClose={() => setLimitsOf(null)} />
    </>
  );
}
