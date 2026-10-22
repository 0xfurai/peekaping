import { z } from "zod";
import { monitorBaseSchema, monitorBaseDefaultValues } from "../shared/schema";

// Regex to validate SQL Server connection string format:
// Server=hostname,port;Database=database;User Id=username;Password=password;Encrypt=true/false;TrustServerCertificate=true/false;Connection Timeout=seconds
const sqlServerConnectionStringRegex = /^Server=([^;,]+)(,\d+)?;Database=[^;]+;User Id=[^;]+;Password=[^;]*;?.*$/i;

export const sqlServerSchema = z
  .object({
    type: z.literal("sqlserver"),
    database_connection_string: z
      .string()
      .min(1, "Connection string is required")
      .regex(
        sqlServerConnectionStringRegex,
        "Invalid SQL Server connection string. Example: Server=localhost,1433;Database=master;User Id=sa;Password=password;Encrypt=false;TrustServerCertificate=true;Connection Timeout=30"
      ),
    database_query: z.string().optional(),
  })
  .merge(monitorBaseSchema);

export type SqlServerFormValues = z.infer<typeof sqlServerSchema>;

export const sqlServerDefaultValues: SqlServerFormValues = {
  type: "sqlserver",
  database_connection_string: "",
  database_query: "SELECT 1",
  ...monitorBaseDefaultValues,
};
