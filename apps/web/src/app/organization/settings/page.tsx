import { OrganizationForm } from "@/components/organization-form";
import { useOrganizationStore } from "@/store/organization";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import Layout from "@/layout";

export default function OrganizationSettingsPage() {
    const { currentOrganization } = useOrganizationStore();
    const { t } = useLocalizedTranslation();

    if (!currentOrganization) {
        return <div>Loading...</div>;
    }

    return (
        <Layout pageName={t("organization.settings_title") || "Organization Settings"}>
            <div className="space-y-6">
                <div>
                    <h3 className="text-lg font-medium">{t("organization.settings_title") || "Organization Settings"}</h3>
                    <p className="text-sm text-muted-foreground">
                        {t("organization.settings_description") || "Manage your organization details and monitors."}
                    </p>
                </div>
                <Card>
                    <CardHeader>
                        <CardTitle>{t("organization.general_title") || "General"}</CardTitle>
                        <CardDescription>
                            {t("organization.general_description") || "Update your organization's name and slug."}
                        </CardDescription>
                    </CardHeader>
                    <CardContent>
                        <OrganizationForm
                            mode="edit"
                            initialValues={{
                                name: currentOrganization.name || "",
                                slug: currentOrganization.slug || "",
                            }}
                            organizationId={currentOrganization.id}
                            onSuccess={() => {
                                // Optional: reload or refetch if needed, form handles invalidation
                            }}
                        />
                    </CardContent>
                </Card>
            </div>
        </Layout>
    );
}
