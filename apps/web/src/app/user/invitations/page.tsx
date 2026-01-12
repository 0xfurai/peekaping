import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { getUserInvitations, postInvitationsByTokenAccept } from "@/api/sdk.gen";
import Layout from "@/layout";
import { Badge } from "@/components/ui/badge";
import { Loader2, Check } from "lucide-react";
import { toast } from "sonner";

export default function UserInvitationsPage() {
    const queryClient = useQueryClient();

    const { data, isLoading } = useQuery({
        queryKey: ["user-invitations"],
        queryFn: () => getUserInvitations(),
    });

    const acceptMutation = useMutation({
        mutationFn: (data: { token: string; slug: string }) => {
            return postInvitationsByTokenAccept({ path: { token: data.token } });
        },
        onSuccess: (_data, variables) => {
            toast.success("Invitation accepted!");
            queryClient.invalidateQueries({ queryKey: ["user-invitations"] });
            queryClient.invalidateQueries({ queryKey: ["user-organizations"] });

            // Redirect to the new organization dashboard
            window.location.href = `/${variables.slug}/monitors`;
        },
        onError: () => {
            toast.error("Failed to accept invitation");
        }
    });

    const invitations = data?.data?.data || [];

    return (
        <Layout pageName="My Invitations">
            <div className="space-y-6">
                <div>
                    <h3 className="text-lg font-medium">Pending Invitations</h3>
                    <p className="text-sm text-muted-foreground">
                        Manage your pending invitations to organizations.
                    </p>
                </div>

                {isLoading ? (
                    <div className="flex justify-center p-8">
                        <Loader2 className="h-6 w-6 animate-spin" />
                    </div>
                ) : (
                    <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
                        {invitations.length === 0 ? (
                            <div className="col-span-full text-center p-8 text-muted-foreground">
                                No pending invitations found.
                            </div>
                        ) : (
                            invitations.map((inv) => (
                                <Card key={inv.token}>
                                    <CardHeader>
                                        <CardTitle>{inv.organization?.name}</CardTitle>
                                        <CardDescription>Invited to join as <span className="capitalize">{inv.role}</span></CardDescription>
                                    </CardHeader>
                                    <CardContent>
                                        <div className="flex justify-between items-center">
                                            <Badge variant="secondary">{inv.status}</Badge>
                                            <Button
                                                size="sm"
                                                onClick={() => acceptMutation.mutate({
                                                    token: inv.token || "",
                                                    slug: inv.organization?.slug || ""
                                                })}
                                                disabled={acceptMutation.isPending}
                                            >
                                                {acceptMutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Check className="h-4 w-4 mr-2" />}
                                                Accept
                                            </Button>
                                        </div>
                                    </CardContent>
                                </Card>
                            ))
                        )}
                    </div>
                )}
            </div>
        </Layout>
    );
}
