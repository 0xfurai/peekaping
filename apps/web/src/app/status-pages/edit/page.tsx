import Layout from "@/layout";
import CreateEditForm from "../components/create-edit-form";
import { useParams } from "react-router-dom";
import { BackButton } from "@/components/back-button";
import { useQuery } from "@tanstack/react-query";
import {
    getMonitorsBatchOptions,
    getStatusPagesByIdOptions,
} from "@/api/@tanstack/react-query.gen";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

import { useOrganizationStore } from "@/store/organization";

const EditStatusPageContent = () => {
    const { id: statusPageId } = useParams<{ id: string }>();
    const { t } = useLocalizedTranslation();
    const { currentOrganization } = useOrganizationStore();
    const slug = currentOrganization?.slug;
    const listPath = slug ? `/${slug}/status-pages` : "/status-pages";

    const { data: statusPage, isLoading: statusPageIsLoading } = useQuery({
        ...getStatusPagesByIdOptions({ path: { id: statusPageId! } }),
        enabled: !!statusPageId,
    });

    const { data: monitorsData, isLoading: monitorsDataIsLoading } = useQuery({
        ...getMonitorsBatchOptions({
            query: {
                ids: statusPage?.data?.monitor_ids?.join(",") || "",
            },
        }),
        enabled: !!statusPage?.data?.monitor_ids?.length,
    });

    if (statusPageIsLoading || monitorsDataIsLoading) {
        return (
            <Layout pageName={t("status_pages.edit_page_name")}>
                <div>{t("common.loading")}</div>
            </Layout>
        );
    }

    if (!statusPage?.data) {
        return (
            <Layout pageName={t("status_pages.edit_page_name")}>
                <div>{t("status_pages.messages.not_found")}</div>
            </Layout>
        );
    }

    const statusPageData = statusPage?.data;

    return (
        <Layout pageName={`${t("status_pages.edit_page_name")}: ${statusPageData.title}`}>
            <BackButton to={listPath} />
            <div className="flex flex-col gap-4">
                <p className="text-gray-500">
                    {t("status_pages.messages.update_description")}
                </p>

                <CreateEditForm
                    mode="edit"
                    id={statusPageId}
                    initialValues={{
                        title: statusPageData.title || "",
                        slug: statusPageData.slug || "",
                        description: statusPageData.description || "",
                        icon: statusPageData.icon || "",
                        footer_text: statusPageData.footer_text || "",
                        auto_refresh_interval: statusPageData?.auto_refresh_interval || 0,
                        published: Boolean(statusPageData?.published),
                        domains: statusPageData.domains || [],
                        monitors: monitorsData?.data?.map((monitor) => ({
                            label: monitor.name || "",
                            value: monitor.id || "",
                        })),
                    }}
                />
            </div>
        </Layout>
    );
};

export default EditStatusPageContent;
