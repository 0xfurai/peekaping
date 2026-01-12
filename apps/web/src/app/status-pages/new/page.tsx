import Layout from "@/layout";
import CreateEditForm from "../components/create-edit-form";
import { BackButton } from "@/components/back-button";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useOrganizationStore } from "@/store/organization";

const NewStatusPageContent = () => {
    const { t } = useLocalizedTranslation();
    const { currentOrganization } = useOrganizationStore();
    const slug = currentOrganization?.slug;
    const listPath = slug ? `/${slug}/status-pages` : "/status-pages";

    return (
        <Layout pageName={t("status_pages.new_page_name")}>
            <BackButton to={listPath} />
            <div className="flex flex-col gap-4">
                <p className="text-gray-500">
                    {t("status_pages.messages.create_description")}
                </p>
                <CreateEditForm mode="create" />
            </div>
        </Layout>
    );
};

export default NewStatusPageContent;
