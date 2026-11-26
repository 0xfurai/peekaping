import { useState } from "react";
import { Link } from "react-router-dom";
import { useMutation } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { AlertCircle, Loader2 } from "lucide-react";
import { postAuthResetPasswordMutation } from "@/api/@tanstack/react-query.gen";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { PasswordInput } from "@/components/ui/password-input";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { accountEmailFailure } from "@/lib/account-email";
import { createNewPasswordSchema } from "@/lib/password";
import { useAuthStore } from "@/store/auth";

// The signup page's password rules
const createResetPasswordSchema = (t: (key: string) => string) =>
  z
    .object({
      password: createNewPasswordSchema(t),
      confirmPassword: z.string().min(8, t("forms.validation.password_min_length")),
    })
    .refine((data) => data.password === data.confirmPassword, {
      message: t("forms.validation.passwords_mismatch"),
      path: ["confirmPassword"],
    });

type ResetPasswordFormValues = z.infer<ReturnType<typeof createResetPasswordSchema>>;

export type ResetPasswordOutcome = "idle" | "done" | "invalid_link" | "rate_limited" | "error";

export function ResetPasswordForm({ token }: { token: string }) {
  const [outcome, setOutcome] = useState<ResetPasswordOutcome>("idle");
  const clearTokens = useAuthStore((state) => state.clearTokens);

  const reset = useMutation({
    ...postAuthResetPasswordMutation(),
    onSuccess: () => {
      // The server ended every session, this browser's included: drop the tokens it holds so the
      // app doesn't try to refresh them
      clearTokens();
      setOutcome("done");
    },
    onError: (error) => {
      const failure = accountEmailFailure(error);
      setOutcome(failure === "invalid_link" || failure === "rate_limited" ? failure : "error");
    },
  });

  return (
    <ResetPasswordView
      outcome={outcome}
      pending={reset.isPending}
      onSubmit={(password) => {
        setOutcome("idle");
        reset.mutate({ body: { token, password } });
      }}
    />
  );
}

export function ResetPasswordView({
  outcome,
  pending,
  onSubmit,
}: {
  outcome: ResetPasswordOutcome;
  pending: boolean;
  onSubmit: (password: string) => void;
}) {
  const { t } = useLocalizedTranslation();
  const form = useForm<ResetPasswordFormValues>({
    resolver: zodResolver(createResetPasswordSchema(t)),
    defaultValues: { password: "", confirmPassword: "" },
  });

  if (outcome === "done" || outcome === "invalid_link") {
    const done = outcome === "done";
    return (
      <Card>
        <CardHeader className="text-center">
          <CardTitle className="text-xl">{t("auth.reset.title")}</CardTitle>
        </CardHeader>
        <CardContent className="grid gap-6">
          <p className="text-center text-sm">
            {t(done ? "auth.reset.success" : "auth.reset.invalid_link")}
          </p>
          <Button asChild className="w-full">
            <Link to={done ? "/login" : "/forgot-password"}>
              {t(done ? "auth.reset.sign_in" : "auth.reset.request_new")}
            </Link>
          </Button>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader className="text-center">
        <CardTitle className="text-xl">{t("auth.reset.title")}</CardTitle>
        <CardDescription>{t("auth.reset.description")}</CardDescription>
      </CardHeader>
      <CardContent>
        <Form {...form}>
          <form
            onSubmit={form.handleSubmit((values) => onSubmit(values.password))}
            className="grid gap-6"
          >
            <FormField
              control={form.control}
              name="password"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("forms.labels.password")}</FormLabel>
                  <FormControl>
                    <PasswordInput {...field} placeholder="********" autoComplete="new-password" />
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
                    <PasswordInput {...field} placeholder="********" autoComplete="new-password" />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            {(outcome === "rate_limited" || outcome === "error") && (
              <Alert variant="destructive">
                <AlertCircle className="h-4 w-4" />
                <AlertTitle>{t("common.error")}</AlertTitle>
                <AlertDescription>
                  {t(outcome === "rate_limited" ? "auth.errors.rate_limited" : "auth.errors.generic")}
                </AlertDescription>
              </Alert>
            )}

            <Button type="submit" className="w-full" disabled={pending}>
              {pending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              {t("auth.reset.submit")}
            </Button>
          </form>
        </Form>
      </CardContent>
    </Card>
  );
}
