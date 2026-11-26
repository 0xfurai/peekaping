import type { MonitorCreateUpdateDto, MonitorMonitorResponseDto } from "@/api";
import type { MongoDBFormValues } from "./schema";
import { deserializeMonitorBase, serializeMonitorBase } from "../shared/codec";

interface MongoDBConfig {
  connectionString: string;
  command?: string;
  jsonPath?: string;
  expectedValue?: string;
}

export const deserialize = (data: MonitorMonitorResponseDto): MongoDBFormValues => {
  let config: MongoDBConfig = {
    connectionString: "mongodb://username:password@host:27017/database",
    command: '{"ping": 1}',
    jsonPath: "$",
    expectedValue: "",
  };

  if (data.config) {
    try {
      const parsedConfig = JSON.parse(data.config);
      config = {
        connectionString:
          parsedConfig.connectionString ||
          "mongodb://username:password@host:27017/database",
        command: parsedConfig.command || '{"ping": 1}',
        jsonPath: parsedConfig.jsonPath || "$",
        expectedValue: parsedConfig.expectedValue || "",
      };
    } catch (error) {
      console.error("Failed to parse MongoDB monitor config:", error);
    }
  }

  return {
    type: "mongodb",
    ...deserializeMonitorBase(data),
    connectionString: config.connectionString,
    command: config.command,
    jsonPath: config.jsonPath,
    expectedValue: config.expectedValue,
  };
};

export const serialize = (values: MongoDBFormValues): MonitorCreateUpdateDto => {
  const config: MongoDBConfig = {
    connectionString: values.connectionString,
    command: values.command,
    jsonPath: values.jsonPath,
    expectedValue: values.expectedValue,
  };

  return {
    type: "mongodb",
    ...serializeMonitorBase(values),
    config: JSON.stringify(config),
  };
};
