import { z } from "zod";
import { advancedDefaultValues, advancedSchema, httpOptionsDefaultValues, httpOptionsSchema, authenticationDefaultValues, authenticationSchema } from "../http/schema";
import { proxiesDefaultValues, proxiesSchema, monitorBaseSchema, monitorBaseDefaultValues } from "../shared/schema";

export const httpKeywordSchema = z
  .object({
    type: z.literal("http-keyword"),
    url: z.string().url({ message: "Invalid URL" }),
    keyword: z.string().min(1, { message: "Keyword is required" }),
    invert_keyword: z.boolean().optional(),
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

export type HttpKeywordFormValues = z.infer<typeof httpKeywordSchema>;

export const httpKeywordDefaultValues: HttpKeywordFormValues = {
  type: "http-keyword",
  url: "https://example.com",
  keyword: "",
  invert_keyword: false,
  ...monitorBaseDefaultValues,
  ...proxiesDefaultValues,
  ...advancedDefaultValues,

  httpOptions: httpOptionsDefaultValues,
  authentication: authenticationDefaultValues,
};
