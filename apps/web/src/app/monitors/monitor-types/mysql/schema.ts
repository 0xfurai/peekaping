import { z } from "zod";
import { monitorBaseSchema, monitorBaseDefaultValues } from "../shared/schema";

// MySQL connection string regex pattern
// Format: mysql://username:password@host:port/database?params
const MYSQL_CONNECTION_STRING_REGEX = new RegExp(
  "^mysql://([^:@]+)(?::([^@]*))?@([^:/]+)(?::(\\d+))?/([^?]+)(?:\\?(.*))?$"
);

export const mysqlSchema = z
  .object({
    type: z.literal("mysql"),
    connection_string: z
      .string()
      .min(1, "Connection string is required")
      .regex(
        MYSQL_CONNECTION_STRING_REGEX,
        "Connection string must be in format: mysql://username:password@host:port/database"
      ),
    query: z.string().optional(),
  })
  .merge(monitorBaseSchema);

export type MySQLFormValues = z.infer<typeof mysqlSchema>;

export const mysqlDefaultValues: MySQLFormValues = {
  type: "mysql",
  connection_string: "mysql://username:password@host:3306/database",
  query: "SELECT 1",
  ...monitorBaseDefaultValues,
};
