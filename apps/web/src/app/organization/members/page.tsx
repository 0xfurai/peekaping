import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useOrganizationStore } from "@/store/organization";
import { Input } from "@/components/ui/input";
import { useState } from "react";
import { toast } from "sonner";
import Layout from "@/layout";

export default function OrganizationMembersPage() {
    const { currentOrganization } = useOrganizationStore();
    const { t } = useLocalizedTranslation();
    const [email, setEmail] = useState("");

    if (!currentOrganization) {
        return <div>Loading...</div>;
    }

    const handleInvite = (e: React.FormEvent) => {
        e.preventDefault();
        // Placeholder for invitation logic
        // Need to call postOrganizationsByIdMembers or similar
        toast.info("Invitation feature coming soon (Backend pending)");
    };

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
                            />
                            <Button type="submit">{t("organization.invite_button") || "Invite"}</Button>
                        </form>
                    </CardContent>
                </Card>

                <Card>
                    <CardHeader>
                        <CardTitle>{t("organization.members_list_title") || "Members"}</CardTitle>
                    </CardHeader>
                    <CardContent>
                        <p className="text-muted-foreground text-sm">Members list will appear here.</p>
                    </CardContent>
                </Card>
            </div>
        </Layout>
    );
}
