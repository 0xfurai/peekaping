import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { PasswordInput } from "@/components/ui/password-input";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import {
  getErrorReportingQueryKey,
  getTelemetryQueryKey,
  postAuthRegisterMutation,
  putErrorReportingMutation,
  putTelemetryMutation,
} from "@/api/@tanstack/react-query.gen";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { isAxiosError } from "axios";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { AlertCircle } from "lucide-react";
import React from "react";
import { AlertTitle } from "@/components/ui/alert";
import { useAuthStore } from "@/store/auth";
import type { AuthModel } from "@/api/types.gen";
import { Link } from "react-router-dom";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { TelemetryDocsLink } from "@/components/telemetry-docs-link";
import { TurnstileWidget } from "@/components/turnstile-widget";
import {
  canSubmitRegister,
  registerBody,
  registerDescriptionKey,
  registerFailureKey,
  shownRegisterFailure,
  showInstallPreferences,
  type RegisterFailure,
} from "@/lib/signup";

const createRegisterSchema = (t: (key: string) => string) => z
  .object({
    email: z.string().email(t("forms.validation.email_invalid")),
    password: z.string().min(8, t("forms.validation.password_min_length")),
    confirmPassword: z
      .string()
      .min(8, t("forms.validation.password_min_length")),
  })
  .refine((data) => data.password === data.confirmPassword, {
    message: t("forms.validation.passwords_mismatch"),
    path: ["confirmPassword"],
  });

type RegisterFormValues = z.infer<ReturnType<typeof createRegisterSchema>>;

// The answer to a failed signup. When the address has an account, a link to sign in follows it.
export function RegisterErrorAlert({ message, signInLink }: { message: string; signInLink: boolean }) {
  const { t } = useLocalizedTranslation();
  return (
    <Alert variant="destructive">
      <AlertCircle className="h-4 w-4" />
      <AlertTitle>{t("common.error")}</AlertTitle>
      <AlertDescription>
        {message}
        {signInLink && (
          <>
            {" "}
            <Link to="/login" className="font-medium underline">
              {t("auth.register.errors.sign_in_instead")}
            </Link>
          </>
        )}
      </AlertDescription>
    </Alert>
  );
}

