import { z } from "zod";
import { LOCAL_PROBE_ID } from "@/app/probes/components/local-probe";

export const generalDefaultValues = {
  name: "My monitor",
};

export const generalSchema = z.object({
  name: z.string(),
});

export const intervalsSchema = z.object({
  interval: z.coerce.number().min(20),
  max_retries: z.coerce.number().min(0),
  retry_interval: z.coerce.number().min(20),
  timeout: z.coerce.number().min(16),
  resend_interval: z.coerce.number().min(0),
});

export type IntervalsFormValues = z.infer<typeof intervalsSchema>;

export const intervalsDefaultValues: IntervalsFormValues = {
  interval: 20,
  max_retries: 0,
  retry_interval: 20,
  timeout: 16,
  resend_interval: 0,
};

export const notificationsSchema = z.object({
  notification_ids: z.array(z.string()),
});

export const notificationsDefaultValues = {
  notification_ids: [],
};

export const proxiesSchema = z.object({
  proxy_id: z.string().optional(),
});

// No proxy is the empty string: that is what a saved monitor without one has, and what removing
// a proxy sets
export const proxiesDefaultValues = {
  proxy_id: "",
};

export const tagsDefaultValues = {
  tag_ids: [] as string[],
};

export const tagsSchema = z.object({
  tag_ids: z.array(z.string()),
});

// A monitor runs on one or more probes. The message is an i18n key, which FormMessage translates.
export const probeSchema = z.object({
  probe_ids: z.array(z.string()).min(1, "monitors.form.shared.probe.required"),
});

export const probeDefaultValues = {
  probe_ids: [LOCAL_PROBE_ID],
};

export const LOCATION_RULES = ["any", "all", "at_least"] as const;

export type LocationRule = (typeof LOCATION_RULES)[number];

// How many of the monitor's locations must be Down for it to be Down. The threshold is the N of
// "at least N"; it is null for the other rules. The number input hands the form a string, and an
// emptied one an empty string. The field takes any number, since a value typed under "at least"
// stays in the form after the user switches rule: only that rule checks it, in
// locationThresholdError. The messages are i18n keys, which FormMessage translates.
export const LOCATION_THRESHOLD_INVALID = "monitors.form.shared.location_rule.threshold_invalid";
export const LOCATION_THRESHOLD_TOO_HIGH = "monitors.form.shared.location_rule.threshold_too_high";

export const locationRuleSchema = z.object({
  location_rule: z.enum(LOCATION_RULES),
  // Typed as the number the form holds, as for the coerced fields (the input hands it a string),
  // so that the key stays required in the form's input type
  location_threshold: z.preprocess((value) => {
    if (value === "" || value === null || value === undefined) return null;
    const number = Number(value);
    return Number.isNaN(number) ? null : number;
  }, z.number().nullable()) as z.ZodType<number | null>,
});

export const locationRuleDefaultValues = {
  location_rule: "any" as LocationRule,
  location_threshold: null as number | null,
};

// The fields every monitor type has: the general, intervals, notifications, tags, probe and
// location rule sections. A type's schema merges this and adds its own fields; proxies are added by the types
// that use one.
export const monitorBaseSchema = generalSchema
  .merge(intervalsSchema)
  .merge(notificationsSchema)
  .merge(tagsSchema)
  .merge(probeSchema)
  .merge(locationRuleSchema);

export type MonitorBaseValues = z.infer<typeof monitorBaseSchema>;

export const monitorBaseDefaultValues: MonitorBaseValues = {
  ...generalDefaultValues,
  ...intervalsDefaultValues,
  ...notificationsDefaultValues,
  ...tagsDefaultValues,
  ...probeDefaultValues,
  ...locationRuleDefaultValues,
};
