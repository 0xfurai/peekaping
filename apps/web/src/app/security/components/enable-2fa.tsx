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

const enable2FAPasswordSchema = z.object({
  currentPassword: z.string().min(1, { message: "Old password is required" }),
});
type Enable2FAPasswordFormType = z.infer<typeof enable2FAPasswordSchema>;

const enable2FASetupSchema = z.object({
  code: z.string().min(6, { message: "Code is required" }),
});
type Enable2FASetupFormType = z.infer<typeof enable2FASetupSchema>;

const Enable2FA = () => {
  const [step, setStep] = useState<"password" | "setup">("password");
  const [secret, setSecret] = useState<string | null>(null);
  const [qr, setQr] = useState<string | null>(null);
  const user = useAuthStore((s) => s.user);
  const email = user?.email || "";
  const setUser = useAuthStore((s) => s.setUser);

  // Step 1: Verify old password
  const passwordForm = useForm<Enable2FAPasswordFormType>({
    defaultValues: { currentPassword: "" },
    resolver: zodResolver(enable2FAPasswordSchema),
  });

  // Step 2: 2FA setup
  const setupForm = useForm<Enable2FASetupFormType>({
    defaultValues: { code: "" },
    resolver: zodResolver(enable2FASetupSchema),
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
      "Failed to verify password or start 2FA setup"
    ),
  });

  const verify2FAMutation = useMutation({
    ...postAuth2FaVerifyMutation(),
    onSuccess: () => {
      toast.success("2FA enabled successfully");
      setStep("password");
      setSecret(null);
      setQr(null);
      passwordForm.reset();
      setupForm.reset();
      setUser({
        ...user,
        twofa_status: true,
      });
    },
    onError: commonMutationErrorHandler("Failed to verify 2FA code"),
  });

  const handlePasswordSubmit = (data: Enable2FAPasswordFormType) => {
    if (!email) {
      toast.error("User email not found");
      return;
    }
    setup2FAMutation.mutate({
      body: { email, password: data.currentPassword },
    });
  };

  const handle2FASubmit = (data: Enable2FASetupFormType) => {
    if (!email) {
      toast.error("User email not found");
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
                  <FormLabel>Current Password</FormLabel>
                  <FormControl>
                    <Input
                      type="password"
                      autoComplete="current-password"
                      placeholder="Enter your current password"
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <Button type="submit" disabled={setup2FAMutation.isPending}>
              {setup2FAMutation.isPending ? "Verifying..." : "Continue"}
            </Button>
          </form>
        </Form>
      )}
      {step === "setup" && (
        <div className="flex flex-col gap-4">
          {qr && (
            <div>
              <TypographyH5 className="mb-1">
                Scan this QR code with your authenticator app
              </TypographyH5>
              <div className="mb-2 bg-white p-2 rounded inline-block">
                <QRCode value={qr} size={160} />
              </div>
            </div>
          )}
          {secret && (
            <div>
              <TypographyH5 className="mb-1">
                Or enter this secret manually
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
                    <FormLabel>Authenticator Code</FormLabel>
                    <FormControl>
                      <Input
                        type="text"
                        autoComplete="one-time-code"
                        placeholder="Enter the 6-digit code"
                        {...field}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <Button type="submit" disabled={verify2FAMutation.isPending}>
                {verify2FAMutation.isPending ? "Enabling..." : "Enable 2FA"}
              </Button>
            </form>
          </Form>
        </div>
      )}
    </div>
  );
};

export default Enable2FA;
