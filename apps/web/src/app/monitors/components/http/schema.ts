import z from "zod";
import { advancedDefaultValues, advancedSchema } from "./advanced";
import { httpOptionsDefaultValues, httpOptionsSchema } from "./options";
import { authenticationDefaultValues, authenticationSchema } from "./authentication";
import { generalDefaultValues, generalSchema } from "../shared/general";
import { intervalsDefaultValues, intervalsSchema } from "../shared/intervals";
import { notificationsDefaultValues, notificationsSchema } from "../shared/notifications";
import { proxiesDefaultValues, proxiesSchema } from "../shared/proxies";

export const httpSchema = z
  .object({
    type: z.literal("http"),
    url: z.string().url({ message: "Invalid URL" }),
  })
  .merge(generalSchema)
  .merge(intervalsSchema)
  .merge(notificationsSchema)
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

export type HttpForm = z.infer<typeof httpSchema>;


export const httpDefaultValues: HttpForm = {
  type: "http",
  url: "https://example.com",

  ...generalDefaultValues,
  ...intervalsDefaultValues,
  ...notificationsDefaultValues,
  ...proxiesDefaultValues,
  ...advancedDefaultValues,

  httpOptions: httpOptionsDefaultValues,
  authentication: authenticationDefaultValues,
};

export interface HttpExecutorConfig {
  url: string; // required, must be a valid URL

  method: "GET" | "POST" | "PUT" | "DELETE" | "PATCH" | "HEAD" | "OPTIONS"; // required

  headers?: string; // optional, must be valid JSON if present
  encoding: "json" | "form" | "xml" | "text"; // required

  skipTlsVerify: boolean; // defaults to false

  body?: string; // optional

  accepted_statuscodes: Array<"2XX" | "3XX" | "4XX" | "5XX">; // required, at least one

  max_redirects?: number; // optional, must be >= 0 if present

  // Authentication fields
  authMethod: "none" | "basic" | "oauth2-cc" | "ntlm" | "mtls"; // required

  basic_auth_user?: string;
  basic_auth_pass?: string;
  authDomain?: string;
  authWorkstation?: string;
  oauth_auth_method?: string;
  oauth_token_url?: string;
  oauth_client_id?: string;
  oauth_client_secret?: string;
  oauth_scopes?: string;
  tlsCert?: string;
  tlsKey?: string;
  tlsCa?: string;
}
