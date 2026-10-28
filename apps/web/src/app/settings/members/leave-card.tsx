import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { getMeOptions, postWorkspaceLeaveMutation } from "@/api/@tanstack/react-query.gen";
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
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { canLeave, type MemberRole } from "@/lib/member-roles";
import { refreshMeThen } from "@/lib/refresh-me-then";
import { workspaceErrorMessage } from "@/lib/workspace-errors";

// Leaving the workspace, for every member. The last owner can't, and is told why.
export function LeaveCard({
  role,
  owners,
}: {
  role: MemberRole | undefined;
  owners: number;
}) {
  const { t } = useLocalizedTranslation();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [confirming, setConfirming] = useState(false);

  const leave = useMutation({
    ...postWorkspaceLeaveMutation(),
    onSuccess: async () => {
      setConfirming(false);
      toast.success(t("members.leave.done"));
      // The workspace is gone from /me; the app's start is the default workspace, or the page
      // for a user who has none. If /me can't be fetched the page stays and offers a retry:
      // going on could land in a workspace the user may no longer be in.
      await refreshMeThen({
        refresh: () => queryClient.fetchQuery({ ...getMeOptions(), staleTime: 0 }),
        proceed: () => navigate("/"),
        onFailed: (retry) =>
          toast.error(t("members.leave.refresh_failed"), {
            action: { label: t("common.retry"), onClick: retry },
          }),
      });
    },
    onError: (error) => {
      setConfirming(false);
      toast.error(workspaceErrorMessage(error, t, t("members.errors.generic")));
    },
  });

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("members.leave.title")}</CardTitle>
        <CardDescription>{t("members.leave.description")}</CardDescription>
      </CardHeader>
      <CardContent>
        {canLeave(role, owners) ? (
          <Button type="button" variant="destructive" onClick={() => setConfirming(true)}>
            {t("members.leave.button")}
          </Button>
        ) : (
          <p className="text-sm text-muted-foreground">{t("members.leave.last_owner")}</p>
        )}
      </CardContent>

      <Dialog open={confirming} onOpenChange={setConfirming}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("members.leave.title")}</DialogTitle>
            <DialogDescription>{t("members.leave.description")}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setConfirming(false)}>
              {t("common.cancel")}
            </Button>
            <Button
              type="button"
              variant="destructive"
              disabled={leave.isPending}
              onClick={() => leave.mutate({})}
            >
              {t("members.leave.confirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  );
}
