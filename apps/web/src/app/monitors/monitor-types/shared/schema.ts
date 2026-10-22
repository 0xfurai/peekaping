import { z } from "zod";

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

// The fields every monitor type has: the general, intervals, notifications and tags sections.
// A type's schema merges this and adds its own fields; proxies are added by the types that use one.
export const monitorBaseSchema = generalSchema
  .merge(intervalsSchema)
  .merge(notificationsSchema)
  .merge(tagsSchema);

export type MonitorBaseValues = z.infer<typeof monitorBaseSchema>;

export const monitorBaseDefaultValues: MonitorBaseValues = {
  ...generalDefaultValues,
  ...intervalsDefaultValues,
  ...notificationsDefaultValues,
  ...tagsDefaultValues,
};
