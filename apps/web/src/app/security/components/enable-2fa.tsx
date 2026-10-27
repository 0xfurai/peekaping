import {
  Form,
  FormField,
  FormItem,
  FormLabel,
  FormControl,
  FormMessage,
} from "@/components/ui/form";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { z } from "zod";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import {
  postAuth2FaSetupMutation,
  postAuth2FaVerifyMutation,
} from "@/api/@tanstack/react-query.gen";
import { toast } from "sonner";
import { useMutation } from "@tanstack/react-query";
import { TypographyH4, TypographyH5 } from "@/components/ui/typography";
import { useState } from "react";
import { useAuthStore } from "@/store/auth";
import QRCode from "react-qr-code";
import { commonMutationErrorHandler } from "@/lib/utils";
import { PasswordInput } from "@/components/ui/password-input";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const enable2FaPasswordSchema = z.object({
  currentPassword: z.string().min(1, { message: "Old password is required" }),
});
type Enable2FaPasswordFormValues = z.infer<typeof enable2FaPasswordSchema>;

const enable2FaSetupSchema = z.object({
  code: z.string().min(6, { message: "Code is required" }),
});
type Enable2FaSetupFormValues = z.infer<typeof enable2FaSetupSchema>;

const Enable2FA = () => {
  const [step, setStep] = useState<"password" | "setup">("password");
  const [secret, setSecret] = useState<string | null>(null);
  const [qr, setQr] = useState<string | null>(null);
  const user = useAuthStore((s) => s.user);
  const email = user?.email || "";
  const setUser = useAuthStore((s) => s.setUser);
  const { t } = useLocalizedTranslation();

  // Step 1: Verify old password
  const passwordForm = useForm<Enable2FaPasswordFormValues>({
    defaultValues: { currentPassword: "" },
    resolver: zodResolver(enable2FaPasswordSchema),
  });

  // Step 2: 2FA setup
  const setupForm = useForm<Enable2FaSetupFormValues>({
    defaultValues: { code: "" },
    resolver: zodResolver(enable2FaSetupSchema),
  });

  const setup2FAMutation = useMutation({
    ...postAuth2FaSetupMutation(),
    onSuccess: (data) => {
      if (typeof data === "object" && data !== null) {
        const d = data as {
          secret?: string;
          twofa_secret?: string;
          provisioningUri?: string;
          qr?: string;
          qr_code?: string;
        };
        setSecret(d.secret || d.twofa_secret || null);
        setQr(d.provisioningUri || d.qr || d.qr_code || null);
        setStep("setup");
      }
    },
    onError: commonMutationErrorHandler(
      t("security.enable_2fa.messages.failed_to_verify_password")
    ),
  });

  const verify2FAMutation = useMutation({
    ...postAuth2FaVerifyMutation(),
    onSuccess: (data) => {
      // The server answers a wrong code with 400, so this is only a guard.
      if (!data.success) {
        toast.error(t("security.enable_2fa.messages.failed_to_verify_2fa_code"));
        return;
      }
      toast.success(t("security.enable_2fa.messages.2fa_enabled_successfully"));
      setStep("password");
      setSecret(null);
      setQr(null);
      passwordForm.reset();
      setupForm.reset();
      if (user) {
        setUser({ ...user, twofa_status: true });
      }
    },
    onError: commonMutationErrorHandler(t("security.enable_2fa.messages.failed_to_verify_2fa_code")),
  });

  const handlePasswordSubmit = (data: Enable2FaPasswordFormValues) => {
    if (!email) {
      toast.error(t("security.enable_2fa.messages.user_email_not_found"));
      return;
    }
    setup2FAMutation.mutate({
      body: { email, password: data.currentPassword },
    });
  };

  const handle2FASubmit = (data: Enable2FaSetupFormValues) => {
    if (!email) {
      toast.error(t("security.enable_2fa.messages.user_email_not_found"));
      return;
    }
    verify2FAMutation.mutate({
      body: { code: data.code, email },
    });
  };

  return (
    <div className="flex flex-col gap-4 mt-8">
      <TypographyH4>Enable Two-Factor Authentication (2FA)</TypographyH4>
      {step === "password" && (
        <Form {...passwordForm}>
          <form
            onSubmit={passwordForm.handleSubmit(handlePasswordSubmit)}
            className="space-y-6 max-w-[600px]"
          >
            <FormField
              control={passwordForm.control}
              name="currentPassword"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("security.enable_2fa.form.current_password_label")}</FormLabel>
                  <FormControl>
                    <PasswordInput
                      autoComplete="current-password"
                      placeholder={t("security.enable_2fa.form.current_password_placeholder")}
                      {...field}
                    />
                  </FormControl>

                  <FormMessage />
                </FormItem>
              )}
            />
            <Button type="submit" disabled={setup2FAMutation.isPending}>
              {setup2FAMutation.isPending ? t("common.verifying") : t("common.continue")}
            </Button>
          </form>
        </Form>
      )}
      {step === "setup" && (
        <div className="flex flex-col gap-4">
          {qr && (
            <div>
              <TypographyH5 className="mb-1">
                {t("security.enable_2fa.messages.scan_qr_code_with_authenticator_app")}
              </TypographyH5>
              <div className="mb-2 bg-white p-2 rounded inline-block">
                <QRCode value={qr} size={160} />
              </div>
            </div>
          )}
          {secret && (
            <div>
              <TypographyH5 className="mb-1">
                {t("security.enable_2fa.messages.or_enter_secret_manually")}
              </TypographyH5>
              <div className="bg-muted p-2 rounded font-mono select-all inline-block">
                {secret}
              </div>
            </div>
          )}
          <Form {...setupForm}>
            <form
              onSubmit={setupForm.handleSubmit(handle2FASubmit)}
              className="space-y-6 max-w-[600px]"
            >
              <FormField
                control={setupForm.control}
                name="code"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t("security.enable_2fa.form.authenticator_code_label")}</FormLabel>
                    <FormControl>
                      <Input
                        type="text"
                        autoComplete="one-time-code"
                        placeholder={t("security.enable_2fa.form.authenticator_code_placeholder")}
                        {...field}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <Button type="submit" disabled={verify2FAMutation.isPending}>
                {verify2FAMutation.isPending ? t("common.enabling") : t("security.enable_2fa.form.enable_2fa_button")}
              </Button>
            </form>
          </Form>
        </div>
      )}
    </div>
  );
};

export default Enable2FA;
