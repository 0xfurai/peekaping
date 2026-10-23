import { z } from "zod";
import { monitorBaseSchema, monitorBaseDefaultValues } from "../shared/schema";

export const postgresSchema = z
  .object({
    type: z.literal("postgres"),
    database_connection_string: z
      .string()
      .min(1, "Connection string is required")
      .regex(
        /^(postgres(ql)?:\/\/)([^:@\s]+)(:[^@\s]*)?@([^:\s]+)(:\d+)?\/[\w-]+(\?.*)?$/,
        "Invalid Postgres connection string. Example: postgres://user:password@host:5432/database"
      ),
    database_query: z.string().optional(),
  })
  .merge(monitorBaseSchema);

export type PostgresFormValues = z.infer<typeof postgresSchema>;

export const postgresDefaultValues: PostgresFormValues = {
  type: "postgres",
  database_connection_string: "postgres://user:password@localhost:5432/database",
  database_query: "SELECT 1",
  ...monitorBaseDefaultValues,
};
