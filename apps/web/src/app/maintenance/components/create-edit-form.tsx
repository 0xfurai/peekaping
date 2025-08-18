import { useMemo } from "react";
import { zodResolver } from "@hookform/resolvers/zod";
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { z } from "zod";
import { useForm } from "react-hook-form";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import SingleMaintenanceWindowForm from "./single-maintenance-window-form";
import CronExpressionForm from "./cron-expression-form";
import RecurringIntervalForm from "./recurring-interval-form";
import RecurringWeekdayForm from "./recurring-weekday-form";
import RecurringDayOfMonthForm from "./recurring-day-of-month-form";
import { convertToDateTimeLocal } from "@/lib/utils";
import SearchableMonitorSelector from "@/components/searchable-monitor-selector";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

// Strategy options - will be populated with translations in component

// Base schema with shared fields
const baseMaintenanceSchema = z.object({
  title: z.string().min(1, "maintenance.validation.title_required"),
  description: z.string().optional(),
  active: z.boolean(),
  monitors: z.array(
    z.object({
      value: z.string(),
      label: z.string(),
    })
  ),
  showOnAllPages: z.boolean().optional(),
  status_page_ids: z
    .array(
      z.object({
        id: z.string(),
        name: z.string(),
      })
    )
    .optional(),
});

const maintenanceSchema = z.discriminatedUnion("strategy", [
  // Manual strategy
  baseMaintenanceSchema.extend({
    strategy: z.literal("manual"),
  }),

  // Single maintenance window
  baseMaintenanceSchema.extend({
    strategy: z.literal("single"),
    timezone: z.string().optional(),
    startDateTime: z.string().min(1, "maintenance.validation.start_date_required"),
    endDateTime: z.string().min(1, "maintenance.validation.end_date_required"),
  }),

  // Cron expression
  baseMaintenanceSchema.extend({
    strategy: z.literal("cron"),
    cron: z.string().optional(),
    duration: z.number().optional(),
    timezone: z.string().optional(),
    startDateTime: z.string().min(1, "maintenance.validation.start_date_required"),
    endDateTime: z.string().min(1, "maintenance.validation.end_date_required"),
  }),

  // Recurring interval
  baseMaintenanceSchema.extend({
    strategy: z.literal("recurring-interval"),
    intervalDay: z.number().min(1).max(3650, "maintenance.validation.interval_day_range"),
    startTime: z.string().min(1, "maintenance.validation.start_time_required"),
    endTime: z.string().min(1, "maintenance.validation.end_time_required"),
    timezone: z.string().optional(),
    startDateTime: z.string().min(1, "maintenance.validation.start_date_required"),
    endDateTime: z.string().min(1, "maintenance.validation.end_date_required"),
  }),

  // Recurring weekday
  baseMaintenanceSchema.extend({
    strategy: z.literal("recurring-weekday"),
    weekdays: z.array(z.number()).min(1, "maintenance.validation.weekday_required"),
    startTime: z.string().min(1, "maintenance.validation.start_time_required"),
    endTime: z.string().min(1, "maintenance.validation.end_time_required"),
    timezone: z.string().optional(),
    startDateTime: z.string().min(1, "maintenance.validation.start_date_required"),
    endDateTime: z.string().min(1, "maintenance.validation.end_date_required"),
  }),

  // Recurring day of month
  baseMaintenanceSchema.extend({
    strategy: z.literal("recurring-day-of-month"),
    daysOfMonth: z.array(z.union([z.number(), z.string()])).min(1, "maintenance.validation.day_of_month_required"),
    startTime: z.string().min(1, "maintenance.validation.start_time_required"),
    endTime: z.string().min(1, "maintenance.validation.end_time_required"),
    timezone: z.string().optional(),
    startDateTime: z.string().min(1, "maintenance.validation.start_date_required"),
    endDateTime: z.string().min(1, "maintenance.validation.end_date_required"),
  }),
]).superRefine((data, ctx) => {
    if (data.strategy === "single" || data.strategy === "cron") {
    if (data.startDateTime && data.endDateTime) {
      const startDate = new Date(data.startDateTime);
      const endDate = new Date(data.endDateTime);
      if (startDate >= endDate) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: "maintenance.validation.start_before_end_date",
          path: ["startDateTime"],
        });
      }
    }
  }

  if (data.strategy === "recurring-interval" ||
      data.strategy === "recurring-weekday" ||
      data.strategy === "recurring-day-of-month") {

    if (data.startTime && data.endTime) {
      const [startHour, startMin] = data.startTime.split(':').map(Number);
      const [endHour, endMin] = data.endTime.split(':').map(Number);

      if (!isNaN(startHour) && !isNaN(startMin) && !isNaN(endHour) && !isNaN(endMin)) {
        const startMinutes = startHour * 60 + startMin;
        const endMinutes = endHour * 60 + endMin;

        if (startMinutes >= endMinutes) {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            message: "maintenance.validation.start_before_end_time",
            path: ["startTime"],
          });
        }
      }
    }

    if (data.startDateTime && data.endDateTime) {
      const startDate = new Date(data.startDateTime);
      const endDate = new Date(data.endDateTime);
      if (startDate >= endDate) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: "maintenance.validation.start_before_end_date",
          path: ["startDateTime"],
        });
      }
    }
  }
});

