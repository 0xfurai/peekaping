import { z } from "zod";
import { monitorBaseSchema, monitorBaseDefaultValues } from "../shared/schema";

export const snmpSchema = z
  .object({
    type: z.literal("snmp"),
    host: z.string().min(1, "Host is required"),
    port: z.coerce
      .number()
      .min(1, "Port must be greater than 0")
      .max(65535, "Port must be less than 65536")
      .optional(),
    community: z.string().min(1, "Community is required"),
    snmp_version: z.enum(["v1", "v2c", "v3"], {
      required_error: "SNMP version is required",
    }),
    oid: z.string().min(1, "OID is required"),
    json_path: z.string().optional(),
    json_path_operator: z.enum(["eq", "ne", "lt", "gt", "le", "ge"]).optional(),
    expected_value: z.string().optional(),
  })
  .merge(monitorBaseSchema);

export type SnmpFormValues = z.infer<typeof snmpSchema>;

export const snmpDefaultValues: SnmpFormValues = {
  type: "snmp",
  host: "127.0.0.1",
  port: 161,
  community: "public",
  snmp_version: "v2c",
  oid: "1.3.6.1.2.1.1.1.0",
  json_path: "$",
  json_path_operator: "eq",
  expected_value: "",
  ...monitorBaseDefaultValues,
};
