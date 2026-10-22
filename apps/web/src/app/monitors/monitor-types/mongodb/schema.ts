import { z } from "zod";
import { monitorBaseSchema, monitorBaseDefaultValues } from "../shared/schema";

// MongoDB connection string regex pattern
// Format: mongodb://username:password@host:port/database or mongodb+srv://username:password@host/database
// Also supports: mongodb://host:port/database (without authentication)
const MONGODB_CONNECTION_STRING_REGEX = new RegExp(
  "^mongodb(\\+srv)?://(?:([^:@/]+)(?::([^@/]*))?@)?([^:/@]+)(?::(\\d+))?/([^?]+)(?:\\?(.*))?$"
);

export const mongodbSchema = z
  .object({
    type: z.literal("mongodb"),
    connectionString: z
      .string()
      .min(1, "Connection string is required")
      .regex(
        MONGODB_CONNECTION_STRING_REGEX,
        "Connection string must be in format: mongodb://[username:password@]host[:port]/database or mongodb+srv://[username:password@]host/database"
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
