import { z } from "zod";
import { monitorBaseSchema, monitorBaseDefaultValues } from "../shared/schema";

export const rabbitMQSchema = z
  .object({
    type: z.literal("rabbitmq"),
    nodes: z.array(z.object({
      url: z.string().url("Must be a valid URL")
    })).min(1, "At least one node is required"),
    username: z.string().min(1, "Username is required"),
    password: z.string().min(1, "Password is required"),
  })
  .merge(monitorBaseSchema);

export type RabbitMQFormValues = z.infer<typeof rabbitMQSchema>;

export const rabbitMQDefaultValues: RabbitMQFormValues = {
  type: "rabbitmq",
  nodes: [{ url: "https://localhost:15672" }],
  username: "",
  password: "",
  ...monitorBaseDefaultValues,
};
