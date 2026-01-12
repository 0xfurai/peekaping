import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useNavigate, useParams } from "react-router-dom";
import { getInvitationsByToken, postInvitationsByTokenAccept } from "@/api/sdk.gen";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";
import { useAuthStore } from "@/store/auth";

export default function InvitationPage() {
    const { token } = useParams<{ token: string }>();
    const navigate = useNavigate();
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
            toast.success("Invitation accepted!");
            navigate("/"); // Go to dashboard/home, which should show the new org
        },
        onError: (err) => {
            toast.error("Failed to accept invitation");
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
                        <CardTitle className="text-red-500">Invalid Invitation</CardTitle>
                        <CardDescription>
                            This invitation link is invalid or has expired.
                        </CardDescription>
                    </CardHeader>
                    <CardContent>
                        <Button onClick={() => navigate("/")} variant="outline" className="w-full">
                            Go Home
                        </Button>
                    </CardContent>
                </Card>
            </div>
        );
    }

    const invitation = data.data?.data;

    return (
        <div className="flex h-screen items-center justify-center bg-muted/40 p-4">
            <Card className="w-full max-w-md">
                <CardHeader>
                    <CardTitle>You've been invited!</CardTitle>
                    <CardDescription>
                        You have been invited to join <strong>{invitation.organization?.name}</strong>.
                    </CardDescription>
                </CardHeader>
                <CardContent className="space-y-4">
                    <div className="rounded-lg bg-muted p-4">
                        <div className="text-sm font-medium text-muted-foreground">Organization</div>
                        <div className="text-lg font-semibold">{invitation.organization?.name}</div>
                        <div className="mt-2 text-sm font-medium text-muted-foreground">Role</div>
                        <div className="capitalize">{invitation.role}</div>
                    </div>

                    {!isAuthenticated ? (
                        <div className="space-y-2">
                            <p className="text-sm text-muted-foreground text-center">
                                You need to log in or create an account to accept this invitation.
                            </p>
                            <Button onClick={handleLogin} className="w-full">
                                Login to Accept
                            </Button>
                            <Button onClick={() => navigate(`/register?returnUrl=/invite/${token}`)} variant="outline" className="w-full">
                                Create Account
                            </Button>
                        </div>
                    ) : (
                        <div className="space-y-2">
                            <div className="text-sm text-center text-muted-foreground mb-4">
                                Logged in as <strong>{useAuthStore.getState().user?.email}</strong>
                            </div>
                            <Button
                                onClick={handleAccept}
                                disabled={acceptMutation.isPending}
                                className="w-full"
                            >
                                {acceptMutation.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
                                Accept Invitation
                            </Button>
                        </div>
                    )}
                </CardContent>
            </Card>
        </div>
    );
}
