import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { getUserInvitations, getUserOrganizations, postInvitationsByTokenAccept } from "@/api/sdk.gen";
import { Badge } from "@/components/ui/badge";
import { Loader2, Check, Plus, ArrowRight } from "lucide-react";
import { toast } from "sonner";
import { useNavigate } from "react-router-dom";
import { LanguageSelector } from "@/components/LanguageSelector";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

export default function OnboardingPage() {
    const queryClient = useQueryClient();
    const navigate = useNavigate();
    const { t } = useLocalizedTranslation();

    // Fetch Invitations
    const { data: invitationsData, isLoading: isLoadingInvites } = useQuery({
        queryKey: ["user-invitations"],
        queryFn: () => getUserInvitations(),
    });

    // Fetch Organizations
    const { data: orgsData, isLoading: isLoadingOrgs } = useQuery({
        queryKey: ["user-organizations"],
        queryFn: () => getUserOrganizations(),
    });

    const invitations = (invitationsData?.data?.data || []) as any[];
    const organizations = (orgsData?.data || []) as any[];
    const isLoading = isLoadingInvites || isLoadingOrgs;

    const acceptMutation = useMutation({
        mutationFn: (data: { token: string; slug: string }) => {
            return postInvitationsByTokenAccept({ path: { token: data.token } });
        },
        onSuccess: (_data, variables) => {
            toast.success(t("onboarding.invitation_accepted"));
            queryClient.invalidateQueries({ queryKey: ["user-invitations"] });
            queryClient.invalidateQueries({ queryKey: ["user-organizations"] });

            // Redirect to the new organization dashboard
            window.location.href = `/${variables.slug}/monitors`;
        },
        onError: () => {
            toast.error(t("onboarding.invitation_failed"));
        }
    });

    const handleCreateOrg = () => {
        navigate("/create-organization");
    };

    const handleSkipToDashboard = () => {
        if (organizations.length > 0 && organizations[0].organization?.slug) {
            navigate(`/${organizations[0].organization.slug} `);
        }
    };

    if (isLoading) {
        return (
            <div className="flex h-screen items-center justify-center bg-muted/50">
                <Loader2 className="h-8 w-8 animate-spin text-primary" />
            </div>
        );
    }

    return (
        <div className="min-h-screen bg-muted/50 flex flex-col items-center justify-center p-4">
            <div className="w-full max-w-4xl space-y-8">
                <div className="text-center space-y-2">
                    <h1 className="text-3xl font-bold tracking-tight">{t("onboarding.welcome_title")}</h1>
                    <p className="text-muted-foreground">
                        {t("onboarding.welcome_description")}
                    </p>
                </div>

                <div className="grid gap-6 md:grid-cols-2">
                    {/* Invitations Section */}
                    <div className="space-y-4">
                        <h2 className="text-xl font-semibold flex items-center gap-2">
                            {t("onboarding.pending_invitations")}
                            <Badge variant="secondary" className="rounded-full">{invitations.length}</Badge>
                        </h2>
                        <div className="grid gap-4">
                            {invitations.length === 0 ? (
                                <Card>
                                    <CardContent className="pt-6 text-center text-muted-foreground">
                                        {t("onboarding.no_invitations")}
                                    </CardContent>
                                </Card>
                            ) : (
                                invitations.map((inv) => (
                                    <div key={inv.token} className="border rounded-md p-6 space-y-4 bg-muted/30 flex flex-col items-center text-center hover:bg-muted/50 transition-colors">
                                        <div className="flex h-12 w-12 items-center justify-center rounded-lg bg-primary text-primary-foreground font-bold text-lg">
                                            {inv.organization?.name?.substring(0, 1).toUpperCase() || "O"}
                                        </div>
                                        <div className="space-y-1">
                                            <div className="font-semibold text-lg">{inv.organization?.name}</div>
                                            <div className="text-sm text-muted-foreground">{t("onboarding.invited_as")} <span className="capitalize text-foreground font-medium">{inv.role}</span></div>
                                        </div>
                                        <Button
                                            className="w-full"
                                            size="sm"
                                            onClick={() => acceptMutation.mutate({
                                                token: inv.token || "",
                                                slug: inv.organization?.slug || ""
                                            })}
                                            disabled={acceptMutation.isPending}
                                        >
                                            {acceptMutation.isPending ? <Loader2 className="h-4 w-4 animate-spin mr-2" /> : <Check className="h-4 w-4 mr-2" />}
                                            {t("onboarding.accept_invitation")}
                                        </Button>
                                    </div>
                                ))
                            )}
                        </div>
                    </div>

                    {/* Actions Section */}
                    <div className="space-y-6">
                        <h2 className="text-xl font-semibold">{t("onboarding.get_started")}</h2>

                        <Card className="hover:border-primary/50 transition-colors cursor-pointer" onClick={handleCreateOrg}>
                            <CardHeader>
                                <CardTitle className="flex items-center gap-2">
                                    <Plus className="h-5 w-5 text-primary" />
                                    {t("onboarding.create_new_organization_title")}
                                </CardTitle>
                                <CardDescription>
                                    {t("onboarding.create_new_organization_desc")}
                                </CardDescription>
                            </CardHeader>
                            <CardContent>
                                <Button variant="outline" className="w-full">{t("onboarding.create_organization_button")}</Button>
                            </CardContent>
                        </Card>

                        {organizations.length > 0 && (
                            <div className="pt-4 border-t">
                                <div className="text-center space-y-4">
                                    <p className="text-sm text-muted-foreground">
                                        {organizations.length === 1
                                            ? t("onboarding.already_member", { count: organizations.length })
                                            : t("onboarding.already_member_plural", { count: organizations.length })}
                                    </p>
                                    <Button variant="ghost" className="group" onClick={handleSkipToDashboard}>
                                        {t("onboarding.skip_to_dashboard")}
                                        <ArrowRight className="ml-2 h-4 w-4 transition-transform group-hover:translate-x-1" />
                                    </Button>
                                </div>
                            </div>
                        )}
                    </div>
                </div>
                <div className="flex justify-center mt-6">
                    <div className="w-[180px]">
                        <LanguageSelector />
                    </div>
                </div>
            </div>
        </div>
    );
}
