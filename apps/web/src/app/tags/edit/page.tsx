import Layout from "@/layout";
import { BackButton } from "@/components/back-button";
import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { getTagsByIdOptions } from "@/api/@tanstack/react-query.gen";
import TagForm from "../components/tag-form";
import { Skeleton } from "@/components/ui/skeleton";
import { Card, CardContent } from "@/components/ui/card";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

import { useOrganizationStore } from "@/store/organization";

const EditTag = () => {
    const { id } = useParams();
    const { t } = useLocalizedTranslation();
    const { currentOrganization } = useOrganizationStore();
    const slug = currentOrganization?.slug;
    const listPath = slug ? `/${slug}/tags` : "/tags";

    const { data: tag, isLoading, error } = useQuery({
        ...getTagsByIdOptions({ path: { id: id! } }),
        enabled: !!id,
    });

    if (!id) {
        return (
            <Layout pageName={t("tags.edit_page_name")}>
                <BackButton to={listPath} />
                <div className="text-red-500">{t("tags.messages.tag_id_required")}</div>
            </Layout>
        );
    }

    if (isLoading) {
        return (
            <Layout pageName={t("tags.edit_page_name")}>
                <BackButton to={listPath} />
                <div className="flex flex-col gap-4">
                    <p className="text-gray-500">{t("common.loading")}</p>
                    <Card>
                        <CardContent className="space-y-4 pt-6">
                            <Skeleton className="h-4 w-1/4" />
                            <Skeleton className="h-10 w-full" />
                            <Skeleton className="h-4 w-1/4" />
                            <Skeleton className="h-10 w-full" />
                            <Skeleton className="h-4 w-1/4" />
                            <Skeleton className="h-20 w-full" />
                        </CardContent>
                    </Card>
                </div>
            </Layout>
        );
    }

    if (error || !tag?.data) {
        return (
            <Layout pageName={t("tags.edit_page_name")}>
                <BackButton to={listPath} />
                <div className="text-red-500">
                    {t("tags.messages.failed_to_load_tag")}
                </div>
            </Layout>
        );
    }

    return (
        <Layout pageName={`${t("tags.edit_page_name")}: ${tag.data.name}`}>
            <BackButton to={listPath} />
            <div className="flex flex-col gap-4">
                <p className="text-gray-500">
                    {t("tags.messages.update_description")}
                </p>

                <TagForm mode="edit" tag={tag.data} />
            </div>
        </Layout>
    );
};

export default EditTag;
