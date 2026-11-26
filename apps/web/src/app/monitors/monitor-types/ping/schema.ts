import { z } from "zod";
import { monitorBaseSchema, monitorBaseDefaultValues } from "../shared/schema";

export const pingSchema = z
  .object({
    type: z.literal("ping"),
    host: z.string().min(1, "Host is required"),
    packet_size: z.coerce
      .number()
      .min(0, "Data size must be at least 0 bytes")
      .max(65507, "Data size must be at most 65507 bytes")
      .optional(),
  })
  .merge(monitorBaseSchema);

export type PingFormValues = z.infer<typeof pingSchema>;

export const pingDefaultValues: PingFormValues = {
  type: "ping",
  host: "example.com",
  packet_size: 32,
  ...monitorBaseDefaultValues,
};
