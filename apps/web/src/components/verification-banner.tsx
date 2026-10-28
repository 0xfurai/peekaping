import { Loader2, MailWarning } from "lucide-react";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useMe } from "@/hooks/useMe";
import { useResendVerification, type ResendOutcome } from "@/hooks/useResendVerification";
import { isSuspendedUnverified, needsVerification } from "@/lib/account-email";
import { workspaceIdFromPathname } from "@/lib/workspace-path";
import { useLocation } from "react-router-dom";

// The banner at the top of the signed-in app that asks the user to verify their address. It shows
// only on an install that sends account emails, and only while /me says the address is not verified.
// In a workspace held back until its owner verifies, it says so instead (one notice, not two), also
// to a member whose own address is fine; only an unverified user gets Resend.
export function VerificationBanner() {
  const me = useMe().data?.data;
  const { pathname } = useLocation();
  const { outcome, pending, resend } = useResendVerification();
  if (!me) return null;
  const suspended = isSuspendedUnverified(me.workspaces, workspaceIdFromPathname(pathname));
  if (!suspended && !needsVerification(me)) return null;
  return (
    <VerificationBannerView
      email={me.user.email}
      suspended={suspended}
      canResend={needsVerification(me)}
      outcome={outcome}
      pending={pending}
      onResend={resend}
    />
  );
}

export function VerificationBannerView({
  email,
  suspended = false,
  canResend = true,
  outcome,
  pending,
  onResend,
}: {
  email: string;
  // The workspace is held back until its owner verifies; the message says so
  suspended?: boolean;
  canResend?: boolean;
  outcome: ResendOutcome;
  pending: boolean;
  onResend: () => void;
}) {
  const { t } = useLocalizedTranslation();
  // The address turned out to be verified already
  if (outcome === "already_verified") return null;

  const status =
    outcome === "sent"
      ? t("auth.resend.sent")
      : outcome === "rate_limited"
        ? t("auth.errors.rate_limited")
        : outcome === "error"
          ? t("auth.errors.generic")
          : null;

  return (
    <Alert>
      <MailWarning className="h-4 w-4" />
      <AlertDescription className="flex flex-wrap items-center gap-x-4 gap-y-2">
        <span>
          {suspended
            ? t("auth.suspension_notice.message")
            : t("auth.verification_banner.message", { email })}
        </span>
        {canResend && (
          <Button type="button" size="sm" variant="outline" disabled={pending} onClick={onResend}>
            {pending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            {t("auth.resend.button")}
          </Button>
        )}
        {status && (
          <span role="status" className={outcome === "sent" ? "text-muted-foreground" : "text-destructive"}>
            {status}
          </span>
        )}
      </AlertDescription>
    </Alert>
  );
}
