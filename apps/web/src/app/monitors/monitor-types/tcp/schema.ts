import { z } from "zod";
import { monitorBaseSchema, monitorBaseDefaultValues } from "../shared/schema";

export const tcpSchema = z
  .object({
    type: z.literal("tcp"),
    host: z.string().min(1, "Host is required"),
    port: z.coerce.number().min(1, "Port must be at least 1").max(65535, "Port must be at most 65535"),
  })
  .merge(monitorBaseSchema);

export type TcpFormValues = z.infer<typeof tcpSchema>;

export const tcpDefaultValues: TcpFormValues = {
  type: "tcp",
  host: "example.com",
  port: 80,
  ...monitorBaseDefaultValues,
};
