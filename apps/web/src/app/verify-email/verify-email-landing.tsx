import { useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { getMeQueryKey, postAuthVerifyEmailMutation } from "@/api/@tanstack/react-query.gen";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useResendVerification } from "@/hooks/useResendVerification";
import { accountEmailFailure } from "@/lib/account-email";
import { useAuthStore } from "@/store/auth";

export type VerifyOutcome = "pending" | "verified" | "invalid_link" | "rate_limited" | "error";

// The page a verification link opens (/verify-email/:token), signed in or not. It spends the
// token as it loads, once.
export function VerifyEmailLanding({ token }: { token: string }) {
  const signedIn = useAuthStore((state) => Boolean(state.accessToken));
  const queryClient = useQueryClient();
  const [outcome, setOutcome] = useState<VerifyOutcome>("pending");
  const started = useRef(false);

  const verify = useMutation({
    ...postAuthVerifyEmailMutation(),
    onSuccess: () => {
      setOutcome("verified");
      // The banner reads /me
      if (signedIn) queryClient.invalidateQueries({ queryKey: getMeQueryKey() });
    },
    onError: (error) => {
      const failure = accountEmailFailure(error);
      setOutcome(failure === "invalid_link" || failure === "rate_limited" ? failure : "error");
    },
  });

  // A token works once: a second request (React's dev double effect) would find it used
  const { mutate } = verify;
  useEffect(() => {
    if (started.current) return;
    started.current = true;
    mutate({ body: { token } });
  }, [mutate, token]);

  return <VerifyEmailView outcome={outcome} signedIn={signedIn} />;
}

function ResendVerification() {
  const { t } = useLocalizedTranslation();
  const { outcome, pending, resend } = useResendVerification();
  const message =
    outcome === "sent"
      ? t("auth.resend.sent")
      : outcome === "rate_limited"
        ? t("auth.errors.rate_limited")
        : outcome === "already_verified"
          ? t("auth.errors.already_verified")
          : outcome === "error"
            ? t("auth.errors.generic")
            : null;
  return (
    <div className="grid gap-3">
      <Button type="button" className="w-full" disabled={pending} onClick={resend}>
        {t("auth.resend.button")}
      </Button>
      {message && (
        <p role="status" className="text-center text-sm text-muted-foreground">
          {message}
        </p>
      )}
    </div>
  );
}

export function VerifyEmailView({ outcome, signedIn }: { outcome: VerifyOutcome; signedIn: boolean }) {
  const { t } = useLocalizedTranslation();

  const message = {
    pending: t("auth.verify.verifying"),
    verified: t("auth.verify.success"),
    invalid_link: t("auth.verify.invalid_link"),
    rate_limited: t("auth.errors.rate_limited"),
    error: t("auth.errors.generic"),
  }[outcome];

  return (
    <Card>
      <CardHeader className="text-center">
        <CardTitle className="text-xl">{t("auth.verify.title")}</CardTitle>
      </CardHeader>
      <CardContent className="grid gap-6">
        <p className="text-center text-sm">{message}</p>
        {outcome === "verified" && (
          <Button asChild className="w-full">
            <Link to="/">{t("auth.verify.open_app")}</Link>
          </Button>
        )}
        {outcome === "invalid_link" && signedIn && <ResendVerification />}
        {outcome !== "pending" && outcome !== "verified" && !signedIn && (
          <Button asChild variant="outline" className="w-full">
            <Link to="/login">{t("auth.reset.sign_in")}</Link>
          </Button>
        )}
      </CardContent>
    </Card>
  );
}
