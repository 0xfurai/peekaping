import type { MonitorCreateUpdateDto, MonitorMonitorResponseDto } from "@/api";
import type { MqttFormValues } from "./schema";
import { formatHostPort, formatOnServer, getNumber, getString, textTarget, type MonitorConfig, type MonitorTarget } from "../monitor-target";
import { deserializeMonitorBase, serializeMonitorBase } from "../shared/codec";

interface MqttConfig {
  hostname: string;
  port: number;
  topic: string;
  username?: string;
  password?: string;
  check_type: string;
  success_keyword?: string;
  json_path?: string;
  expected_value?: string;
}

export const serialize = (values: MqttFormValues): MonitorCreateUpdateDto => {
  const config: MqttConfig = {
    hostname: values.hostname,
    port: values.port,
    topic: values.topic,
    username: values.username,
    password: values.password,
    check_type: values.check_type,
    success_keyword: values.success_keyword,
    json_path: values.json_path,
    expected_value: values.expected_value,
  };

  return {
    type: values.type,
    ...serializeMonitorBase(values),
    config: JSON.stringify(config),
  };
};

export const formatTarget = (config: MonitorConfig): MonitorTarget | null =>
  textTarget(
    formatOnServer(
      getString(config, "topic"),
      formatHostPort(getString(config, "hostname"), getNumber(config, "port"))
    )
  );

export const deserialize = (data: MonitorMonitorResponseDto): MqttFormValues => {
  const config: MqttConfig = data.config ? JSON.parse(data.config) : {};

  return {
    type: "mqtt",
    ...deserializeMonitorBase(data),
    hostname: config.hostname || "localhost",
    port: config.port || 1883,
    topic: config.topic || "test/topic",
    username: config.username || "",
    password: config.password || "",
    check_type: (config.check_type as "keyword" | "json-query" | "none") || "none",
    success_keyword: config.success_keyword || "",
    json_path: config.json_path || "",
    expected_value: config.expected_value || "",
  };
};