export type MaintenanceFormValues = z.infer<typeof maintenanceSchema>;

const defaultValues: MaintenanceFormValues = {
  title: "",
  description: "",
  strategy: "single" as const,
  monitors: [],
  showOnAllPages: false,
  status_page_ids: [],
  timezone: "SAME_AS_SERVER",
  startDateTime: convertToDateTimeLocal(new Date().toISOString()),
  endDateTime: convertToDateTimeLocal(
    new Date(new Date().getTime() + 1 * 60 * 60 * 1000).toISOString()
  ),
  active: true,
};

export default function CreateEditMaintenance({
  initialValues = defaultValues,
  isLoading = false,
  mode = "create",
  onSubmit,
}: {
  initialValues?: MaintenanceFormValues;
  isLoading?: boolean;
  mode?: "create" | "edit";
  onSubmit: (data: MaintenanceFormValues) => void;
}) {
  const { t } = useLocalizedTranslation();

  const STRATEGY_OPTIONS = useMemo(() => [
    { value: "manual", label: t("maintenance.strategy.manual") },
    { value: "single", label: t("maintenance.strategy.single") },
    { value: "cron", label: t("maintenance.strategy.cron") },
    { value: "recurring-interval", label: t("maintenance.strategy.recurring_interval") },
    { value: "recurring-weekday", label: t("maintenance.strategy.recurring_weekday") },
    { value: "recurring-day-of-month", label: t("maintenance.strategy.recurring_day_of_month") },
  ], [t]);

  const form = useForm<MaintenanceFormValues>({
    resolver: zodResolver(maintenanceSchema),
    defaultValues: initialValues,
  });

  const strategy = form.watch("strategy");

  const handleSubmit = (data: MaintenanceFormValues) => {
    onSubmit(data);
  };

  return (
    <div className="flex flex-col gap-6 max-w-[800px]">
      <CardTitle className="text-xl">
        {mode === "edit" ? t("maintenance.edit_title") : t("maintenance.schedule_title")}
      </CardTitle>

      <Form {...form}>
        <form onSubmit={form.handleSubmit(handleSubmit)} className="space-y-6">
          <FormField
            control={form.control}
            name="title"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("maintenance.form.title_label")}</FormLabel>
                <FormControl>
                  <Input placeholder={t("maintenance.form.title_placeholder")} {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          {/* Description */}
          <FormField
            control={form.control}
            name="description"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("maintenance.form.description_label")}</FormLabel>
                <FormControl>
                  <Textarea
                    placeholder={t("maintenance.form.description_placeholder")}
                    className="min-h-[100px]"
                    {...field}
                  />
                </FormControl>
                <FormDescription>{t("maintenance.form.markdown_supported")}</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <div className="space-y-4">
            <h2 className="text-lg font-semibold">{t("maintenance.form.affected_monitors_title")}</h2>
            <div className="space-y-2">
              <p className="text-sm text-muted-foreground">
                {t("maintenance.form.affected_monitors_description")}
              </p>

              <SearchableMonitorSelector
                value={form.watch("monitors")}
                onSelect={(value) => {
                  form.setValue("monitors", value);
                }}
              />
            </div>
          </div>

          {/* Date and Time */}
          <div className="space-y-4">
            <h2 className="text-lg font-semibold">{t("maintenance.form.date_time_title")}</h2>

            {/* Strategy */}
            <FormField
              control={form.control}
              name="strategy"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("maintenance.form.strategy_label")}</FormLabel>
                  <Select onValueChange={field.onChange} value={field.value}>
                    <FormControl>
                      <SelectTrigger>
                        <SelectValue placeholder={t("maintenance.form.strategy_placeholder")} />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent>
                      {STRATEGY_OPTIONS.map((option) => (
                        <SelectItem key={option.value} value={option.value}>
                          {option.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <FormMessage />
                </FormItem>
              )}
            />

            {strategy === "single" && <SingleMaintenanceWindowForm />}
            {strategy === "cron" && <CronExpressionForm />}
            {strategy === "recurring-interval" && <RecurringIntervalForm />}
            {strategy === "recurring-weekday" && <RecurringWeekdayForm />}
            {strategy === "recurring-day-of-month" && (
              <RecurringDayOfMonthForm />
            )}
          </div>

          <div className="flex gap-2 pt-4">
            <Button type="submit" disabled={isLoading}>
              {isLoading ? t("common.saving") : t("common.save")}
            </Button>
          </div>
        </form>
      </Form>
    </div>
  );
}
