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

const NewMaintenance = () => {
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  const createMaintenanceMutation = useMutation({
    ...postMaintenancesMutation(),
    onSuccess: () => {
      toast.success("Maintenance created successfully");

      queryClient.invalidateQueries({ queryKey: getMaintenancesQueryKey() });

      navigate("/maintenances");
    },
    onError: commonMutationErrorHandler("Failed to create maintenance"),
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
    <Layout pageName="Schedule Maintenance">
      <BackButton to="/maintenances" />
      <CreateEditMaintenance onSubmit={handleSubmit} />
    </Layout>
  );
};

export default NewMaintenance;
