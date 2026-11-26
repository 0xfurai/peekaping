import { useState } from "react";
import { Link } from "react-router-dom";
import { useMutation } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { AlertCircle, Loader2 } from "lucide-react";
import { postAuthForgotPasswordMutation } from "@/api/@tanstack/react-query.gen";
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
import { Input } from "@/components/ui/input";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { accountEmailFailure } from "@/lib/account-email";

const forgotPasswordSchema = z.object({
  email: z.string().email("forms.validation.email_invalid"),
});

type ForgotPasswordFormValues = z.infer<typeof forgotPasswordSchema>;

export type ForgotPasswordOutcome = "idle" | "sent" | "rate_limited" | "error";

// The server answers 202 for any address, known or not, so "sent" tells nothing about the account
export function ForgotPasswordForm() {
  const [outcome, setOutcome] = useState<ForgotPasswordOutcome>("idle");

  const forgot = useMutation({
    ...postAuthForgotPasswordMutation(),
    onSuccess: () => setOutcome("sent"),
    onError: (error) =>
      setOutcome(accountEmailFailure(error) === "rate_limited" ? "rate_limited" : "error"),
  });

  return (
    <ForgotPasswordView
      outcome={outcome}
      pending={forgot.isPending}
      onSubmit={(email) => {
        setOutcome("idle");
        forgot.mutate({ body: { email } });
      }}
    />
  );
}

export function ForgotPasswordView({
  outcome,
  pending,
  onSubmit,
}: {
  outcome: ForgotPasswordOutcome;
  pending: boolean;
  onSubmit: (email: string) => void;
}) {
  const { t } = useLocalizedTranslation();
  const form = useForm<ForgotPasswordFormValues>({
    resolver: zodResolver(forgotPasswordSchema),
    defaultValues: { email: "" },
  });

  const backToLogin = (
    <div className="text-center text-sm text-muted-foreground">
      <Link to="/login" className="font-medium text-primary hover:underline">
        {t("auth.forgot.back_to_login")}
      </Link>
    </div>
  );

  return (
    <Card>
      <CardHeader className="text-center">
        <CardTitle className="text-xl">{t("auth.forgot.title")}</CardTitle>
        {outcome !== "sent" && <CardDescription>{t("auth.forgot.description")}</CardDescription>}
      </CardHeader>
      <CardContent>
        {outcome === "sent" ? (
          <div className="grid gap-6">
            <p className="text-center text-sm">{t("auth.forgot.sent")}</p>
            {backToLogin}
          </div>
        ) : (
          <Form {...form}>
            <form
              onSubmit={form.handleSubmit((values) => onSubmit(values.email))}
              className="grid gap-6"
            >
              <FormField
                control={form.control}
                name="email"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t("forms.labels.email")}</FormLabel>
                    <FormControl>
                      <Input placeholder="example@example.com" type="email" {...field} />
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
                {t("auth.forgot.submit")}
              </Button>

              {backToLogin}
            </form>
          </Form>
        )}
      </CardContent>
    </Card>
  );
}
