import type { MonitorCreateUpdateDto, MonitorMonitorResponseDto } from "@/api";
import type { MonitorBaseValues } from "./schema";

// The form values of the fields every monitor type has, read from a saved monitor. A field the
// API left out gets the value a new monitor starts with.
export const deserializeMonitorBase = (data: MonitorMonitorResponseDto): MonitorBaseValues => ({
  name: data.name ?? "",
  interval: data.interval ?? 60,
  timeout: data.timeout ?? 16,
  max_retries: data.max_retries ?? 3,
  retry_interval: data.retry_interval ?? 60,
  resend_interval: data.resend_interval ?? 10,
  notification_ids: data.notification_ids ?? [],
  tag_ids: data.tag_ids ?? [],
});

// The request body fields every monitor type sends. The type's codec adds `type` and `config`.
export const serializeMonitorBase = (
  values: MonitorBaseValues
): Pick<
  MonitorCreateUpdateDto,
  | "name"
  | "interval"
  | "timeout"
  | "max_retries"
  | "retry_interval"
  | "resend_interval"
  | "notification_ids"
  | "tag_ids"
> => ({
  name: values.name,
  interval: values.interval,
  timeout: values.timeout,
  max_retries: values.max_retries,
  retry_interval: values.retry_interval,
  resend_interval: values.resend_interval,
  notification_ids: values.notification_ids,
  tag_ids: values.tag_ids,
});