export function RegisterForm({
  className,
  turnstileSiteKey,
  signupMode,
  firstSignup,
  ...props
}: React.ComponentPropsWithoutRef<"div"> & {
  // The key of the Turnstile widget the install asks for, or null without one
  turnstileSiteKey?: string | null;
  // The install's signup mode. Open mode signs up strangers, who are not admins after the first.
  signupMode?: "invite" | "open";
  // Whether the install has no user yet, so that this signup becomes its instance admin
  firstSignup?: boolean;
}) {
  const { t } = useLocalizedTranslation();
  // The install's first signup becomes its admin and chooses for the install; anyone else may not
  const installPreferences = showInstallPreferences(firstSignup);
  // A refused signup's message. One the visitor can fix by signing in gets a link beside it.
  const [failure, setFailure] = React.useState<RegisterFailure | null>(null);
  const [turnstileToken, setTurnstileToken] = React.useState<string | null>(null);
  const [turnstileReset, setTurnstileReset] = React.useState(0);
  const setTokens = useAuthStore(
    (state: {
      setTokens: (accessToken: string, refreshToken: string) => void;
    }) => state.setTokens
  );
  const setUser = useAuthStore((state: { setUser: (user: AuthModel | null) => void }) => state.setUser);
  const queryClient = useQueryClient();
  // On until the admin unticks it (ADR 0003). It is not part of the register request.
  const [sendUsageStatistics, setSendUsageStatistics] = React.useState(true);
  // Its own choice, on until unticked, and independent of the usage statistics.
  const [sendErrorReports, setSendErrorReports] = React.useState(true);

  const telemetryMutation = useMutation({
    ...putTelemetryMutation(),
    // The answer says the upgrade notice is out of the way, so the app doesn't show it
    onSuccess: (state) => {
      queryClient.setQueryData(getTelemetryQueryKey(), state);
    },
    // Setup has already succeeded, so a failure here only costs the admin a visit to Settings.
    // The toast tells them, as the console is not where they look.
    onError: (error) => {
      console.error("Failed to save the usage statistics choice", error);
      toast.warning(t("telemetry.not_saved"));
    },
  });

  const errorReportingMutation = useMutation({
    ...putErrorReportingMutation(),
    onSuccess: (state) => {
      queryClient.setQueryData(getErrorReportingQueryKey(), state);
      // The notice is part of the usage statistics state, and this choice answered it too
      queryClient.invalidateQueries({ queryKey: getTelemetryQueryKey() });
    },
    // Like the usage statistics: setup has succeeded, and Settings is where to try again.
    onError: (error) => {
      console.error("Failed to save the error reports choice", error);
      toast.warning(t("error_reporting.not_saved"));
    },
  });

  const registerMutation = useMutation({
    ...postAuthRegisterMutation(),
    onSuccess: (response) => {
      if (response.data?.accessToken && response.data?.refreshToken) {
        setTokens(response.data.accessToken, response.data.refreshToken);
        setUser(response.data.user ?? null);
        if (installPreferences) {
          // Storing the tokens signs the API client in, so these requests are the admin's. Both
          // send the choice, on or off: an unticked box is an opt-out to store, not nothing.
          telemetryMutation.mutate({ body: { enabled: sendUsageStatistics } });
          errorReportingMutation.mutate({ body: { enabled: sendErrorReports } });
        }
        toast.success(t("messages.register_success"));
      } else {
        toast.error(t("messages.register_no_tokens"));
      }
    },
    onError: (error, variables) => {
      // A token is good for one check, so a refused signup needs a new one
      if (turnstileSiteKey) {
        setTurnstileToken(null);
        setTurnstileReset((n) => n + 1);
      }
      const email = variables.body.email;
      const failureKey = registerFailureKey(error);
      if (failureKey) {
        const errorMessage = t(failureKey);
        setFailure({ message: errorMessage, emailTaken: failureKey === "auth.register.errors.email_taken", email });
        toast.error(errorMessage);
      } else if (isAxiosError(error)) {
        const errorMessage =
          error.response?.data.message ||
          t("messages.register_error");
        setFailure({ message: errorMessage, emailTaken: false, email });
        toast.error(errorMessage);
      } else {
        const errorMessage = t("messages.unexpected_error");
        setFailure({ message: errorMessage, emailTaken: false, email });
        console.error(error);
      }
    },
  });

  const formSchema = React.useMemo(() => createRegisterSchema(t), [t]);
  
  const form = useForm<RegisterFormValues>({
    resolver: zodResolver(formSchema),
    defaultValues: {
      email: "",
      password: "",
      confirmPassword: "",
    },
  });

  // The refused signup shows while the email field holds its address
  const shownFailure = shownRegisterFailure(failure, form.watch("email"));

  function onSubmit(data: RegisterFormValues) {
    setFailure(null);
    registerMutation.mutate({
      body: registerBody(data, turnstileSiteKey ? turnstileToken : null),
    });
  }

  return (
    <div className={cn("flex flex-col gap-6", className)} {...props}>
      <Card>
        <CardHeader className="text-center">
          <CardTitle className="text-xl">{t("auth.register.title")}</CardTitle>
          <CardDescription>{t(registerDescriptionKey(signupMode, firstSignup))}</CardDescription>
        </CardHeader>

        <CardContent>
          <Form {...form}>
            <form onSubmit={form.handleSubmit(onSubmit)} className="grid gap-6">
              <FormField
                control={form.control}
                name="email"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t("forms.labels.email")}</FormLabel>
                    <FormControl>
                      <Input
                        placeholder="example@example.com"
                        type="email"
                        {...field}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name="password"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t("forms.labels.password")}</FormLabel>
                    <FormControl>
                      <PasswordInput {...field} placeholder="********" />
                    </FormControl>
                    <FormMessage />
                  </FormItem>

                )}
              />

              <FormField
                control={form.control}
                name="confirmPassword"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t("forms.labels.confirm_password")}</FormLabel>
                    <FormControl>
                      <PasswordInput {...field} placeholder="********" />
                    </FormControl>
                    <FormMessage />
                  </FormItem>

                )}
              />

              {installPreferences && (
                <div className="grid gap-2">
                  <div className="flex items-center gap-2">
                    <Checkbox
                      id="send-usage-statistics"
                      checked={sendUsageStatistics}
                      onCheckedChange={(checked) =>
                        setSendUsageStatistics(checked === true)
                      }
                    />
                    <Label
                      htmlFor="send-usage-statistics"
                      className="text-xs text-muted-foreground font-normal"
                    >
                      {t("telemetry.label")}
                    </Label>
                  </div>
                  <div className="flex items-center gap-2">
                    <Checkbox
                      id="send-error-reports"
                      checked={sendErrorReports}
                      onCheckedChange={(checked) =>
                        setSendErrorReports(checked === true)
                      }
                    />
                    <Label
                      htmlFor="send-error-reports"
                      className="text-xs text-muted-foreground font-normal"
                    >
                      {t("error_reporting.label")}
                    </Label>
                  </div>
                  <p className="text-xs text-muted-foreground">
                    {t("auth.register.telemetry_note")}{" "}
                    <TelemetryDocsLink>
                      {t("auth.register.telemetry_whats_sent")}
                    </TelemetryDocsLink>
                  </p>
                </div>
              )}

              {turnstileSiteKey && (
                <TurnstileWidget
                  siteKey={turnstileSiteKey}
                  onToken={setTurnstileToken}
                  resetKey={turnstileReset}
                />
              )}

              {shownFailure && <RegisterErrorAlert message={shownFailure.message} signInLink={shownFailure.emailTaken} />}

              <Button
                type="submit"
                className="w-full"
                disabled={!canSubmitRegister(turnstileSiteKey, turnstileToken, registerMutation.isPending)}
              >
                {t("auth.register.submit")}
              </Button>

              <div className="text-center text-sm text-muted-foreground">
                {t("auth.register.have_account")}{" "}
                <Link
                  to="/login"
                  className="font-medium text-primary hover:underline"
                >
                  {t("auth.register.sign_in")}
                </Link>
              </div>
            </form>
          </Form>
        </CardContent>
      </Card>
    </div>
  );
}
