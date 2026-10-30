import { z } from "zod";
import { monitorBaseSchema, monitorBaseDefaultValues } from "../shared/schema";

// MongoDB connection string regex pattern
// Format: mongodb://username:password@host:port/database or mongodb+srv://username:password@host/database
// Also supports: mongodb://host:port/database (without authentication)
// and a seed list of several hosts: mongodb://host1:port,host2:port/database?replicaSet=rs0
// A host is a name, an IPv4 address or an IPv6 address in brackets, with an optional port. The
// server checks the rest (port range, mongodb+srv taking one host with no port).
const MONGODB_HOST = "(?:\\[[^\\]\\s]+\\]|[^\\s:,/?@\\[\\]]+)(?::\\d+)?";
const MONGODB_CONNECTION_STRING_REGEX = new RegExp(
  `^mongodb(\\+srv)?://(?:[^@/?]*@)?${MONGODB_HOST}(?:,${MONGODB_HOST})*/([^?]+)(?:\\?(.*))?$`
);

export const mongodbSchema = z
  .object({
    type: z.literal("mongodb"),
    connectionString: z
      .string()
      .min(1, "Connection string is required")
      .regex(
        MONGODB_CONNECTION_STRING_REGEX,
        "Connection string must be in format: mongodb://[username:password@]host[:port]/database or mongodb+srv://[username:password@]host/database. A replica set can list several hosts: mongodb://host1:port,host2:port/database"
      ),
    command: z.string().optional(),
    jsonPath: z.string().optional(),
    expectedValue: z.string().optional(),
  })
  .merge(monitorBaseSchema);

export type MongoDBFormValues = z.infer<typeof mongodbSchema>;

export const mongodbDefaultValues: MongoDBFormValues = {
  type: "mongodb",
  connectionString: "mongodb://username:password@host:27017/database",
  command: '{"ping": 1}',
  jsonPath: "$",
  expectedValue: "",
  ...monitorBaseDefaultValues,
};
