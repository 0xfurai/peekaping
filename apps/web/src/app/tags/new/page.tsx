import Layout from "@/layout";
import { BackButton } from "@/components/back-button";
import TagForm from "../components/tag-form";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

import { useOrganizationStore } from "@/store/organization";

const NewTag = () => {
    const { t } = useLocalizedTranslation();
    const { currentOrganization } = useOrganizationStore();
    const slug = currentOrganization?.slug;
    const listPath = slug ? `/${slug}/tags` : "/tags";

    return (
        <Layout pageName={t("tags.new_page_name")}>
            <BackButton to={listPath} />
            <div className="flex flex-col gap-4">
                <p className="text-gray-500">
                    {t("tags.messages.create_description")}
                </p>

                <TagForm mode="create" />
            </div>
        </Layout>
    );
};

export default NewTag;
