import Layout from "@/layout";
import CreateEditMaintenance, {
    type MaintenanceFormValues,
} from "../components/create-edit-form";
import { BackButton } from "@/components/back-button";
import {
    getMaintenancesQueryKey,
    postMaintenancesMutation,
} from "@/api/@tanstack/react-query.gen";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { MaintenanceCreateUpdateDto } from "@/api";
import { commonMutationErrorHandler } from "@/lib/utils";
import { useNavigate } from "react-router-dom";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useOrganizationStore } from "@/store/organization";

const NewMaintenance = () => {
    const { t } = useLocalizedTranslation();
    const queryClient = useQueryClient();
    const navigate = useNavigate();
    const { currentOrganization } = useOrganizationStore();
    const slug = currentOrganization?.slug;
    const listPath = slug ? `/${slug}/maintenances` : "/maintenances";

    const createMaintenanceMutation = useMutation({
        ...postMaintenancesMutation(),
        onSuccess: () => {
            toast.success(t("maintenance.toasts.created_success"));

            queryClient.invalidateQueries({ queryKey: getMaintenancesQueryKey() });

            navigate(listPath);
        },
        onError: commonMutationErrorHandler(t("maintenance.toasts.create_error")),
    });

    const handleSubmit = (data: MaintenanceFormValues) => {
        const apiData: MaintenanceCreateUpdateDto = {
            title: data.title,
            description: data.description,
            active: data.active,
            strategy: data.strategy,
            monitor_ids: data.monitors.map((monitor) => monitor.value),
            ...(data.strategy === "single" && {
                timezone: data.timezone,
                start_date_time: data.startDateTime,
                end_date_time: data.endDateTime,
            }),
            ...(data.strategy === "cron" && {
                cron: data.cron,
                duration: data.duration,
                timezone: data.timezone,
                start_date_time: data.startDateTime,
                end_date_time: data.endDateTime,
            }),
            ...(data.strategy === "recurring-interval" && {
                interval_day: data.intervalDay,
                start_time: data.startTime,
                end_time: data.endTime,
                timezone: data.timezone,
                start_date_time: data.startDateTime,
                end_date_time: data.endDateTime,
            }),
            ...(data.strategy === "recurring-weekday" && {
                weekdays: data.weekdays,
                start_time: data.startTime,
                end_time: data.endTime,
                timezone: data.timezone,
                start_date_time: data.startDateTime,
                end_date_time: data.endDateTime,
            }),
            ...(data.strategy === "recurring-day-of-month" && {
                days_of_month: data.daysOfMonth?.map((day) =>
                    typeof day === "string" ? parseInt(day, 10) : day
                ),
                start_time: data.startTime,
                end_time: data.endTime,
                timezone: data.timezone,
                start_date_time: data.startDateTime,
                end_date_time: data.endDateTime,
            }),
        };

        createMaintenanceMutation.mutate({
            body: apiData,
        });
    };

    return (
        <Layout pageName={t("maintenance.schedule_title")}>
            <BackButton to={listPath} />
            <CreateEditMaintenance onSubmit={handleSubmit} />
        </Layout>
    );
};

export default NewMaintenance;
