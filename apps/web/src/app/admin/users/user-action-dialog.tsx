import { useId, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  deleteAdminUsersByIdMutation,
  getAdminUsersQueryKey,
  patchAdminUsersByIdMutation,
  postAdminUsersByIdDisable2FaMutation,
} from "@/api/@tanstack/react-query.gen";
import type { AdminUserResponse } from "@/api/types.gen";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { adminErrorMessage, dismissWhileIdle } from "@/lib/admin-users";

// The actions that ask for a confirmation first: each ends the user's sessions, takes away a
// protection or deletes the account
export type ConfirmKind = "disable" | "disableTwoFactor" | "delete";

export type ConfirmTarget = { kind: ConfirmKind; user: AdminUserResponse };

// The body of the confirmation: what the action does, the server's refusal when there was one, and the
// buttons. The title and the dialog around it are the caller's, since they need the dialog's context.
export function UserActionDialogContent({
  kind,
  email,
  pending,
  error,
  descriptionId,
  onCancel,
  onConfirm,
}: {
  kind: ConfirmKind;
  email: string;
  pending: boolean;
  // What went wrong with the last try, already worded for the user
  error: string | null;
  descriptionId?: string;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const { t } = useLocalizedTranslation();
  return (
    <div className="flex flex-col gap-4">
      <p id={descriptionId} className="text-sm text-muted-foreground">
        {kind === "delete"
          ? t("admin.users.confirm.delete.description", { email })
          : t(`admin.users.confirm.${kind}.description`, { email })}
      </p>
      {error && (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="outline" disabled={pending} onClick={onCancel}>
          {t("admin.users.confirm.cancel")}
        </Button>
        <Button
          type="button"
          variant={kind === "disableTwoFactor" ? "default" : "destructive"}
          disabled={pending}
          onClick={onConfirm}
        >
          {t(`admin.users.confirm.${kind}.confirm`)}
        </Button>
      </div>
    </div>
  );
}

// Asks before disabling a user, turning off their 2FA or deleting them, and does it. A refusal
// of the server (the last owner of a workspace, say) stays in the dialog with its reasons.
export function UserActionDialog({
  target,
  onClose,
}: {
  target: ConfirmTarget | null;
  onClose: () => void;
}) {
  const { t } = useLocalizedTranslation();
  const queryClient = useQueryClient();
  const descriptionId = useId();
  const [failure, setFailure] = useState<{ target: ConfirmTarget; message: string } | null>(null);

  const disable = useMutation(patchAdminUsersByIdMutation());
  const turnOffTwoFactor = useMutation(postAdminUsersByIdDisable2FaMutation());
  const remove = useMutation(deleteAdminUsersByIdMutation());
  const pending = disable.isPending || turnOffTwoFactor.isPending || remove.isPending;

  const close = () => {
    setFailure(null);
    onClose();
  };

  const confirm = () => {
    if (!target) return;
    const { kind, user } = target;
    const options = {
      onSuccess: () => {
        toast.success(
          t(
            kind === "delete"
              ? "admin.users.messages.deleted"
              : kind === "disable"
                ? "admin.users.messages.disabled"
                : "admin.users.messages.two_factor_off",
            { email: user.email }
          )
        );
        queryClient.invalidateQueries({ queryKey: getAdminUsersQueryKey() });
        close();
      },
      onError: (error: unknown) =>
        setFailure({
          target,
          message: adminErrorMessage(error, t, t("admin.users.messages.update_failed")),
        }),
    };
    const path = { id: user.id };
    if (kind === "delete") remove.mutate({ path }, options);
    else if (kind === "disable") disable.mutate({ path, body: { active: false } }, options);
    else turnOffTwoFactor.mutate({ path }, options);
  };

  // Show the refusal only for the action it answered
  const error = failure && failure.target === target ? failure.message : null;
  // A pending action can't be cancelled: the dialog stays until its answer
  const { onOpenChange, onEscapeKeyDown, onPointerDownOutside } = dismissWhileIdle(pending, close);

  return (
    <Dialog open={target !== null} onOpenChange={onOpenChange}>
      <DialogContent
        aria-describedby={descriptionId}
        showCloseButton={!pending}
        onEscapeKeyDown={onEscapeKeyDown}
        onPointerDownOutside={onPointerDownOutside}
      >
        <DialogHeader>
          <DialogTitle>{t(`admin.users.confirm.${target?.kind ?? "delete"}.title`)}</DialogTitle>
        </DialogHeader>
        <UserActionDialogContent
          kind={target?.kind ?? "delete"}
          email={target?.user.email ?? ""}
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
