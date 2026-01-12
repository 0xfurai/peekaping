import { useParams, useNavigate } from "react-router-dom";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { BackButton } from "@/components/back-button";
import {
    getNotificationChannelsByIdOptions,
    getNotificationChannelsByIdQueryKey,
    putNotificationChannelsByIdMutation,
} from "@/api/@tanstack/react-query.gen";
import Layout from "@/layout";
import CreateEditNotificationChannel, {
    type NotificationForm,
} from "../components/create-edit-notification-channel";
import { toast } from "sonner";
import { commonMutationErrorHandler } from "@/lib/utils";
import type { NotificationChannelCreateUpdateDto } from "@/api";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useOrganizationStore } from "@/store/organization";

const EditNotificationChannel = () => {
    const { id } = useParams<{ id: string }>();
    const navigate = useNavigate();
    const { t } = useLocalizedTranslation();
    const queryClient = useQueryClient();
    const { currentOrganization } = useOrganizationStore();
    const slug = currentOrganization?.slug;
    const listPath = slug ? `/${slug}/notification-channels` : "/notification-channels";

    const { data, isLoading, error } = useQuery({
        ...getNotificationChannelsByIdOptions({ path: { id: id! } }),
        enabled: !!id,
    });

    const mutation = useMutation({
        ...putNotificationChannelsByIdMutation(),
        onSuccess: () => {
            toast.success(t("notifications.messages.updated_success"));
            queryClient.removeQueries({
                queryKey: getNotificationChannelsByIdQueryKey({ path: { id: id! } }),
            });
            navigate(listPath);
        },
        onError: commonMutationErrorHandler(t("notifications.messages.update_failed")),
    });

    if (isLoading) return <Layout pageName={t("notifications.edit_title")}>{t("common.loading")}</Layout>;
    if (error || !data?.data)
        return <Layout pageName={t("notifications.edit_title")}>{t("notifications.messages.error_loading_notifier")}</Layout>;

    // Prepare initial values for the form
    const notifier = data.data;
    const config = JSON.parse(notifier.config || "{}");

    const initialValues = {
        name: notifier.name || "",
        type: notifier.type,
        ...(config || {}),
    };

    const handleSubmit = (values: NotificationForm) => {
        const payload: NotificationChannelCreateUpdateDto = {
            name: values.name,
            type: values.type,
            config: JSON.stringify(values),
            active: notifier.active,
            is_default: notifier.is_default,
        };

        mutation.mutate({
            path: { id: id! },
            body: payload,
        });
    };

    return (
        <Layout pageName={`${t("notifications.edit_channel_title")}: ${notifier.name}`}>
            <BackButton to={listPath} />
            <CreateEditNotificationChannel
                initialValues={initialValues}
                onSubmit={handleSubmit}
                isLoading={mutation.isPending}
                mode="edit"
            />
        </Layout>
    );
};

export default EditNotificationChannel;
