import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  deleteWorkspaceMembersByUserIdMutation,
  getWorkspaceMembersQueryKey,
  patchWorkspaceMembersByUserIdMutation,
} from "@/api/@tanstack/react-query.gen";
import type { WorkspacehttpMemberResponse } from "@/api/types.gen";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
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
import { formatDateToTimezone } from "@/lib/formatDateToTimezone";
import {
  assignableRoles,
  canRemoveMember,
  type MemberRole,
} from "@/lib/member-roles";
import { workspaceErrorMessage } from "@/lib/workspace-errors";
import { RoleSelect } from "./role-select";

// Everyone in the workspace. An admin or owner also gets, per member, a role select and a Remove
// button, offering only what their own role allows (lib/member-roles.ts); the server still decides.
export function MembersCard({
  actor,
  currentUserId,
  members,
}: {
  actor: MemberRole | undefined;
  currentUserId: string | undefined;
  members: WorkspacehttpMemberResponse[];
}) {
  const { t } = useLocalizedTranslation();
  const { timezone } = useTimezone();
  const queryClient = useQueryClient();
  const [removing, setRemoving] = useState<WorkspacehttpMemberResponse | null>(null);
  const owners = members.filter((m) => m.role === "owner").length;

  const refresh = () =>
    queryClient.invalidateQueries({ queryKey: getWorkspaceMembersQueryKey() });
  const onError = (error: unknown) =>
    toast.error(workspaceErrorMessage(error, t, t("members.errors.generic")));

  const changeRole = useMutation({
    ...patchWorkspaceMembersByUserIdMutation(),
    onSuccess: () => {
      toast.success(t("members.role_updated"));
      refresh();
    },
    onError: (error) => {
      onError(error);
      // The select shows the role it was set to: show the stored one again
      refresh();
    },
  });

  const removeMember = useMutation({
    ...deleteWorkspaceMembersByUserIdMutation(),
    onSuccess: () => {
      toast.success(t("members.removed"));
      setRemoving(null);
      refresh();
    },
    onError: (error) => {
      onError(error);
      setRemoving(null);
      refresh();
    },
  });

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("members.list.title")}</CardTitle>
        <CardDescription>{t("members.list.description")}</CardDescription>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t("members.list.email")}</TableHead>
              <TableHead>{t("members.list.role")}</TableHead>
              <TableHead>{t("members.list.joined")}</TableHead>
              <TableHead />
            </TableRow>
          </TableHeader>
          <TableBody>
            {members.map((member) => {
              const roles = assignableRoles(actor, member.role, owners);
              const changeable = roles.some((role) => role !== member.role);
              const removable =
                member.userId !== currentUserId &&
                canRemoveMember(actor, member.role, owners);
              return (
                <TableRow key={member.userId}>
                  <TableCell className="break-all">
                    {member.email}
                    {member.userId === currentUserId && (
                      <span className="ml-2 text-xs text-muted-foreground">
                        ({t("members.list.you")})
                      </span>
                    )}
                  </TableCell>
                  <TableCell>
                    {changeable ? (
                      <RoleSelect
                        roles={roles}
                        value={member.role}
                        aria-label={t("members.change_role_aria", { email: member.email })}
                        disabled={changeRole.isPending}
                        onChange={(e) =>
                          changeRole.mutate({
                            path: { userId: member.userId },
                            body: { role: e.target.value as MemberRole },
                          })
                        }
                      />
                    ) : (
                      t(`members.roles.${member.role}`)
                    )}
                  </TableCell>
                  <TableCell>
                    {formatDateToTimezone(member.joinedAt, timezone, {
                      hour: undefined,
                      minute: undefined,
                      second: undefined,
                    })}
                  </TableCell>
                  <TableCell className="text-right">
                    {removable && (
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        aria-label={t("members.remove_aria", { email: member.email })}
                        onClick={() => setRemoving(member)}
                      >
                        {t("members.remove")}
                      </Button>
                    )}
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </CardContent>

      <Dialog open={removing !== null} onOpenChange={(open) => !open && setRemoving(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("members.remove_dialog.title")}</DialogTitle>
            <DialogDescription>
              {t("members.remove_dialog.description", { email: removing?.email ?? "" })}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setRemoving(null)}>
              {t("common.cancel")}
            </Button>
            <Button
              type="button"
              variant="destructive"
              disabled={removeMember.isPending}
              onClick={() =>
                removing && removeMember.mutate({ path: { userId: removing.userId } })
              }
            >
              {t("members.remove_dialog.confirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  );
}
