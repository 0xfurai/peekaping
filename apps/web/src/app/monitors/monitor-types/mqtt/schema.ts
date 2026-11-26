import { z } from "zod";
import { monitorBaseSchema, monitorBaseDefaultValues } from "../shared/schema";

export const mqttSchema = z
  .object({
    type: z.literal("mqtt"),
    hostname: z.string().min(1, "Hostname is required"),
    port: z.coerce.number().min(1).max(65535),
    topic: z.string().min(1, "Topic is required"),
    username: z.string().optional(),
    password: z.string().optional(),
    check_type: z.enum(["keyword", "json-query", "none"]),
    success_keyword: z.string().optional(),
    json_path: z.string().optional(),
    expected_value: z.string().optional(),
  })
  .merge(monitorBaseSchema);

export type MqttFormValues = z.infer<typeof mqttSchema>;

export const mqttDefaultValues: MqttFormValues = {
  type: "mqtt",
  hostname: "localhost",
  port: 1883,
  topic: "test/topic",
  username: "",
  password: "",
  check_type: "none",
  success_keyword: "",
  json_path: "",
  expected_value: "",
  ...monitorBaseDefaultValues,
};
