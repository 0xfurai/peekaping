import { useId, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  deleteAdminWorkspacesByIdMutation,
  getAdminWorkspacesQueryKey,
  getMeQueryKey,
  patchAdminWorkspacesByIdMutation,
} from "@/api/@tanstack/react-query.gen";
import type { AdminWorkspaceResponse } from "@/api/types.gen";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useMe } from "@/hooks/useMe";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { adminErrorMessage, dismissWhileIdle } from "@/lib/admin-users";
import { canDeleteWorkspace, type WorkspaceAction } from "@/lib/admin-workspaces";

// The actions that ask for a confirmation: Limits opens a dialog of its own (workspace-limits-dialog)
export type ConfirmedAction = Exclude<WorkspaceAction, "limits">;

export type WorkspaceConfirmTarget = { kind: ConfirmedAction; workspace: AdminWorkspaceResponse };

// The body of the confirmation: what the action does, the workspace's name to type before a delete,
// the server's refusal when there was one, and the buttons. The title and the dialog around it are
// the caller's, since they need the dialog's context.
export function WorkspaceActionDialogContent({
  kind,
  name,
  typed,
  onTypedChange,
  pending,
  error,
  descriptionId,
  onCancel,
  onConfirm,
}: {
  kind: ConfirmedAction;
  name: string;
  // What the admin typed as the workspace's name, which a delete waits for
  typed: string;
  onTypedChange: (typed: string) => void;
  pending: boolean;
  // What went wrong with the last try, already worded for the user
  error: string | null;
  descriptionId?: string;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const { t } = useLocalizedTranslation();
  const inputId = useId();
  const ready = kind !== "delete" || canDeleteWorkspace(typed, name);
  return (
    <div className="flex flex-col gap-4">
      <p id={descriptionId} className="text-sm text-muted-foreground">
        {t(`admin.workspaces.confirm.${kind}.description`, { name })}
      </p>
      {kind === "delete" && (
        <div className="flex flex-col gap-2">
          <Label htmlFor={inputId} className="break-all">
            {t("admin.workspaces.confirm.delete.name_label", { name })}
          </Label>
          <Input
            id={inputId}
            autoComplete="off"
            value={typed}
            disabled={pending}
            onChange={(event) => onTypedChange(event.target.value)}
          />
        </div>
      )}
      {error && (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="outline" disabled={pending} onClick={onCancel}>
          {t("admin.workspaces.confirm.cancel")}
        </Button>
        <Button
          type="button"
          variant={kind === "resume" ? "default" : "destructive"}
          disabled={pending || !ready}
          onClick={onConfirm}
        >
          {t(`admin.workspaces.confirm.${kind}.confirm`)}
        </Button>
      </div>
    </div>
  );
}

// Asks before suspending, resuming or deleting a workspace, and does it. When the admin is a
// member of the workspace, /me is fetched again: it carries the workspace's suspension, and after
// a delete no longer lists it, so the way back from the admin area leads to the default workspace
// /me names then, or to the no-workspace page.
export function WorkspaceActionDialog({
  target,
  onClose,
}: {
  target: WorkspaceConfirmTarget | null;
  onClose: () => void;
}) {
  const { t } = useLocalizedTranslation();
  const queryClient = useQueryClient();
  const descriptionId = useId();
  const myWorkspaces = useMe().data?.data?.workspaces;
  const [typed, setTyped] = useState("");
  const [failure, setFailure] = useState<{ target: WorkspaceConfirmTarget; message: string } | null>(
    null
  );

  const update = useMutation(patchAdminWorkspacesByIdMutation());
  const remove = useMutation(deleteAdminWorkspacesByIdMutation());
  const pending = update.isPending || remove.isPending;

  const close = () => {
    setFailure(null);
    setTyped("");
    onClose();
  };

  const confirm = () => {
    if (!target) return;
    const { kind, workspace } = target;
    if (kind === "delete" && !canDeleteWorkspace(typed, workspace.name)) return;
    const member = myWorkspaces?.some((w) => w.id.toLowerCase() === workspace.id.toLowerCase());
    const options = {
      onSuccess: async () => {
        toast.success(
          t(
            kind === "delete"
              ? "admin.workspaces.messages.deleted"
              : kind === "suspend"
                ? "admin.workspaces.messages.suspended"
                : "admin.workspaces.messages.resumed",
            { name: workspace.name }
          )
        );
        queryClient.invalidateQueries({ queryKey: getAdminWorkspacesQueryKey() });
        if (member) await queryClient.invalidateQueries({ queryKey: getMeQueryKey() });
        close();
      },
      onError: (error: unknown) => {
        setFailure({
          target,
          message: adminErrorMessage(error, t, t("admin.workspaces.messages.failed")),
        });
        // The workspace may be gone or changed by another admin: show the list as it is
        queryClient.invalidateQueries({ queryKey: getAdminWorkspacesQueryKey() });
      },
    };
    const path = { id: workspace.id };
    if (kind === "delete") remove.mutate({ path }, options);
    else update.mutate({ path, body: { suspended: kind === "suspend" } }, options);
  };

  // Show the refusal only for the action it answered
  const error = failure && failure.target === target ? failure.message : null;
  // A pending action can't be cancelled: the dialog stays until its answer
  const { onOpenChange, onEscapeKeyDown, onPointerDownOutside } = dismissWhileIdle(pending, close);
  const kind = target?.kind ?? "delete";

  return (
    <Dialog open={target !== null} onOpenChange={onOpenChange}>
      <DialogContent
        aria-describedby={descriptionId}
        showCloseButton={!pending}
        onEscapeKeyDown={onEscapeKeyDown}
        onPointerDownOutside={onPointerDownOutside}
      >
        <DialogHeader>
          <DialogTitle>{t(`admin.workspaces.confirm.${kind}.title`)}</DialogTitle>
        </DialogHeader>
        <WorkspaceActionDialogContent
          kind={kind}
          name={target?.workspace.name ?? ""}
          typed={typed}
          onTypedChange={setTyped}
          pending={pending}
          error={error}
          descriptionId={descriptionId}
          onCancel={close}
          onConfirm={confirm}
        />
      </DialogContent>
    </Dialog>
  );
}
