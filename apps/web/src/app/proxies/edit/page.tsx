import { useParams, useNavigate } from "react-router-dom";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { BackButton } from "@/components/back-button";
import {
    getProxiesByIdOptions,
    putProxiesByIdMutation,
    getProxiesInfiniteQueryKey,
    getProxiesByIdQueryKey,
} from "@/api/@tanstack/react-query.gen";
import Layout from "@/layout";
import { toast } from "sonner";
import CreateEditProxy from "../components/create-edit-proxy";
import type { ProxyCreateUpdateDto } from "@/api/types.gen";
import { type ProxyForm } from "../components/create-edit-proxy";
import { commonMutationErrorHandler } from "@/lib/utils";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useOrganizationStore } from "@/store/organization";

const EditProxy = () => {
    const { id } = useParams<{ id: string }>();
    const navigate = useNavigate();
    const queryClient = useQueryClient();
    const { t } = useLocalizedTranslation();
    const { currentOrganization } = useOrganizationStore();
    const slug = currentOrganization?.slug;
    const listPath = slug ? `/${slug}/proxies` : "/proxies";

    const { data, isLoading, error } = useQuery({
        ...getProxiesByIdOptions({ path: { id: id! } }),
        enabled: !!id,
    });

    const mutation = useMutation({
        ...putProxiesByIdMutation(),
        onSuccess: () => {
            toast.success(t("proxies.messages.updated_success"));

            queryClient.invalidateQueries({
                queryKey: getProxiesInfiniteQueryKey()
            });

            queryClient.removeQueries({
                queryKey: getProxiesByIdQueryKey({
                    path: {
                        id: id!
                    }
                })
            });

            navigate(listPath);
        },
        onError: commonMutationErrorHandler(t("proxies.messages.update_failed")),
    });

    if (isLoading) return <Layout pageName={t("proxies.edit.page_name")}>{t('common.loading')}</Layout>;
    if (error || !data?.data)
        return <Layout pageName={t("proxies.edit.page_name")}>{t('proxies.messages.error_loading_proxy')}</Layout>;

    // Prepare initial values for the form
    const proxy = data.data;

    const initialValues = {
        protocol: proxy.protocol as "http" | "https" | "socks" | "socks5" | "socks5h" | "socks4",
        host: proxy.host || "",
        port: proxy.port || 1,
        auth: proxy.auth || false,
        username: proxy.username || "",
        password: proxy.password || "",
    };

    const handleSubmit = (formData: ProxyForm) => {
        const proxyData: ProxyCreateUpdateDto = {
            protocol: formData.protocol,
            host: formData.host,
            port: formData.port,
            auth: formData.auth,
            username: formData.auth ? formData.username : undefined,
            password: formData.auth ? formData.password : undefined,
        };

        mutation.mutate({
            path: { id: id! },
            body: proxyData,
        });
    };

    return (
        <Layout pageName={`${t("proxies.edit.page_name")}: ${proxy.host}:${proxy.port}`}>
            <BackButton to={listPath} />
            <CreateEditProxy
                initialValues={initialValues}
                onSubmit={handleSubmit}
                mode="edit"
            />
        </Layout>
    );
};

export default EditProxy;
