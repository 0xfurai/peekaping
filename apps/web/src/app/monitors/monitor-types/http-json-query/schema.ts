import { z } from "zod";
import { advancedDefaultValues, advancedSchema, httpOptionsDefaultValues, httpOptionsSchema, authenticationDefaultValues, authenticationSchema } from "../http/schema";
import { proxiesDefaultValues, proxiesSchema, monitorBaseSchema, monitorBaseDefaultValues } from "../shared/schema";

export const httpJsonQuerySchema = z
  .object({
    type: z.literal("http-json-query"),
    url: z.string().url({ message: "Invalid URL" }),
    json_query: z.string().optional(),
    json_condition: z.enum(["==", "!=", ">", "<", ">=", "<="]).optional(),
    expected_value: z.string().optional(),
  })
  .merge(monitorBaseSchema)
  .merge(proxiesSchema)
  .merge(advancedSchema)
  .merge(
    z.object({
      httpOptions: httpOptionsSchema,
    })
  )
  .merge(
    z.object({
      authentication: authenticationSchema,
    })
  );

export type HttpJsonQueryFormValues = z.infer<typeof httpJsonQuerySchema>;

export const httpJsonQueryDefaultValues: HttpJsonQueryFormValues = {
  type: "http-json-query",
  url: "https://example.com",
  json_query: "",
  json_condition: "==",
  expected_value: "",
  ...monitorBaseDefaultValues,
  ...proxiesDefaultValues,
  ...advancedDefaultValues,

  httpOptions: httpOptionsDefaultValues,
  authentication: authenticationDefaultValues,
};
