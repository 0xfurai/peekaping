import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  deleteWorkspaceInvitesByIdMutation,
  getWorkspaceInvitesOptions,
  getWorkspaceInvitesQueryKey,
} from "@/api/@tanstack/react-query.gen";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
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
import { workspaceErrorMessage } from "@/lib/workspace-errors";

// The invites nobody used yet, with revoke, for an admin or owner
export function PendingInvitesCard() {
  const { t } = useLocalizedTranslation();
  const { timezone } = useTimezone();
  const queryClient = useQueryClient();
  const { data, isLoading } = useQuery(getWorkspaceInvitesOptions());
  const invites = data?.data ?? [];

  const revoke = useMutation({
    ...deleteWorkspaceInvitesByIdMutation(),
    onSuccess: () => {
      toast.success(t("members.pending.revoked"));
      queryClient.invalidateQueries({ queryKey: getWorkspaceInvitesQueryKey() });
    },
    onError: (error) =>
      toast.error(workspaceErrorMessage(error, t, t("members.errors.generic"))),
  });

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("members.pending.title")}</CardTitle>
        <CardDescription>{t("members.pending.description")}</CardDescription>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <div className="h-10 flex items-center">{t("common.loading")}</div>
        ) : invites.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("members.pending.empty")}</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t("members.pending.email")}</TableHead>
                <TableHead>{t("members.pending.role")}</TableHead>
                <TableHead>{t("members.pending.expires")}</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {invites.map((invite) => (
                <TableRow key={invite.id}>
                  <TableCell className="break-all">{invite.email}</TableCell>
                  <TableCell>{t(`members.roles.${invite.role}`)}</TableCell>
                  <TableCell>
                    {invite.status === "expired"
                      ? t("members.pending.expired")
                      : formatDateToTimezone(invite.expiresAt, timezone, {
                          hour: undefined,
                          minute: undefined,
                          second: undefined,
                        })}
                  </TableCell>
                  <TableCell className="text-right">
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      aria-label={t("members.pending.revoke_aria", { email: invite.email })}
                      disabled={revoke.isPending}
                      onClick={() => revoke.mutate({ path: { id: invite.id } })}
                    >
                      {t("members.pending.revoke")}
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  );
}
