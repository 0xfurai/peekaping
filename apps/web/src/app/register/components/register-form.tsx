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
import { postAuthRegisterMutation } from "@/api/@tanstack/react-query.gen";
import { useMutation } from "@tanstack/react-query";
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
import { useSmartRedirect } from "@/hooks/use-smart-redirect";

const createFormSchema = (t: (key: string) => string) => z
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

type FormValues = z.infer<ReturnType<typeof createFormSchema>>;

export function RegisterForm({
    className,
    ...props
}: React.ComponentPropsWithoutRef<"div">) {
    const { t } = useLocalizedTranslation();
    const [serverError, setServerError] = React.useState<string | null>(null);
    const setTokens = useAuthStore(
        (state: {
            setTokens: (accessToken: string, refreshToken: string) => void;
        }) => state.setTokens
    );
    const setUser = useAuthStore((state: { setUser: (user: AuthModel | null) => void }) => state.setUser);
    const { handleRedirect } = useSmartRedirect();

    const registerMutation = useMutation({
        ...postAuthRegisterMutation(),
        onSuccess: (response) => {
            if (response.data?.accessToken && response.data?.refreshToken) {
                setTokens(response.data.accessToken, response.data.refreshToken);
                setUser(response.data.user ?? null);
                toast.success(t("messages.register_success"));
            } else {
                toast.error(t("messages.register_no_tokens"));
            }
        },
        onError: (error) => {
            if (isAxiosError(error)) {
                const errorMessage =
                    error.response?.data.message ||
                    t("messages.register_error");
                setServerError(errorMessage);
                toast.error(errorMessage);
            } else {
                const errorMessage = t("messages.unexpected_error");
                setServerError(errorMessage);
                console.error(error);
            }
        },
    });

    const formSchema = React.useMemo(() => createFormSchema(t), [t]);

    const form = useForm<FormValues>({
        resolver: zodResolver(formSchema),
        defaultValues: {
            email: "",
            password: "",
            confirmPassword: "",
        },
    });

    function onSubmit(data: FormValues) {
        setServerError(null);
        registerMutation.mutate({
            body: data,
        });
    }

    return (
        <div className={cn("flex flex-col gap-6", className)} {...props}>
            <Card>
                <CardHeader className="text-center">
                    <CardTitle className="text-xl">{t("auth.register.title")}</CardTitle>
                    <CardDescription>{t("auth.register.description")}</CardDescription>
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

                            {serverError && (
                                <Alert variant="destructive">
                                    <AlertCircle className="h-4 w-4" />
                                    <AlertTitle>{t("common.error")}</AlertTitle>
                                    <AlertDescription>{serverError}</AlertDescription>
                                </Alert>
                            )}

                            <Button type="submit" className="w-full">
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
