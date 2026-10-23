import { z } from "zod";
import { monitorBaseSchema, monitorBaseDefaultValues } from "../shared/schema";

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
  .merge(monitorBaseSchema);

export type KafkaProducerFormValues = z.infer<typeof kafkaProducerSchema>;

export const kafkaProducerDefaultValues: KafkaProducerFormValues = {
  type: "kafka-producer",
  brokers: ["localhost:9092"],
  topic: "test-topic",
  message: '{"status": "up", "timestamp": "' + new Date().toISOString() + '"}',
  allow_auto_topic_creation: false,
  ssl: false,
  sasl_mechanism: "None",
  sasl_username: "",
  sasl_password: "",
  ...monitorBaseDefaultValues,
};
