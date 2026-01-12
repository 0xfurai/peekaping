import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useOrganizationStore } from "@/store/organization";
import { Input } from "@/components/ui/input";
import { useState } from "react";
import { toast } from "sonner";
import Layout from "@/layout";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { getOrganizationsByIdMembers, postOrganizationsByIdMembers } from "@/api/sdk.gen";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";
import { Copy, Loader2 } from "lucide-react";

export default function OrganizationMembersPage() {
    const { currentOrganization } = useOrganizationStore();
    const { t } = useLocalizedTranslation();
    const [email, setEmail] = useState("");
    const queryClient = useQueryClient();

    const { data: membersResponse, isLoading } = useQuery({
        queryKey: ["organization-members", currentOrganization?.id],
        queryFn: () => getOrganizationsByIdMembers({ path: { id: currentOrganization?.id || "" } }),
        enabled: !!currentOrganization?.id,
    });

    const inviteMutation = useMutation({
        mutationFn: (data: { email: string; role: "admin" | "member" }) =>
            postOrganizationsByIdMembers({
                path: { id: currentOrganization?.id || "" },
                body: data,
            }),
        onSuccess: (data) => {
            toast.success("Invitation created successfully");
            setEmail("");
            queryClient.invalidateQueries({ queryKey: ["organization-members"] });

            // If we want to show the link immediately toast it? 
            // Although it will appear in the list also.
            const invitation = (data.data as any).data;
            if (invitation?.token) {
                const link = `${window.location.origin}/invite/${invitation.token}`;
                navigator.clipboard.writeText(link);
                toast.success("Invitation link copied to clipboard");
            }
        },
        onError: (error) => {
            toast.error("Failed to invite member");
            console.error(error);
        }
    });

    const handleInvite = (e: React.FormEvent) => {
        e.preventDefault();
        if (!email) return;
        inviteMutation.mutate({ email, role: "member" });
    };

    const copyInviteLink = (token: string) => {
        const link = `${window.location.origin}/invite/${token}`;
        navigator.clipboard.writeText(link);
        toast.success("Invitation link copied to clipboard");
    };

    if (!currentOrganization) {
        return <div>Loading...</div>;
    }

    const members = (membersResponse?.data?.data as any[]) || [];

    return (
        <Layout pageName={t("organization.members_title") || "Organization Members"}>
            <div className="space-y-6">
                <div>
                    <h3 className="text-lg font-medium">{t("organization.members_title") || "Organization Members"}</h3>
                    <p className="text-sm text-muted-foreground">
                        {t("organization.members_description") || "Manage members and invitations."}
                    </p>
                </div>

                <Card>
                    <CardHeader>
                        <CardTitle>{t("organization.invite_member_title") || "Invite Member"}</CardTitle>
                        <CardDescription>
                            {t("organization.invite_member_description") || "Invite a new member by email."}
                        </CardDescription>
                    </CardHeader>
                    <CardContent>
                        <form onSubmit={handleInvite} className="flex gap-4">
                            <Input
                                placeholder="colleague@example.com"
                                type="email"
                                value={email}
                                onChange={e => setEmail(e.target.value)}
                                required
                                disabled={inviteMutation.isPending}
                            />
                            <Button type="submit" disabled={inviteMutation.isPending}>
                                {inviteMutation.isPending ? <Loader2 className="animate-spin w-4 h-4 mr-2" /> : null}
                                {t("organization.invite_button") || "Invite"}
                            </Button>
                        </form>
                    </CardContent>
                </Card>

                <Card>
                    <CardHeader>
                        <CardTitle>{t("organization.members_list_title") || "Members"}</CardTitle>
                    </CardHeader>
                    <CardContent>
                        {isLoading ? (
                            <div className="p-4 flex justify-center"><Loader2 className="animate-spin" /></div>
                        ) : (
                            <Table>
                                <TableHeader>
                                    <TableRow>
                                        <TableHead>User</TableHead>
                                        <TableHead>Role</TableHead>
                                        <TableHead>Status</TableHead>
                                        <TableHead className="text-right">Actions</TableHead>
                                    </TableRow>
                                </TableHeader>
                                <TableBody>
                                    {members.map((member: any) => (
                                        <TableRow key={member.user_id || member.invitation_token || Math.random()}>
                                            <TableCell>
                                                <div className="flex flex-col">
                                                    <span className="font-medium">{member.user?.name || member.user?.email || "Unknown"}</span>
                                                    <span className="text-xs text-muted-foreground">{member.user?.email}</span>
                                                </div>
                                            </TableCell>
                                            <TableCell className="capitalize">{member.role}</TableCell>
                                            <TableCell>
                                                <Badge variant={member.status === "active" ? "default" : "secondary"}>
                                                    {member.status}
                                                </Badge>
                                            </TableCell>
                                            <TableCell className="text-right">
                                                {member.status === "pending" && member.invitation_token && (
                                                    <Button
                                                        variant="ghost"
                                                        size="sm"
                                                        onClick={() => copyInviteLink(member.invitation_token)}
                                                        title="Copy Invitation Link"
                                                    >
                                                        <Copy className="h-4 w-4" />
                                                    </Button>
                                                )}
                                            </TableCell>
                                        </TableRow>
                                    ))}
                                    {members.length === 0 && (
                                        <TableRow>
                                            <TableCell colSpan={4} className="text-center text-muted-foreground">
                                                No members found.
                                            </TableCell>
                                        </TableRow>
                                    )}
                                </TableBody>
                            </Table>
                        )}
                    </CardContent>
                </Card>
            </div>
        </Layout>
    );
}
