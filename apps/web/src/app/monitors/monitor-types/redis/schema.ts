import { z } from "zod";
import { monitorBaseSchema, monitorBaseDefaultValues } from "../shared/schema";

// Redis connection string validation regex
// Updated to support IPv6 addresses in brackets [::1] or without brackets ::1
const redisConnectionStringRegex =
  /^(rediss?:\/\/)([^@]*@)?(\[[^\]]+\]|[^:/]+)(:\d{1,5})?(\/[0-9]*)?$/;

export const redisSchema = z
  .object({
    type: z.literal("redis"),
    databaseConnectionString: z
      .string()
      .min(1, "Database connection string is required")
      .regex(
        redisConnectionStringRegex,
        "Invalid Redis connection string format. Expected: redis://[user:password@]host[:port][/db] or rediss://[user:password@]host[:port][/db]"
      )
      .refine(
        (value) => {
          if (!value) return false;

          // Basic format check
          if (!redisConnectionStringRegex.test(value)) return false;

          // Parse the URL to validate components
          try {
            const url = new URL(value);

            // Check protocol
            if (!["redis:", "rediss:"].includes(url.protocol)) return false;

            // Check hostname
            if (!url.hostname || url.hostname.length === 0) return false;

            // Check port (optional, but if present should be valid)
            if (url.port) {
              const port = parseInt(url.port);
              if (isNaN(port) || port < 1 || port > 65535) return false;
            }

            // Check database number (optional, but if present should be valid)
            if (url.pathname && url.pathname !== "/") {
              const dbNumber = parseInt(url.pathname.slice(1));
              if (isNaN(dbNumber) || dbNumber < 0) return false;
            }

            return true;
          } catch {
            return false;
          }
        },
        {
          message:
            "Invalid Redis connection string. Please check the format and ensure host, port (1-65535), and database number (0+) are valid.",
        }
      ),
    ignoreTls: z.boolean(),
    caCert: z.string().optional(),
    clientCert: z.string().optional(),
    clientKey: z.string().optional(),
  })
  .merge(monitorBaseSchema);

export type RedisFormValues = z.infer<typeof redisSchema>;

export const redisDefaultValues: RedisFormValues = {
  type: "redis",
  databaseConnectionString: "redis://user:password@host:port",
  ignoreTls: false,
  caCert: "",
  clientCert: "",
  clientKey: "",
  ...monitorBaseDefaultValues,
};
