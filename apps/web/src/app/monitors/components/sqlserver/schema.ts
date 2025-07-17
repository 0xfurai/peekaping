import { z } from "zod";
import { generalDefaultValues, generalSchema } from "../shared/general";
import { intervalsDefaultValues, intervalsSchema } from "../shared/intervals";
import { notificationsDefaultValues, notificationsSchema } from "../shared/notifications";
import { tagsDefaultValues, tagsSchema } from "../shared/tags";
import type { MonitorMonitorResponseDto, MonitorCreateUpdateDto } from "@/api";

// Regex to validate SQL Server connection string format:
// Server=hostname,port;Database=database;User Id=username;Password=password;Encrypt=true/false;TrustServerCertificate=true/false;Connection Timeout=seconds
const sqlServerConnectionStringRegex = /^Server=([^;,]+)(,\d+)?;Database=[^;]+;User Id=[^;]+;Password=[^;]*;?.*$/i;

export const sqlServerSchema = z
  .object({
    type: z.literal("sqlserver"),
    database_connection_string: z
      .string()
      .min(1, "Connection string is required")
      .regex(
        sqlServerConnectionStringRegex,
        "Invalid SQL Server connection string. Example: Server=localhost,1433;Database=master;User Id=sa;Password=password;Encrypt=false;TrustServerCertificate=true;Connection Timeout=30"
      ),
    database_query: z.string().optional(),
  })
  .merge(generalSchema)
  .merge(intervalsSchema)
  .merge(notificationsSchema)
  .merge(tagsSchema);

export type SQLServerForm = z.infer<typeof sqlServerSchema>;

export const sqlServerDefaultValues: SQLServerForm = {
  type: "sqlserver",
  database_connection_string: "",
  database_query: "SELECT 1",
  ...generalDefaultValues,
  ...intervalsDefaultValues,
  ...notificationsDefaultValues,
  ...tagsDefaultValues,
};

export const deserialize = (data: MonitorMonitorResponseDto): SQLServerForm => {
  let config: Partial<{ database_connection_string: string; database_query?: string }> = {};
  try {
    config = data.config ? JSON.parse(data.config) : {};
  } catch (error) {
    console.error("Failed to parse SQL Server monitor config:", error);
    config = {};
  }

  return {
    type: "sqlserver",
    name: data.name || "My SQL Server Monitor",
    interval: data.interval || 60,
    timeout: data.timeout || 16,
    max_retries: data.max_retries ?? 3,
    retry_interval: data.retry_interval || 60,
    resend_interval: data.resend_interval ?? 10,
    notification_ids: data.notification_ids || [],
    tag_ids: data.tag_ids || [],
    database_connection_string: config.database_connection_string || "",
    database_query: config.database_query || "SELECT 1",
  };
};

export const serialize = (formData: SQLServerForm): MonitorCreateUpdateDto => {
  const config = {
    database_connection_string: formData.database_connection_string,
    database_query: formData.database_query,
  };

  return {
    type: "sqlserver",
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
