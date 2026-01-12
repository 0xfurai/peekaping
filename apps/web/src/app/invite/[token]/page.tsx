import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useNavigate, useParams } from "react-router-dom";
import { getInvitationsByToken, postInvitationsByTokenAccept } from "@/api/sdk.gen";
import { Loader2, GalleryVerticalEnd } from "lucide-react";
import { toast } from "sonner";
import { useAuthStore } from "@/store/auth";
import { LanguageSelector } from "@/components/LanguageSelector";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

export default function InvitationPage() {
    const { token } = useParams<{ token: string }>();
    const navigate = useNavigate();
    const { t } = useLocalizedTranslation();
    const { accessToken } = useAuthStore();
    const isAuthenticated = !!accessToken;

    const { data, isLoading, error } = useQuery({
        queryKey: ["invitation", token],
        queryFn: () => getInvitationsByToken({ path: { token: token! } }),
        enabled: !!token,
        retry: false,
    });

    const acceptMutation = useMutation({
        mutationFn: () => postInvitationsByTokenAccept({ path: { token: token! } }),
        onSuccess: () => {
            toast.success(t("onboarding.invitation_accepted"));
            navigate("/"); // Go to dashboard/home, which should show the new org
        },
        onError: (err) => {
            toast.error(t("onboarding.invitation_failed"));
            console.error(err);
        }
    });

    const handleAccept = () => {
        acceptMutation.mutate();
    };

    const handleLogin = () => {
        // Redirect to login with return url
        navigate(`/login?returnUrl=/invite/${token}`);
    };

    if (isLoading) {
        return (
            <div className="flex h-screen items-center justify-center">
                <Loader2 className="h-8 w-8 animate-spin" />
            </div>
        );
    }

    if (error || !data?.data) {
        return (
            <div className="flex h-screen items-center justify-center bg-muted/40 p-4">
                <Card className="w-full max-w-md">
                    <CardHeader>
                        <CardTitle className="text-red-500">{t("invitation.invalid_title")}</CardTitle>
                        <CardDescription>
                            {t("invitation.invalid_description")}
                        </CardDescription>
                    </CardHeader>
                    <CardContent>
                        <Button onClick={() => navigate("/")} variant="outline" className="w-full">
                            {t("invitation.go_home")}
                        </Button>
                    </CardContent>
                </Card>
            </div>
        );
    }

    const invitation = data.data?.data;

    return (
        <div className="flex min-h-svh flex-col items-center justify-center gap-6 bg-muted p-6 md:p-10">
            <div className="flex w-full max-w-sm flex-col gap-6">
                <a href="#" className="flex items-center gap-2 self-center font-medium">
                    <div className="flex h-6 w-6 items-center justify-center rounded-md bg-primary text-primary-foreground">
                        <GalleryVerticalEnd className="size-4" />
                    </div>
                    Vigi
                </a>

                <Card>
                    <CardHeader className="text-center pb-2">
                        <div className="mx-auto mb-4 flex h-16 w-16 items-center justify-center rounded-xl bg-primary text-3xl font-bold text-primary-foreground">
                            {invitation.organization?.name?.substring(0, 1).toUpperCase() || "O"}
                        </div>
                        <CardTitle className="text-xl">{t("invitation.invited_title")}</CardTitle>
                        <CardDescription>
                            {t("invitation.join_description")} <strong>{invitation.organization?.name}</strong> {t("onboarding.invited_as").toLowerCase()} <span className="capitalize">{invitation.role}</span>
                        </CardDescription>
                    </CardHeader>
                    <CardContent className="space-y-4 pt-2">
                        {!isAuthenticated ? (
                            <div className="space-y-3">
                                <p className="text-sm text-center text-muted-foreground">
                                    {t("invitation.login_to_accept_text")}
                                </p>
                                <Button onClick={handleLogin} className="w-full">
                                    {t("invitation.login_button")}
                                </Button>

                                <div className="relative text-center text-sm after:absolute after:inset-0 after:top-1/2 after:z-0 after:flex after:items-center after:border-t after:border-border">
                                    <span className="relative z-10 bg-background px-2 text-muted-foreground">
                                        {t("invitation.or")}
                                    </span>
                                </div>

                                <Button onClick={() => navigate(`/register?returnUrl=/invite/${token}`)} variant="outline" className="w-full">
                                    {t("invitation.create_account_button")}
                                </Button>
                            </div>
                        ) : (
                            <div className="space-y-4">
                                <div className="rounded-md bg-muted/50 p-3 text-center text-sm text-muted-foreground">
                                    {t("invitation.logged_in_as")} <span className="font-medium text-foreground">{useAuthStore.getState().user?.email}</span>
                                </div>
                                <Button
                                    onClick={handleAccept}
                                    disabled={acceptMutation.isPending}
                                    className="w-full"
                                >
                                    {acceptMutation.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
                                    {t("onboarding.accept_invitation")}
                                </Button>
                            </div>
                        )}
                    </CardContent>
                </Card>
                <div className="w-[180px] self-center">
                    <LanguageSelector />
                </div>
            </div>
        </div>
    );
}
