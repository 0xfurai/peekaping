import { z } from "zod";
import { proxiesDefaultValues, proxiesSchema, monitorBaseSchema, monitorBaseDefaultValues } from "../shared/schema";
import { isJson, isValidForm, isValidXml } from "@/lib/utils";

export const advancedSchema = z.object({
  accepted_statuscodes: z.array(z.string()),
  max_redirects: z.coerce.number().min(0).max(30),
  ignore_tls_errors: z.boolean(),
  check_cert_expiry: z.boolean(),
});

const base = z.object({
  method: z.enum(["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"]),
  headers: z.string().refine(isJson, { message: "Invalid JSON" }),
});

const jsonSchema = base.extend({
  encoding: z.literal("json"),
  body: z.string().refine(isJson, { message: "Invalid JSON" }),
});

const formSchema = base.extend({
  encoding: z.literal("form"),
  body: z.string().refine(isValidForm, { message: "Invalid form data" }),
});

const textSchema = base.extend({
  encoding: z.literal("text"),
  body: z.string(),
});

const xmlSchema = base.extend({
  encoding: z.literal("xml"),
  body: z.string().refine(isValidXml, { message: "Invalid XML" }),
});

export const httpOptionsSchema = z.discriminatedUnion("encoding", [
  jsonSchema,
  formSchema,
  textSchema,
  xmlSchema,
]);

// Zod schema for authentication options
export const authenticationSchema = z.discriminatedUnion("authMethod", [
  z.object({
    authMethod: z.literal("none"),
  }),
  z.object({
    authMethod: z.literal("basic"),
    basic_auth_user: z.string().min(1, "Username is required"),
    basic_auth_pass: z.string().min(1, "Password is required"),
  }),
  z.object({
    authMethod: z.literal("oauth2-cc"),
    oauth_auth_method: z.enum(["client_secret_basic", "client_secret_post"]),
    oauth_token_url: z.string().url("Invalid URL"),
    oauth_client_id: z.string().min(1, "Client ID is required"),
    oauth_client_secret: z.string().min(1, "Client Secret is required"),
    oauth_scopes: z.string().optional(),
  }),
  z.object({
    authMethod: z.literal("ntlm"),
    basic_auth_user: z.string().min(1, "Username is required"),
    basic_auth_pass: z.string().min(1, "Password is required"),
    authDomain: z.string().min(1, "Domain is required"),
    authWorkstation: z.string().min(1, "Workstation is required"),
  }),
  z.object({
    authMethod: z.literal("mtls"),
    tlsCert: z.string().min(1, "Certificate is required"),
    tlsKey: z.string().min(1, "Key is required"),
    tlsCa: z.string().optional(),
  }),
]);

export const httpSchema = z
  .object({
    type: z.literal("http"),
    url: z.string().url({ message: "Invalid URL" }),
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

export type HttpFormValues = z.infer<typeof httpSchema>;

export type AdvancedFormValues = z.infer<typeof advancedSchema>;

export const advancedDefaultValues: AdvancedFormValues = {
  accepted_statuscodes: ["2XX"],
  max_redirects: 10,
  ignore_tls_errors: false,
  check_cert_expiry: false,
}

export type HttpOptionsFormValues = z.infer<typeof httpOptionsSchema>;

export const httpOptionsDefaultValues: HttpOptionsFormValues = {
  method: "GET",
  encoding: "json",
  body: "",
  headers: "",
};

export type AuthenticationFormValues = z.infer<typeof authenticationSchema>;

export const authenticationDefaultValues: AuthenticationFormValues = {
  authMethod: "none",
};

export const httpDefaultValues: HttpFormValues = {
  type: "http",
  url: "https://example.com",
  ...monitorBaseDefaultValues,
  ...proxiesDefaultValues,
  ...advancedDefaultValues,

  httpOptions: httpOptionsDefaultValues,
  authentication: authenticationDefaultValues,
};
