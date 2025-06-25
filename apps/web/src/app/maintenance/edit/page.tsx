import { useParams, useNavigate } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  getMaintenancesByIdOptions,
  getMaintenancesByIdQueryKey,
  getMaintenancesQueryKey,
  getMonitorsBatchOptions,
  putMaintenancesByIdMutation,
} from "@/api/@tanstack/react-query.gen";
import Layout from "@/layout";
import CreateEditMaintenance, {
  type MaintenanceFormValues,
} from "../components/create-edit-form";
import dayjs from "dayjs";
import { toast } from "sonner";
import type { MaintenanceCreateUpdateDto } from "@/api";
import { commonMutationErrorHandler } from "@/lib/utils";

const EditMaintenance = () => {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const { data, isLoading, error } = useQuery({
    ...getMaintenancesByIdOptions({
      path: { id: id! },
    }),
    enabled: !!id,
  });

  // Prepare initial values for the form
  const maintenance = data?.data;

  const { data: monitorsData, isLoading: monitorsDataIsLoading } = useQuery({
    ...getMonitorsBatchOptions({
      query: {
        ids: maintenance?.monitor_ids?.join(",") || "",
      },
    }),
    enabled: !!maintenance?.monitor_ids,
  });

  const updateMaintenanceMutation = useMutation({
    ...putMaintenancesByIdMutation(),
    onSuccess: () => {
      toast.success("Maintenance updated successfully");

      queryClient.removeQueries({ queryKey: getMaintenancesQueryKey() });
      queryClient.removeQueries({
        queryKey: getMaintenancesByIdQueryKey({
          path: {
            id: id!,
          },
        }),
      });
      navigate("/maintenances");
    },
    onError: commonMutationErrorHandler("Failed to update maintenance")
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

    updateMaintenanceMutation.mutate({
      path: { id: id! },
      body: apiData,
    });
  };

  if (isLoading) return <Layout pageName="Edit Maintenance">Loading...</Layout>;
  if (error || !data?.data)
    return (
      <Layout pageName="Edit Maintenance">Error loading maintenance</Layout>
    );

  if (monitorsDataIsLoading) {
    return <Layout pageName="Edit Maintenance">Loading monitors...</Layout>;
  }

  const initialValues: MaintenanceFormValues = {
    title: maintenance?.title || "",
    description: maintenance?.description || "",
    strategy: maintenance?.strategy as MaintenanceFormValues["strategy"],
    // showOnAllPages: false,
    // selectedStatusPages: [],
    cron: maintenance?.cron || "",
    duration: maintenance?.duration || 60,
    intervalDay: maintenance?.interval_day || 1,
    weekdays: maintenance?.weekdays || [],
    daysOfMonth: maintenance?.days_of_month || [],
    startTime: maintenance?.start_time || "",
    endTime: maintenance?.end_time || "",
    timezone: maintenance?.timezone || "SAME_AS_SERVER",
    startDateTime: dayjs(maintenance?.start_date_time).format(
      "YYYY-MM-DDTHH:mm"
    ),
    endDateTime: dayjs(maintenance?.end_date_time).format("YYYY-MM-DDTHH:mm"),
    active: maintenance?.active || true,
    monitors:
      monitorsData?.data
        ?.map((monitor) => ({
          value: monitor.id || "",
          label: monitor.name || "",
        }))
        .filter((monitor) => monitor.value && monitor.label) || [],
  };

  return (
    <Layout pageName={`Edit Maintenance: ${maintenance?.title}`}>
      <CreateEditMaintenance
        initialValues={initialValues}
        mode="edit"
        onSubmit={handleSubmit}
      />
    </Layout>
  );
};

export default EditMaintenance;
