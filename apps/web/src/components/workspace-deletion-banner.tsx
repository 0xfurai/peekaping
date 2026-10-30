import { Loader2, Trash2 } from "lucide-react";
import { useLocation } from "react-router-dom";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { useCancelWorkspaceDeletion } from "@/hooks/useCancelWorkspaceDeletion";
import { useCurrentWorkspace } from "@/hooks/useCurrentWorkspace";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { deletionNotice, formatDeletionDate } from "@/lib/workspace-deletion";
import { workspaceIdFromPathname } from "@/lib/workspace-path";

// The banner at the top of a workspace an owner scheduled for deletion, for every member: the date
// it will be deleted. Only an owner gets Cancel deletion. The admin area is outside every workspace.
export function WorkspaceDeletionBanner() {
  const workspace = useCurrentWorkspace();
  const { pathname } = useLocation();
  const { cancel, pending } = useCancelWorkspaceDeletion();
  const notice = workspaceIdFromPathname(pathname) ? deletionNotice(workspace) : null;
  if (!notice) return null;
  return (
    <WorkspaceDeletionBannerView
      scheduledAt={notice.scheduledAt}
      canCancel={notice.canCancel}
      pending={pending}
      onCancel={cancel}
    />
  );
}

export function WorkspaceDeletionBannerView({
  scheduledAt,
  canCancel,
  pending,
  onCancel,
}: {
  scheduledAt: string;
  canCancel: boolean;
  pending: boolean;
  onCancel: () => void;
}) {
  const { t, i18n } = useLocalizedTranslation();
  const date = formatDeletionDate(scheduledAt, i18n.language);
  return (
    <Alert variant="destructive">
      <Trash2 className="h-4 w-4" />
      <AlertDescription className="flex flex-wrap items-center gap-x-4 gap-y-2">
        <span>{t(canCancel ? "workspaces.deletion.message" : "workspaces.deletion.member_message", { date })}</span>
        {canCancel && (
          <Button type="button" size="sm" variant="outline" disabled={pending} onClick={onCancel}>
            {pending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            {t("workspaces.deletion.cancel")}
          </Button>
        )}
      </AlertDescription>
    </Alert>
  );
}
