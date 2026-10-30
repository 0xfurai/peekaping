import { useId, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { getMeQueryKey, postWorkspaceDeletionMutation } from "@/api/@tanstack/react-query.gen";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useTimezone } from "@/context/timezone-context";
import { useCancelWorkspaceDeletion } from "@/hooks/useCancelWorkspaceDeletion";
import { useCurrentWorkspace } from "@/hooks/useCurrentWorkspace";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { dismissWhileIdle } from "@/lib/admin-users";
import { canDeleteWorkspace } from "@/lib/admin-workspaces";
import { workspaceErrorMessage } from "@/lib/workspace-errors";
import { deletionNotice, formatDeletionDate } from "@/lib/workspace-deletion";

// Deleting the workspace, for its owners (GLOSSARY "Scheduled deletion"). The owner types the
// workspace's name; the workspace is then held for 7 days, and the card offers Cancel deletion
// until it is gone. An admin or a member doesn't get the card; the banner tells them the date.
export function DangerZoneCard() {
  const workspace = useCurrentWorkspace();
  if (workspace?.role !== "owner") return null;
  const notice = deletionNotice(workspace);
  return notice ? (
    <ScheduledDeletionCard scheduledAt={notice.scheduledAt} />
  ) : (
    <DeleteWorkspaceCard name={workspace.name} />
  );
}

function ScheduledDeletionCard({ scheduledAt }: { scheduledAt: string }) {
  const { t, i18n } = useLocalizedTranslation();
  const { timezone } = useTimezone();
  const { cancel, pending } = useCancelWorkspaceDeletion();
  return (
    <Card className="border-destructive/50">
      <CardHeader>
        <CardTitle>{t("settings.danger_zone.title")}</CardTitle>
        <CardDescription>
          {t("settings.danger_zone.scheduled", { date: formatDeletionDate(scheduledAt, i18n.language, timezone) })}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <Button type="button" variant="outline" disabled={pending} onClick={cancel}>
          {pending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
          {t("workspaces.deletion.cancel")}
        </Button>
      </CardContent>
    </Card>
  );
}

function DeleteWorkspaceCard({ name }: { name: string }) {
  const { t } = useLocalizedTranslation();
  const [open, setOpen] = useState(false);
  return (
    <Card className="border-destructive/50">
      <CardHeader>
        <CardTitle>{t("settings.danger_zone.title")}</CardTitle>
        <CardDescription>{t("settings.danger_zone.description")}</CardDescription>
      </CardHeader>
      <CardContent>
        <Button type="button" variant="destructive" onClick={() => setOpen(true)}>
          {t("settings.danger_zone.button")}
        </Button>
      </CardContent>
      <DeleteWorkspaceDialog name={name} open={open} onClose={() => setOpen(false)} />
    </Card>
  );
}

// Asks for the workspace's name, typed exactly, and schedules the deletion. /me is fetched again
// before the dialog closes: it carries the date, which turns the card into the scheduled one and
// shows the banner.
function DeleteWorkspaceDialog({ name, open, onClose }: { name: string; open: boolean; onClose: () => void }) {
  const { t } = useLocalizedTranslation();
  const queryClient = useQueryClient();
  const inputId = useId();
  const descriptionId = useId();
  const [typed, setTyped] = useState("");
  const [error, setError] = useState<string | null>(null);

  const close = () => {
    setTyped("");
    setError(null);
    onClose();
  };

  const schedule = useMutation({
    ...postWorkspaceDeletionMutation(),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: getMeQueryKey() });
      close();
    },
    onError: (failure) => setError(workspaceErrorMessage(failure, t, t("settings.danger_zone.failed"))),
  });
  const ready = canDeleteWorkspace(typed, name);
  const confirm = () => {
    if (ready) schedule.mutate({ body: { name: typed } });
  };
  // A pending request can't be dismissed: the dialog stays until its answer
  const { onOpenChange, onEscapeKeyDown, onPointerDownOutside } = dismissWhileIdle(schedule.isPending, close);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        aria-describedby={descriptionId}
        showCloseButton={!schedule.isPending}
        onEscapeKeyDown={onEscapeKeyDown}
        onPointerDownOutside={onPointerDownOutside}
      >
        <DialogHeader>
          <DialogTitle>{t("settings.danger_zone.button")}</DialogTitle>
          <DialogDescription id={descriptionId}>
            {t("settings.danger_zone.dialog_description", { name })}
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-2">
          <Label htmlFor={inputId} className="break-all">
            {t("settings.danger_zone.name_label", { name })}
          </Label>
          <Input
            id={inputId}
            autoComplete="off"
            value={typed}
            disabled={schedule.isPending}
            onChange={(event) => setTyped(event.target.value)}
          />
        </div>
        {error && (
          <Alert variant="destructive">
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        )}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" disabled={schedule.isPending} onClick={close}>
            {t("common.cancel")}
          </Button>
          <Button type="button" variant="destructive" disabled={schedule.isPending || !ready} onClick={confirm}>
            {t("settings.danger_zone.button")}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
