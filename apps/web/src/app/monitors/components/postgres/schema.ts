import { z } from "zod";
import { generalDefaultValues, generalSchema } from "../shared/general";
import { intervalsDefaultValues, intervalsSchema } from "../shared/intervals";
import { notificationsDefaultValues, notificationsSchema } from "../shared/notifications";
import { tagsDefaultValues, tagsSchema } from "../shared/tags";
import type { MonitorMonitorResponseDto, MonitorCreateUpdateDto } from "@/api";

export const postgresSchema = z
  .object({
    type: z.literal("postgres"),
    database_connection_string: z
      .string()
      .min(1, "Connection string is required")
      .regex(
        /^(postgres(ql)?:\/\/)([^:@\s]+)(:[^@\s]*)?@([^:\s]+)(:\d+)?\/[\w-]+(\?.*)?$/,
        "Invalid Postgres connection string. Example: postgres://user:password@host:5432/database"
      ),
    database_query: z.string().optional(),
  })
  .merge(generalSchema)
  .merge(intervalsSchema)
  .merge(notificationsSchema)
  .merge(tagsSchema);

export type PostgresForm = z.infer<typeof postgresSchema>;

export const postgresDefaultValues: PostgresForm = {
  type: "postgres",
  database_connection_string: "postgres://user:password@localhost:5432/database",
  database_query: "SELECT 1",
  ...generalDefaultValues,
  ...intervalsDefaultValues,
  ...notificationsDefaultValues,
  ...tagsDefaultValues,
};

export const deserialize = (data: MonitorMonitorResponseDto): PostgresForm => {
  let config: Partial<{ database_connection_string: string; database_query?: string }> = {};
  try {
    config = data.config ? JSON.parse(data.config) : {};
  } catch (error) {
    console.error("Failed to parse Postgres monitor config:", error);
    config = {};
  }

  return {
    type: "postgres",
    name: data.name || "My Postgres Monitor",
    interval: data.interval || 60,
    timeout: data.timeout || 16,
    max_retries: data.max_retries ?? 3,
    retry_interval: data.retry_interval || 60,
    resend_interval: data.resend_interval ?? 10,
    notification_ids: data.notification_ids || [],
    tag_ids: data.tag_ids || [],
    database_connection_string: config.database_connection_string || "postgres://user:password@localhost:5432/database",
    database_query: config.database_query || "SELECT 1",
  };
};

export const serialize = (formData: PostgresForm): MonitorCreateUpdateDto => {
  const config = {
    database_connection_string: formData.database_connection_string,
    database_query: formData.database_query,
  };

  return {
    type: "postgres",
    name: formData.name,
    interval: formData.interval,
    max_retries: formData.max_retries,
    retry_interval: formData.retry_interval,
    notification_ids: formData.notification_ids,
    resend_interval: formData.resend_interval,
    timeout: formData.timeout,
    config: JSON.stringify(config),
    tag_ids: formData.tag_ids,
  };
};
