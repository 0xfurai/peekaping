import type { MonitorCreateUpdateDto, MonitorMonitorResponseDto } from "@/api";
import type { MySQLFormValues } from "./schema";
import { deserializeMonitorBase, serializeMonitorBase } from "../shared/codec";

interface MySQLConfig {
  connection_string: string;
  query?: string;
}

export const deserialize = (data: MonitorMonitorResponseDto): MySQLFormValues => {
  let config: MySQLConfig = {
    connection_string: "mysql://username:password@host:3306/database",
    query: "SELECT 1",
  };

  if (data.config) {
    try {
      const parsedConfig = JSON.parse(data.config);
      config = {
        connection_string: parsedConfig.connection_string || "mysql://username:password@host:3306/database",
        query: parsedConfig.query || "SELECT 1",
      };
    } catch (error) {
      console.error("Failed to parse MySQL monitor config:", error);
    }
  }

  return {
    type: "mysql",
    ...deserializeMonitorBase(data),
    connection_string: config.connection_string,
    query: config.query,
  };
};

export const serialize = (values: MySQLFormValues): MonitorCreateUpdateDto => {
  const config: MySQLConfig = {
    connection_string: values.connection_string,
    query: values.query,
  };

  return {
    type: "mysql",
    ...serializeMonitorBase(values),
    config: JSON.stringify(config),
  };
};
