import { useId } from "react";
import { Copy } from "lucide-react";
import { toast } from "sonner";
import type { AdminResetLinkResponse } from "@/api/types.gen";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { useTimezone } from "@/context/timezone-context";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { formatDateToTimezone } from "@/lib/formatDateToTimezone";

// The body of the reset-link dialog. The link is a secret the server shows once, so it is held
// only while the dialog is open: a null reset shows the dialog waiting for the server (or, with an
// error, failing), never a link.
export function ResetLinkContent({
  email,
  reset,
  error,
  accountEmails,
  descriptionId,
  onClose,
}: {
  email: string;
  reset: AdminResetLinkResponse | null;
  error?: string | null;
  // Whether the install sends account emails, in which case the user is told a link was made
  accountEmails: boolean;
  descriptionId?: string;
  onClose: () => void;
}) {
  const { t } = useLocalizedTranslation();
  const { timezone } = useTimezone();

  const copy = async () => {
    if (!reset) return;
    try {
      await navigator.clipboard.writeText(reset.link);
      toast.success(t("admin.users.reset_link.copied"));
    } catch {
      // navigator.clipboard is missing on a page served over plain http
      toast.error(t("admin.users.reset_link.copy_failed"));
    }
  };

  return (
    <div className="flex flex-col gap-4">
      <p id={descriptionId} className="text-sm text-muted-foreground">
        {t("admin.users.reset_link.description", { email })}
      </p>
      {error && (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}
      {!reset && !error && (
        <p className="text-sm text-muted-foreground">{t("admin.users.reset_link.making")}</p>
      )}
      {reset && (
        <>
          <div className="flex flex-col gap-1">
            <Label>{t("admin.users.reset_link.label")}</Label>
            <div className="flex items-center gap-2">
              <code
                data-testid="reset-link"
                className="rounded bg-muted px-2 py-1 font-mono text-sm break-all"
              >
                {reset.link}
              </code>
              <Button
                type="button"
                variant="outline"
                size="icon"
                aria-label={t("admin.users.reset_link.copy")}
                title={t("admin.users.reset_link.copy")}
                onClick={copy}
              >
                <Copy />
              </Button>
            </div>
          </div>
          <ul className="list-disc space-y-1 pl-5 text-sm text-muted-foreground">
            <li>
              {t("admin.users.reset_link.expires", {
                when: formatDateToTimezone(reset.expiresAt, timezone),
              })}
            </li>
            <li>{t("admin.users.reset_link.signs_out")}</li>
            {accountEmails && <li>{t("admin.users.reset_link.emailed")}</li>}
          </ul>
        </>
      )}
      <div className="flex justify-end">
        <Button type="button" variant="outline" onClick={onClose}>
          {t("admin.users.reset_link.close")}
        </Button>
      </div>
    </div>
  );
}

// Shows a reset link just made for a user, once. The owner holds the request: closing the dialog
// forgets the link.
export function ResetLinkDialog({
  email,
  open,
  reset,
  error,
  accountEmails,
  onClose,
}: {
  email: string;
  open: boolean;
  reset: AdminResetLinkResponse | null;
  error: string | null;
  accountEmails: boolean;
  onClose: () => void;
}) {
  const { t } = useLocalizedTranslation();
  const descriptionId = useId();
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent aria-describedby={descriptionId}>
        <DialogHeader>
          <DialogTitle>{t("admin.users.reset_link.title")}</DialogTitle>
        </DialogHeader>
        <ResetLinkContent
          email={email}
          reset={reset}
          error={error}
          accountEmails={accountEmails}
          descriptionId={descriptionId}
          onClose={onClose}
        />
      </DialogContent>
    </Dialog>
  );
}
