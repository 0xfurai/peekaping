import { useState } from "react";
import { MoreHorizontalIcon } from "lucide-react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  getAdminUsersQueryKey,
  getMeQueryKey,
  patchAdminUsersByIdMutation,
  postAdminUsersByIdResetLinkMutation,
} from "@/api/@tanstack/react-query.gen";
import type { AdminUserResponse } from "@/api/types.gen";
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
import { useMe } from "@/hooks/useMe";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import {
  adminErrorMessage,
  isLastInstanceAdmin,
  userActions,
  type UserAction,
} from "@/lib/admin-users";
import { formatDateToTimezone } from "@/lib/formatDateToTimezone";
import { ResetLinkDialog } from "./reset-link-dialog";
import { UserActionDialog, type ConfirmTarget } from "./user-action-dialog";

const DATE_ONLY: Intl.DateTimeFormatOptions = {
  hour: undefined,
  minute: undefined,
  second: undefined,
};

// The users of the install, each with a menu of what an instance admin can do to them
// (lib/admin-users.ts). complete says the list holds every user, which is what tells whether
// you are the last instance admin.
export function UsersTable({
  users,
  currentUserId,
  complete,
}: {
  users: AdminUserResponse[];
  currentUserId: string | undefined;
  complete: boolean;
}) {
  const { t } = useLocalizedTranslation();
  const { timezone } = useTimezone();
  const queryClient = useQueryClient();
  const accountEmails = useMe().data?.data?.accountEmails === true;
  const [confirming, setConfirming] = useState<ConfirmTarget | null>(null);
  const [resetFor, setResetFor] = useState<AdminUserResponse | null>(null);

  const update = useMutation(patchAdminUsersByIdMutation());
  const resetLink = useMutation(postAdminUsersByIdResetLinkMutation());

  const refresh = (userId: string) => {
    queryClient.invalidateQueries({ queryKey: getAdminUsersQueryKey() });
    // Taking your own flag takes the admin area away: /me says so, and the area closes
    if (userId === currentUserId) queryClient.invalidateQueries({ queryKey: getMeQueryKey() });
  };

  const change = (
    user: AdminUserResponse,
    body: { instanceAdmin: boolean } | { active: boolean },
    message: string
  ) =>
    update.mutate(
      { path: { id: user.id }, body },
      {
        onSuccess: () => {
          toast.success(t(message, { email: user.email }));
          refresh(user.id);
        },
        onError: (error) => {
          toast.error(adminErrorMessage(error, t, t("admin.users.messages.update_failed")));
          refresh(user.id);
        },
      }
    );

  const run = (action: UserAction, user: AdminUserResponse) => {
    switch (action) {
      case "grantAdmin":
        return change(user, { instanceAdmin: true }, "admin.users.messages.admin_granted");
      case "revokeAdmin":
        return change(user, { instanceAdmin: false }, "admin.users.messages.admin_revoked");
      case "enable":
        return change(user, { active: true }, "admin.users.messages.enabled");
      case "disable":
      case "disableTwoFactor":
      case "delete":
        return setConfirming({ kind: action, user });
      case "resetLink":
        setResetFor(user);
        return resetLink.mutate({ path: { id: user.id } });
    }
  };

  const closeResetLink = () => {
    setResetFor(null);
    // The link is shown once: forget it
    resetLink.reset();
  };

  const yesNo = (value: boolean) => t(value ? "admin.users.values.yes" : "admin.users.values.no");

  return (
    <>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t("admin.users.columns.email")}</TableHead>
            <TableHead>{t("admin.users.columns.workspaces")}</TableHead>
            <TableHead>{t("admin.users.columns.instance_admin")}</TableHead>
            <TableHead>{t("admin.users.columns.status")}</TableHead>
            <TableHead>{t("admin.users.columns.two_factor")}</TableHead>
            <TableHead>{t("admin.users.columns.created")}</TableHead>
            <TableHead>{t("admin.users.columns.last_sign_in")}</TableHead>
            <TableHead>{t("admin.users.columns.email_verified")}</TableHead>
            <TableHead />
          </TableRow>
        </TableHeader>
        <TableBody>
          {users.map((user) => {
            const self = user.id === currentUserId;
            const actions = userActions(user, {
              self,
              onlyInstanceAdmin: isLastInstanceAdmin(user, users, complete),
            });
            return (
              <TableRow key={user.id}>
                <TableCell className="break-all">
                  {user.email}
                  {self && (
                    <span className="ml-2 text-xs text-muted-foreground">
                      ({t("admin.users.you")})
                    </span>
                  )}
                </TableCell>
                <TableCell>
                  {user.workspaces.length === 0 ? (
                    <span className="text-muted-foreground">{t("admin.users.none")}</span>
                  ) : (
                    <ul className="flex flex-col gap-0.5">
                      {user.workspaces.map((workspace) => (
                        <li key={workspace.id}>
                          {`${workspace.name} (${t(`members.roles.${workspace.role}`)})`}
                        </li>
                      ))}
                    </ul>
                  )}
                </TableCell>
                <TableCell>
                  {user.instanceAdmin ? (
                    <Badge>{t("admin.users.values.yes")}</Badge>
                  ) : (
                    <span className="text-muted-foreground">{t("admin.users.values.no")}</span>
                  )}
                </TableCell>
                <TableCell>
                  <Badge variant={user.active ? "secondary" : "destructive"}>
                    {t(user.active ? "admin.users.values.active" : "admin.users.values.disabled")}
                  </Badge>
                </TableCell>
                <TableCell>
                  {t(
                    user.twoFactorEnabled
                      ? "admin.users.values.two_factor_on"
                      : "admin.users.values.two_factor_off"
                  )}
                </TableCell>
                <TableCell>{formatDateToTimezone(user.createdAt, timezone, DATE_ONLY)}</TableCell>
                <TableCell>
                  {user.lastLoginAt
                    ? formatDateToTimezone(user.lastLoginAt, timezone, DATE_ONLY)
                    : t("admin.users.never")}
                </TableCell>
                <TableCell>{yesNo(user.emailVerified)}</TableCell>
                <TableCell className="text-right">
                  <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon"
                        aria-label={t("admin.users.actions_aria", { email: user.email })}
                      >
                        <MoreHorizontalIcon />
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end">
                      {actions.map((action) => (
                        <DropdownMenuItem
                          key={action}
                          variant={action === "delete" ? "destructive" : "default"}
                          disabled={update.isPending}
                          onSelect={() => run(action, user)}
                        >
                          {t(`admin.users.actions.${action}`)}
                        </DropdownMenuItem>
                      ))}
                    </DropdownMenuContent>
                  </DropdownMenu>
                </TableCell>
              </TableRow>
            );
          })}
        </TableBody>
      </Table>

      <UserActionDialog target={confirming} onClose={() => setConfirming(null)} />
      <ResetLinkDialog
        email={resetFor?.email ?? ""}
        open={resetFor !== null}
        reset={resetLink.data?.data ?? null}
        error={
          resetLink.isError
            ? adminErrorMessage(resetLink.error, t, t("admin.users.reset_link.failed"))
            : null
        }
        accountEmails={accountEmails}
        onClose={closeResetLink}
      />
    </>
  );
}
