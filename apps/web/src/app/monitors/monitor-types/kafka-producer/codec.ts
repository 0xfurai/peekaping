import type { MonitorMonitorResponseDto } from "@/api";
import { formatOnServer, getString, getStrings, textTarget, type MonitorConfig, type MonitorTarget } from "../monitor-target";
import { deserializeMonitorBase, serializeMonitorBase } from "../shared/codec";
import type { KafkaProducerFormValues } from "./schema";

export interface KafkaProducerConfig {
  brokers: string[];
  topic: string;
  message: string;
  allow_auto_topic_creation: boolean;
  ssl: boolean;
  sasl_options: {
    mechanism: string;
    username: string;
    password: string;
  };
}

export const serialize = (values: KafkaProducerFormValues) => {
  const config: KafkaProducerConfig = {
    brokers: values.brokers,
    topic: values.topic,
    message: values.message,
    allow_auto_topic_creation: values.allow_auto_topic_creation,
    ssl: values.ssl,
    sasl_options: {
      mechanism: values.sasl_mechanism,
      username: values.sasl_username || "",
      password: values.sasl_password || "",
    },
  };

  return {
    type: values.type,
    ...serializeMonitorBase(values),
    config: JSON.stringify(config),
  };
};

export const formatTarget = (config: MonitorConfig): MonitorTarget | null =>
  textTarget(
    formatOnServer(getString(config, "topic"), getStrings(config, "brokers").join(", ") || null)
  );

export const deserialize = (
  data: MonitorMonitorResponseDto
): KafkaProducerFormValues => {
  const parsedConfig: KafkaProducerConfig = data.config
    ? JSON.parse(data.config)
    : {};

  return {
    type: "kafka-producer",
    ...deserializeMonitorBase(data),
    brokers: parsedConfig.brokers || ["localhost:9092"],
    topic: parsedConfig.topic || "test-topic",
    message: parsedConfig.message || '{"status": "up"}',
    allow_auto_topic_creation: parsedConfig.allow_auto_topic_creation || false,
    ssl: parsedConfig.ssl || false,
    sasl_mechanism: (parsedConfig.sasl_options?.mechanism || "None") as "None" | "PLAIN" | "SCRAM-SHA-256" | "SCRAM-SHA-512",
    sasl_username: parsedConfig.sasl_options?.username || "",
    sasl_password: parsedConfig.sasl_options?.password || "",
  };
};
