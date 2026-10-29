import { useId, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  getAdminWorkspacesQueryKey,
  getWorkspaceLimitsQueryKey,
  patchAdminWorkspacesByIdMutation,
} from "@/api/@tanstack/react-query.gen";
import type { AdminWorkspaceResponse } from "@/api/types.gen";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { adminErrorMessage, dismissWhileIdle } from "@/lib/admin-users";
import {
  OWN_LIMITS,
  limitsBody,
  limitsDraft,
  type LimitsChoice,
  type LimitsDraft,
  type OwnLimit,
} from "@/lib/admin-workspaces";

const CHOICES: LimitsChoice[] = ["own", "unlimited", "defaults"];

// The body of the Limits dialog: the three choices, the workspace's own limits to type when it gets
// its own, what the server refused, and the buttons. The title and the dialog around it are the
// caller's, as in WorkspaceActionDialogContent.
export function WorkspaceLimitsDialogContent({
  draft,
  onDraftChange,
  invalid,
  pending,
  error,
  descriptionId,
  onCancel,
  onSave,
}: {
  draft: LimitsDraft;
  onDraftChange: (draft: LimitsDraft) => void;
  // The limits that are no whole number of 1 or more, after a save was tried
  invalid: OwnLimit[];
  pending: boolean;
  // What went wrong with the last save, already worded for the user
  error: string | null;
  descriptionId?: string;
  onCancel: () => void;
  onSave: () => void;
}) {
  const { t } = useLocalizedTranslation();
  const id = useId();
  const own = draft.choice === "own";
  return (
    <div className="flex flex-col gap-4">
      <p id={descriptionId} className="text-sm text-muted-foreground">
        {t("admin.workspaces.limits.dialog.description")}
      </p>
      <fieldset className="flex flex-col gap-3" disabled={pending}>
        {CHOICES.map((choice) => (
          <div key={choice} className="flex items-start gap-2">
            <input
              type="radio"
              id={`${id}-${choice}`}
              name={`${id}-choice`}
              value={choice}
              className="mt-1"
              checked={draft.choice === choice}
              onChange={() => onDraftChange({ ...draft, choice })}
            />
            <div className="flex flex-col gap-0.5">
              <Label htmlFor={`${id}-${choice}`}>{t(`admin.workspaces.limits.dialog.${choice}`)}</Label>
              <span className="text-xs text-muted-foreground">
                {t(`admin.workspaces.limits.dialog.${choice}_help`)}
              </span>
            </div>
          </div>
        ))}
      </fieldset>
      {own && (
        <div className="grid gap-3 sm:grid-cols-2">
          {OWN_LIMITS.map((name) => (
            <div key={name} className="flex flex-col gap-1">
              <Label htmlFor={`${id}-${name}`}>{t(`limits.names.${name}`)}</Label>
              <Input
                id={`${id}-${name}`}
                type="number"
                min={1}
                inputMode="numeric"
                placeholder={t("limits.none")}
                value={draft.values[name]}
                disabled={pending}
                aria-invalid={invalid.includes(name) || undefined}
                onChange={(event) =>
                  onDraftChange({ ...draft, values: { ...draft.values, [name]: event.target.value } })
                }
              />
            </div>
          ))}
        </div>
      )}
      {own && invalid.length > 0 && <p className="text-sm text-destructive">{t("limits.invalid")}</p>}
      {error && (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="outline" disabled={pending} onClick={onCancel}>
          {t("admin.workspaces.limits.dialog.cancel")}
        </Button>
        <Button type="button" disabled={pending} onClick={onSave}>
          {t("admin.workspaces.limits.dialog.save")}
        </Button>
      </div>
    </div>
  );
}

// Sets a workspace's own limits, makes it unlimited, or gives it the install's defaults again
// (PATCH /admin/workspaces/{id}). Lowering a limit pauses and deletes nothing.
export function WorkspaceLimitsDialog({
  workspace,
  onClose,
}: {
  workspace: AdminWorkspaceResponse | null;
  onClose: () => void;
}) {
  const { t } = useLocalizedTranslation();
  const queryClient = useQueryClient();
  const descriptionId = useId();
  const update = useMutation(patchAdminWorkspacesByIdMutation());
  const pending = update.isPending;
  // The fields start from the workspace's limits each time the dialog opens for one
  const [form, setForm] = useState<{
    id: string;
    draft: LimitsDraft;
    invalid: OwnLimit[];
    error: string | null;
  } | null>(null);
  if (workspace && form?.id !== workspace.id) {
    setForm({ id: workspace.id, draft: limitsDraft(workspace.limits), invalid: [], error: null });
  }

  const close = () => {
    setForm(null);
    onClose();
  };

  const save = () => {
    if (!workspace || !form) return;
    const result = limitsBody(form.draft);
    if ("invalid" in result) {
      setForm({ ...form, invalid: result.invalid });
      return;
    }
    setForm({ ...form, invalid: [], error: null });
    update.mutate(
      { path: { id: workspace.id }, body: result.body },
      {
        onSuccess: () => {
          toast.success(t("admin.workspaces.limits.dialog.saved", { name: workspace.name }));
          queryClient.invalidateQueries({ queryKey: getAdminWorkspacesQueryKey() });
          // The admin may be a member of the workspace, whose limits the app reads
          queryClient.invalidateQueries({ queryKey: getWorkspaceLimitsQueryKey() });
          close();
        },
        onError: (error) => {
          const message = adminErrorMessage(error, t, t("admin.workspaces.limits.dialog.failed"));
          setForm((current) => (current ? { ...current, error: message } : current));
          // The workspace may be gone or changed by another admin: show the list as it is
          queryClient.invalidateQueries({ queryKey: getAdminWorkspacesQueryKey() });
        },
      }
    );
  };

  // A pending save can't be cancelled: the dialog stays until its answer
  const { onOpenChange, onEscapeKeyDown, onPointerDownOutside } = dismissWhileIdle(pending, close);

  return (
    <Dialog open={workspace !== null} onOpenChange={onOpenChange}>
      <DialogContent
        aria-describedby={descriptionId}
        showCloseButton={!pending}
        onEscapeKeyDown={onEscapeKeyDown}
        onPointerDownOutside={onPointerDownOutside}
      >
        <DialogHeader>
          <DialogTitle className="break-all">
            {t("admin.workspaces.limits.dialog.title", { name: workspace?.name ?? "" })}
          </DialogTitle>
        </DialogHeader>
        {form && (
          <WorkspaceLimitsDialogContent
            draft={form.draft}
            onDraftChange={(draft) => setForm({ ...form, draft })}
            invalid={form.invalid}
            pending={pending}
            error={form.error}
            descriptionId={descriptionId}
            onCancel={close}
            onSave={save}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}
