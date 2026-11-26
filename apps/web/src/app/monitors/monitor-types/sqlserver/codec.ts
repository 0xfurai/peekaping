import type { MonitorMonitorResponseDto, MonitorCreateUpdateDto } from "@/api";
import { deserializeMonitorBase, serializeMonitorBase } from "../shared/codec";
import type { SqlServerFormValues } from "./schema";

export const deserialize = (data: MonitorMonitorResponseDto): SqlServerFormValues => {
  let config: Partial<{ database_connection_string: string; database_query?: string }> = {};
  try {
    config = data.config ? JSON.parse(data.config) : {};
  } catch (error) {
    console.error("Failed to parse SQL Server monitor config:", error);
    config = {};
  }

  return {
    type: "sqlserver",
    ...deserializeMonitorBase(data),
    database_connection_string: config.database_connection_string || "",
    database_query: config.database_query || "SELECT 1",
  };
};

export const serialize = (values: SqlServerFormValues): MonitorCreateUpdateDto => {
  const config = {
    database_connection_string: values.database_connection_string,
    database_query: values.database_query,
  };

  return {
    type: "sqlserver",
    ...serializeMonitorBase(values),
    config: JSON.stringify(config),
  };
};
