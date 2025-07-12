import z from "zod";
import { advancedDefaultValues, advancedSchema } from "./advanced";
import { httpOptionsDefaultValues, httpOptionsSchema } from "./options";
import { authenticationDefaultValues, authenticationSchema } from "./authentication";
import { generalDefaultValues, generalSchema } from "../shared/general";
import { intervalsDefaultValues, intervalsSchema } from "../shared/intervals";
import { notificationsDefaultValues, notificationsSchema } from "../shared/notifications";
import { proxiesDefaultValues, proxiesSchema } from "../shared/proxies";
import { tagsDefaultValues, tagsSchema } from "../shared/tags";
import type { MonitorMonitorResponseDto, MonitorCreateUpdateDto } from "@/api";

export const httpSchema = z
  .object({
    type: z.literal("http"),
    url: z.string().url({ message: "Invalid URL" }),
  })
  .merge(generalSchema)
  .merge(intervalsSchema)
  .merge(notificationsSchema)
  .merge(proxiesSchema)
  .merge(tagsSchema)
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
  ...tagsDefaultValues,
  ...advancedDefaultValues,

  httpOptions: httpOptionsDefaultValues,
  authentication: authenticationDefaultValues,
};

export const deserialize = (data: MonitorMonitorResponseDto): HttpForm => {
  let config: Partial<HttpExecutorConfig> = {};
  try {
    config = data.config ? JSON.parse(data.config) : {};
  } catch (error) {
    console.error("Failed to parse HTTP monitor config:", error);
    config = {};
  }

  // Build authentication object based on authMethod
  const authMethod = config.authMethod || "none";
  let authentication: HttpForm["authentication"];

  switch (authMethod) {
    case "basic":
      authentication = {
        authMethod: "basic",
        basic_auth_user: config.basic_auth_user || "",
        basic_auth_pass: config.basic_auth_pass || "",
      };
      break;
    case "oauth2-cc":
      authentication = {
        authMethod: "oauth2-cc",
        oauth_auth_method: (config.oauth_auth_method === "client_secret_post"
          ? "client_secret_post"
          : "client_secret_basic") as "client_secret_basic" | "client_secret_post",
        oauth_token_url: config.oauth_token_url || "",
        oauth_client_id: config.oauth_client_id || "",
        oauth_client_secret: config.oauth_client_secret || "",
        oauth_scopes: config.oauth_scopes || "",
      };
      break;
    case "ntlm":
      authentication = {
        authMethod: "ntlm",
        basic_auth_user: config.basic_auth_user || "",
        basic_auth_pass: config.basic_auth_pass || "",
        authDomain: config.authDomain || "",
        authWorkstation: config.authWorkstation || "",
      };
      break;
    case "mtls":
      authentication = {
        authMethod: "mtls",
        tlsCert: config.tlsCert || "",
        tlsKey: config.tlsKey || "",
        tlsCa: config.tlsCa || "",
      };
      break;
    default:
      authentication = {
        authMethod: "none",
      };
  }

  return {
    type: "http",
    name: data.name || "My Monitor",
    interval: data.interval || 60,
    timeout: data.timeout || 16,
    max_retries: data.max_retries || 3,
    retry_interval: data.retry_interval || 60,
    resend_interval: data.resend_interval || 10,
    notification_ids: data.notification_ids || [],
    tag_ids: data.tag_ids || [],
    proxy_id: data.proxy_id || "",
    url: config.url || "https://example.com",
    accepted_statuscodes: config.accepted_statuscodes || ["2XX"],
    max_redirects: config.max_redirects || 10,
    ignore_tls_errors: config.ignore_tls_errors || false,
    httpOptions: {
      method: config.method || "GET",
      encoding: config.encoding || "json",
      headers: config.headers || '{ "Content-Type": "application/json" }',
      body: config.body || "",
    },
    authentication,
  };
};

export const serialize = (formData: HttpForm): MonitorCreateUpdateDto => {
  const config: HttpExecutorConfig = {
    url: formData.url,
    method: formData.httpOptions.method,
    headers: formData.httpOptions.headers,
    encoding: formData.httpOptions.encoding,
    body: formData.httpOptions.body,
    accepted_statuscodes: formData.accepted_statuscodes as Array<"2XX" | "3XX" | "4XX" | "5XX">,
    max_redirects: formData.max_redirects,
    ignore_tls_errors: formData.ignore_tls_errors,
    authMethod: formData.authentication.authMethod,

    // Include authentication fields based on method
    ...(formData.authentication.authMethod === "basic" && {
      basic_auth_user: formData.authentication.basic_auth_user,
      basic_auth_pass: formData.authentication.basic_auth_pass,
    }),
    ...(formData.authentication.authMethod === "oauth2-cc" && {
      oauth_auth_method: formData.authentication.oauth_auth_method,
      oauth_token_url: formData.authentication.oauth_token_url,
      oauth_client_id: formData.authentication.oauth_client_id,
      oauth_client_secret: formData.authentication.oauth_client_secret,
      oauth_scopes: formData.authentication.oauth_scopes,
    }),
    ...(formData.authentication.authMethod === "ntlm" && {
      basic_auth_user: formData.authentication.basic_auth_user,
      basic_auth_pass: formData.authentication.basic_auth_pass,
      authDomain: formData.authentication.authDomain,
      authWorkstation: formData.authentication.authWorkstation,
    }),
    ...(formData.authentication.authMethod === "mtls" && {
      tlsCert: formData.authentication.tlsCert,
      tlsKey: formData.authentication.tlsKey,
      tlsCa: formData.authentication.tlsCa,
    }),
  };

  return {
    type: "http",
    name: formData.name,
    interval: formData.interval,
    max_retries: formData.max_retries,
    retry_interval: formData.retry_interval,
    notification_ids: formData.notification_ids,
    tag_ids: formData.tag_ids,
    proxy_id: formData.proxy_id,
    resend_interval: formData.resend_interval,
    timeout: formData.timeout,
    config: JSON.stringify(config),
  };
};

export interface HttpExecutorConfig {
  url: string; // required, must be a valid URL

  method: "GET" | "POST" | "PUT" | "DELETE" | "PATCH" | "HEAD" | "OPTIONS"; // required

  headers?: string; // optional, must be valid JSON if present
  encoding: "json" | "form" | "xml" | "text"; // required

  body?: string; // optional

  accepted_statuscodes: Array<"2XX" | "3XX" | "4XX" | "5XX">; // required, at least one

  max_redirects?: number; // optional, must be >= 0 if present
  ignore_tls_errors: boolean; // defaults to false

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
