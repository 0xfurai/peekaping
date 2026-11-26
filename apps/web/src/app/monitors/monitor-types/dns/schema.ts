import { z } from "zod";
import { monitorBaseSchema, monitorBaseDefaultValues } from "../shared/schema";

export const dnsSchema = z
  .object({
    type: z.literal("dns"),
    host: z.string().min(1, "Host is required"),
    resolver_server: z.string().ip("Invalid IP address"),
    port: z.coerce.number().min(1).max(65535),
    resolve_type: z.enum([
      "A",
      "AAAA",
      "CAA",
      "CNAME",
      "MX",
      "NS",
      "PTR",
      "SOA",
      "SRV",
      "TXT",
    ]),
  })
  .merge(monitorBaseSchema);

export type DnsFormValues = z.infer<typeof dnsSchema>;

export const dnsDefaultValues: DnsFormValues = {
  type: "dns",
  host: "example.com",
  resolver_server: "1.1.1.1",
  port: 53,
  resolve_type: "A",
  ...monitorBaseDefaultValues,
};
