import { Loader2, MailWarning } from "lucide-react";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useMe } from "@/hooks/useMe";
import { useResendVerification, type ResendOutcome } from "@/hooks/useResendVerification";
import { needsVerification } from "@/lib/account-email";

// The banner at the top of the signed-in app that asks the user to verify their address. It shows
// only on an install that sends account emails, and only while /me says the address is not verified.
export function VerificationBanner() {
  const me = useMe().data?.data;
  const { outcome, pending, resend } = useResendVerification();
  if (!me || !needsVerification(me)) return null;
  return (
    <VerificationBannerView
      email={me.user.email}
      outcome={outcome}
      pending={pending}
      onResend={resend}
    />
  );
}

export function VerificationBannerView({
  email,
  outcome,
  pending,
  onResend,
}: {
  email: string;
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
        <span>{t("auth.verification_banner.message", { email })}</span>
        <Button type="button" size="sm" variant="outline" disabled={pending} onClick={onResend}>
          {pending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
          {t("auth.resend.button")}
        </Button>
        {status && (
          <span role="status" className={outcome === "sent" ? "text-muted-foreground" : "text-destructive"}>
            {status}
          </span>
        )}
      </AlertDescription>
    </Alert>
  );
}
