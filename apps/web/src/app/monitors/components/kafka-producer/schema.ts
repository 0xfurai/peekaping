import { z } from "zod";
import { generalDefaultValues, generalSchema } from "../shared/general";
import { intervalsDefaultValues, intervalsSchema } from "../shared/intervals";
import { notificationsSchema } from "../shared/notifications";
import { tagsSchema } from "../shared/tags";
import type { MonitorMonitorResponseDto } from "@/api";

export const kafkaProducerSchema = z
  .object({
    type: z.literal("kafka-producer"),
    brokers: z.array(z.string().min(1, "Broker address is required")).min(1, "At least one broker is required"),
    topic: z.string().min(1, "Topic is required"),
    message: z.string().min(1, "Message is required"),
    allow_auto_topic_creation: z.boolean(),
    ssl: z.boolean(),
    sasl_mechanism: z.enum(["None", "PLAIN", "SCRAM-SHA-256", "SCRAM-SHA-512"]),
    sasl_username: z.string(),
    sasl_password: z.string(),
  })
  .merge(generalSchema)
  .merge(intervalsSchema)
  .merge(notificationsSchema)
  .merge(tagsSchema);

export type KafkaProducerForm = z.infer<typeof kafkaProducerSchema>;

export const kafkaProducerDefaultValues: KafkaProducerForm = {
  type: "kafka-producer",
  brokers: ["localhost:9092"],
  topic: "test-topic",
  message: '{"status": "up", "timestamp": "' + new Date().toISOString() + '"}',
  allow_auto_topic_creation: false,
  ssl: false,
  sasl_mechanism: "None",
  sasl_username: "",
  sasl_password: "",
  ...generalDefaultValues,
  ...intervalsDefaultValues,
  notification_ids: [],
  tag_ids: [],
};

export interface KafkaProducerExecutorConfig {
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

export const serialize = (data: KafkaProducerForm) => {
  const config: KafkaProducerExecutorConfig = {
    brokers: data.brokers,
    topic: data.topic,
    message: data.message,
    allow_auto_topic_creation: data.allow_auto_topic_creation,
    ssl: data.ssl,
    sasl_options: {
      mechanism: data.sasl_mechanism,
      username: data.sasl_username || "",
      password: data.sasl_password || "",
    },
  };

  return {
    type: data.type,
    name: data.name,
    interval: data.interval,
    timeout: data.timeout,
    max_retries: data.max_retries,
    retry_interval: data.retry_interval,
    resend_interval: data.resend_interval,
    notification_ids: data.notification_ids,
    tag_ids: data.tag_ids,
    config: JSON.stringify(config),
  };
};

export const deserialize = (
  monitor: MonitorMonitorResponseDto
): KafkaProducerForm => {
  const parsedConfig: KafkaProducerExecutorConfig = monitor.config
    ? JSON.parse(monitor.config)
    : {};

  return {
    type: "kafka-producer",
    name: monitor.name || "",
    interval: monitor.interval || 60,
    timeout: monitor.timeout || 16,
    max_retries: monitor.max_retries || 3,
    retry_interval: monitor.retry_interval || 60,
    resend_interval: monitor.resend_interval || 10,
    notification_ids: monitor.notification_ids || [],
    tag_ids: monitor.tag_ids || [],
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
