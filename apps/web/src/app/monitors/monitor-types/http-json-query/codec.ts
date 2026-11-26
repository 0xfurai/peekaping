import { normalizeHttpHeaders } from "../http/codec";
import type { MonitorMonitorResponseDto, MonitorCreateUpdateDto } from "@/api";
import type { HttpJsonQueryFormValues } from "./schema";
import { deserializeMonitorBase, serializeMonitorBase } from "../shared/codec";

export interface HttpJsonQueryConfig {
  url: string;
  method: "GET" | "POST" | "PUT" | "DELETE" | "PATCH" | "HEAD" | "OPTIONS";
  headers?: string;
  encoding: "json" | "form" | "xml" | "text";
  body?: string;
  accepted_statuscodes: Array<"2XX" | "3XX" | "4XX" | "5XX">;
  max_redirects?: number;
  ignore_tls_errors: boolean;

  // JSON query validation fields
  json_query?: string;
  json_condition?: "==" | "!=" | ">" | "<" | ">=" | "<=";
  expected_value?: string;

  // Authentication fields
  authMethod: "none" | "basic" | "oauth2-cc" | "ntlm" | "mtls";
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
  check_cert_expiry: boolean;
}

export const deserialize = (data: MonitorMonitorResponseDto): HttpJsonQueryFormValues => {
  let config: Partial<HttpJsonQueryConfig> = {};
  try {
    config = data.config ? JSON.parse(data.config) : {};
  } catch (error) {
    console.error("Failed to parse HTTP JSON query monitor config:", error);
    config = {};
  }

  // Build authentication object based on authMethod
  const authMethod = config.authMethod || "none";
  let authentication: HttpJsonQueryFormValues["authentication"];

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
    type: "http-json-query",
    ...deserializeMonitorBase(data),
    proxy_id: data.proxy_id || "",
    url: config.url || "https://example.com",
    accepted_statuscodes: config.accepted_statuscodes || ["2XX"],
    max_redirects: config.max_redirects ?? 10,
    ignore_tls_errors: config.ignore_tls_errors || false,
    httpOptions: {
      method: config.method || "GET",
      encoding: config.encoding || "json",
      headers: normalizeHttpHeaders(config.headers, config.body),
      body: config.body || "",
    },
    authentication,
    check_cert_expiry: config.check_cert_expiry ?? false,
    json_query: config.json_query || "",
    json_condition: (config.json_condition as "==" | "!=" | ">" | "<" | ">=" | "<=") || "==",
    expected_value: config.expected_value || "",
  };
};

export const serialize = (values: HttpJsonQueryFormValues): MonitorCreateUpdateDto => {
  const config: HttpJsonQueryConfig = {
    url: values.url,
    method: values.httpOptions.method,
    headers: values.httpOptions.headers,
    encoding: values.httpOptions.encoding,
    body: values.httpOptions.body,
    accepted_statuscodes: values.accepted_statuscodes as Array<"2XX" | "3XX" | "4XX" | "5XX">,
    max_redirects: values.max_redirects,
    ignore_tls_errors: values.ignore_tls_errors,
    authMethod: values.authentication.authMethod,
    check_cert_expiry: values.check_cert_expiry,

    // JSON query validation fields
    json_query: values.json_query || "",
    json_condition: values.json_condition,
    expected_value: values.expected_value || "",

    // Include authentication fields based on method
    ...(values.authentication.authMethod === "basic" && {
      basic_auth_user: values.authentication.basic_auth_user,
      basic_auth_pass: values.authentication.basic_auth_pass,
    }),
    ...(values.authentication.authMethod === "oauth2-cc" && {
      oauth_auth_method: values.authentication.oauth_auth_method,
      oauth_token_url: values.authentication.oauth_token_url,
      oauth_client_id: values.authentication.oauth_client_id,
      oauth_client_secret: values.authentication.oauth_client_secret,
      oauth_scopes: values.authentication.oauth_scopes,
    }),
    ...(values.authentication.authMethod === "ntlm" && {
      basic_auth_user: values.authentication.basic_auth_user,
      basic_auth_pass: values.authentication.basic_auth_pass,
      authDomain: values.authentication.authDomain,
      authWorkstation: values.authentication.authWorkstation,
    }),
    ...(values.authentication.authMethod === "mtls" && {
      tlsCert: values.authentication.tlsCert,
      tlsKey: values.authentication.tlsKey,
      tlsCa: values.authentication.tlsCa,
    }),
  };

  return {
    type: "http-json-query",
    ...serializeMonitorBase(values),
    proxy_id: values.proxy_id,
    config: JSON.stringify(config),
  };
};
