import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
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
import { postAuthLoginMutation } from "@/api/@tanstack/react-query.gen";
import { useMutation } from "@tanstack/react-query";
import { toast } from "sonner";
import { isAxiosError } from "axios";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { AlertCircle, Loader2 } from "lucide-react";
import React from "react";
import { useAuthStore } from "@/store/auth";
import type { AuthModel } from "@/api/types.gen";
import { TwoFADialog } from "./two-fa-dialog";
import { Link } from "react-router-dom";

const formSchema = z.object({
  email: z.string().email("Please enter a valid email address"),
  password: z.string().min(1, "Password is required"),
});

type FormValues = z.infer<typeof formSchema>;

export function LoginForm({
  className,
  ...props
}: React.ComponentPropsWithoutRef<"div">) {
  const [serverError, setServerError] = React.useState<string | null>(null);
  const setTokens = useAuthStore(
    (state: {
      setTokens: (accessToken: string, refreshToken: string) => void;
    }) => state.setTokens
  );
  const setUser = useAuthStore(
    (state: { setUser: (user: AuthModel | null) => void }) => state.setUser
  );
  const [show2FAPrompt, setShow2FAPrompt] = React.useState(false);
  const [verifying2FA, setVerifying2FA] = React.useState(false);

  const loginMutation = useMutation({
    ...postAuthLoginMutation(),
    onSuccess: (response) => {
      const { accessToken, refreshToken, user } = response.data || {};

      if (accessToken && refreshToken) {
        setTokens(accessToken, refreshToken);
        setUser(user ?? null);
        toast.success("Logged in successfully");
        setShow2FAPrompt(false);
      } else {
        toast.error("Login successful but no tokens received");
      }
      setVerifying2FA(false);
    },
    onError: (error) => {
      if (isAxiosError(error)) {
        const errorMessage =
          error.response?.data.message || "An error occurred during login";

        if (errorMessage === "2FA token required") {
          setShow2FAPrompt(true);
        } else {
          setServerError(errorMessage);
          toast.error(errorMessage);
        }
      } else {
        const errorMessage = "An unexpected error occurred";
        setServerError(errorMessage);
        console.error(error);
      }
      setVerifying2FA(false);
    },
  });

  const form = useForm<FormValues>({
    resolver: zodResolver(formSchema),
    defaultValues: {
      email: "",
      password: "",
    },
  });

  function onSubmit(data: FormValues) {
    setServerError(null);
    loginMutation.mutate({
      body: data,
    });
  }

  return (
    <div className={cn("flex flex-col gap-6", className)} {...props}>
      {show2FAPrompt ? (
        <TwoFADialog
          email={form.getValues("email")}
          password={form.getValues("password")}
          onSubmit={({ email, password, code }) => {
            setServerError(null);
            setVerifying2FA(true);
            loginMutation.mutate({
              body: { email, password, token: code },
            });
          }}
          error={serverError}
          loading={verifying2FA}
        />
      ) : (
        <Card>
          <CardHeader className="text-center">
            <CardTitle className="text-xl">Welcome back</CardTitle>
            <CardDescription>Login with your Email</CardDescription>
          </CardHeader>
          <CardContent>
            <Form {...form}>
              <form
                onSubmit={form.handleSubmit(onSubmit)}
                className="grid gap-6"
              >
                <FormField
                  control={form.control}
                  name="email"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>Email</FormLabel>
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
                      <FormLabel>Password</FormLabel>
                      <FormControl>
                        <Input
                          type="password"
                          {...field}
                          placeholder="********"
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                {serverError && (
                  <Alert variant="destructive">
                    <AlertCircle className="h-4 w-4" />
                    <AlertTitle>Error</AlertTitle>
                    <AlertDescription>{serverError}</AlertDescription>
                  </Alert>
                )}

                <Button type="submit" className="w-full">
                  {loginMutation.isPending && (
                    <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                  )}
                  Login
                </Button>

                <div className="text-center text-sm text-muted-foreground">
                  Don't have an account?{" "}
                  <Link
                    to="/register"
                    className="font-medium text-primary hover:underline"
                  >
                    Sign up
                  </Link>
                </div>
              </form>
            </Form>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
