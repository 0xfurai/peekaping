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

// Strategy options
const STRATEGY_OPTIONS = [
  { value: "manual", label: "Active/Inactive manually" },
  { value: "single", label: "Single Maintenance Window" },
  { value: "cron", label: "Cron Expression" },
  { value: "recurring-interval", label: "Recurring - Interval" },
  { value: "recurring-weekday", label: "Recurring - Day of Week" },
  { value: "recurring-day-of-month", label: "Recurring - Day of Month" },
];

// Base schema with shared fields
const baseMaintenanceSchema = z.object({
  title: z.string().min(1, "Title is required"),
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
    startDateTime: z.string().min(1, "Start date is required"),
    endDateTime: z.string().min(1, "End date is required"),
  }),

  // Cron expression
  baseMaintenanceSchema.extend({
    strategy: z.literal("cron"),
    cron: z.string().optional(),
    duration: z.number().optional(),
    timezone: z.string().optional(),
    startDateTime: z.string().min(1, "Start date is required"),
    endDateTime: z.string().min(1, "End date is required"),
  }),

  // Recurring interval
  baseMaintenanceSchema.extend({
    strategy: z.literal("recurring-interval"),
    intervalDay: z.number().min(1).max(3650, "Interval day must be between 1 and 3650"),
    startTime: z.string().min(1, "Start time is required"),
    endTime: z.string().min(1, "End time is required"),
    timezone: z.string().optional(),
    startDateTime: z.string().min(1, "Start date is required"),
    endDateTime: z.string().min(1, "End date is required"),
  }),

  // Recurring weekday
  baseMaintenanceSchema.extend({
    strategy: z.literal("recurring-weekday"),
    weekdays: z.array(z.number()).min(1, "At least one weekday must be selected"),
    startTime: z.string().min(1, "Start time is required"),
    endTime: z.string().min(1, "End time is required"),
    timezone: z.string().optional(),
    startDateTime: z.string().min(1, "Start date is required"),
    endDateTime: z.string().min(1, "End date is required"),
  }),

  // Recurring day of month
  baseMaintenanceSchema.extend({
    strategy: z.literal("recurring-day-of-month"),
    daysOfMonth: z.array(z.union([z.number(), z.string()])).min(1, "At least one day must be selected"),
    startTime: z.string().min(1, "Start time is required"),
    endTime: z.string().min(1, "End time is required"),
    timezone: z.string().optional(),
    startDateTime: z.string().min(1, "Start date is required"),
    endDateTime: z.string().min(1, "End date is required"),
  }),
]).superRefine((data, ctx) => {
  if (data.strategy === "single" || data.strategy === "cron") {
    if (data.startDateTime && data.endDateTime) {
      const startDate = new Date(data.startDateTime);
      const endDate = new Date(data.endDateTime);
      if (startDate >= endDate) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: "Start date must be before end date",
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
            message: "Start time must be before end time",
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
          message: "Start date must be before end date",
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
        {mode === "edit" ? "Edit" : "Schedule"} Maintenance
      </CardTitle>

      <Form {...form}>
        <form onSubmit={form.handleSubmit(handleSubmit)} className="space-y-6">
          <FormField
            control={form.control}
            name="title"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Title</FormLabel>
                <FormControl>
                  <Input placeholder="Maintenance title" {...field} />
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
                <FormLabel>Description</FormLabel>
                <FormControl>
                  <Textarea
                    placeholder="Maintenance description..."
                    className="min-h-[100px]"
                    {...field}
                  />
                </FormControl>
                <FormDescription>Markdown is supported</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <div className="space-y-4">
            <h2 className="text-lg font-semibold">Affected Monitors</h2>
            <div className="space-y-2">
              <p className="text-sm text-muted-foreground">
                Select the monitors that will be affected by this maintenance
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
            <h2 className="text-lg font-semibold">Date and Time</h2>

            {/* Strategy */}
            <FormField
              control={form.control}
              name="strategy"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Strategy</FormLabel>
                  <Select onValueChange={field.onChange} value={field.value}>
                    <FormControl>
                      <SelectTrigger>
                        <SelectValue placeholder="Select strategy" />
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
              {isLoading ? "Saving..." : "Save"}
            </Button>
          </div>
        </form>
      </Form>
    </div>
  );
}
