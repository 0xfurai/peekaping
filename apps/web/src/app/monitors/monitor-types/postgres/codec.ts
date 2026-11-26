import type { MonitorMonitorResponseDto, MonitorCreateUpdateDto } from "@/api";
import type { PostgresFormValues } from "./schema";
import { deserializeMonitorBase, serializeMonitorBase } from "../shared/codec";

export const deserialize = (data: MonitorMonitorResponseDto): PostgresFormValues => {
  let config: Partial<{ database_connection_string: string; database_query?: string }> = {};
  try {
    config = data.config ? JSON.parse(data.config) : {};
  } catch (error) {
    console.error("Failed to parse Postgres monitor config:", error);
    config = {};
  }

  return {
    type: "postgres",
    ...deserializeMonitorBase(data),
    database_connection_string: config.database_connection_string || "postgres://user:password@localhost:5432/database",
    database_query: config.database_query || "SELECT 1",
  };
};

export const serialize = (values: PostgresFormValues): MonitorCreateUpdateDto => {
  const config = {
    database_connection_string: values.database_connection_string,
    database_query: values.database_query,
  };

  return {
    type: "postgres",
    ...serializeMonitorBase(values),
    config: JSON.stringify(config),
  };
};
