import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { TypographyH4 } from "@/components/ui/typography";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useFormContext, useWatch } from "react-hook-form";
import { Textarea } from "@/components/ui/textarea";
import { z } from "zod";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

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
    tlsCa: z.string().min(1, "CA is required"),
  }),
]);

export type AuthenticationForm = z.infer<typeof authenticationSchema>;

export const authenticationDefaultValues: AuthenticationForm = {
  authMethod: "none",
};

const BasicAuth = () => {
  const { t } = useLocalizedTranslation();
  const form = useFormContext();

  return (
    <>
      <FormField
        control={form.control}
        name="authentication.basic_auth_user"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("forms.labels.username")}</FormLabel>
            <FormControl>
              <Input placeholder={t("forms.labels.username")} {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name="authentication.basic_auth_pass"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("forms.labels.password")}</FormLabel>
            <FormControl>
              <Input placeholder={t("forms.labels.password")} {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  );
};

const OAuth2 = () => {
  const { t } = useLocalizedTranslation();
  const form = useFormContext();

  return (
    <>
      <FormField
        control={form.control}
        name="authentication.oauth_auth_method"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("monitors.form.http.authentication.method_label")}</FormLabel>
            <Select onValueChange={field.onChange} value={field.value}>
              <FormControl>
                <SelectTrigger>
                  <SelectValue placeholder={t("monitors.form.http.authentication.method_placeholder")} />
                </SelectTrigger>
              </FormControl>

              <SelectContent>
                <SelectItem
                  key="client_secret_basic"
                  value="client_secret_basic"
                >
                  {t("monitors.form.http.authentication.auth_header")}
                </SelectItem>
                <SelectItem key="client_secret_post" value="client_secret_post">
                  {t("monitors.form.http.authentication.form_data_body")}
                </SelectItem>
              </SelectContent>
            </Select>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="authentication.oauth_token_url"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("monitors.form.http.authentication.oauth_token_url")}</FormLabel>
            <FormControl>
              <Input placeholder={t("monitors.form.http.authentication.oauth_token_url_placeholder")} {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="authentication.oauth_client_id"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("monitors.form.http.authentication.client_id")}</FormLabel>
            <FormControl>
              <Input placeholder={t("monitors.form.http.authentication.client_id_placeholder")} {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="authentication.oauth_client_secret"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("monitors.form.http.authentication.client_secret")}</FormLabel>
            <FormControl>
              <Input placeholder={t("monitors.form.http.authentication.client_secret_placeholder")} {...field} type="password" />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="authentication.oauth_scopes"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("monitors.form.http.authentication.oauth_scope")}</FormLabel>
            <FormControl>
              <Input
                placeholder={t("monitors.form.http.authentication.oauth_scope_placeholder")}
                {...field}
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  );
};

const NTLM = () => {
  const { t } = useLocalizedTranslation();
  const form = useFormContext();

  return (
    <>
      <FormField
        control={form.control}
        name="authentication.authDomain"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("monitors.form.http.authentication.domain")}</FormLabel>
            <FormControl>
              <Input placeholder={t("monitors.form.http.authentication.domain_placeholder")} {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="authentication.authWorkstation"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("monitors.form.http.authentication.workstation")}</FormLabel>
            <FormControl>
              <Input placeholder={t("monitors.form.http.authentication.workstation_placeholder")} {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  );
};

const MTLS = () => {
  const { t } = useLocalizedTranslation();
  const form = useFormContext();

  return (
    <>
      <FormField
        control={form.control}
        name="authentication.tlsCert"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("monitors.form.http.authentication.certificate")}</FormLabel>
            <FormControl>
              <Textarea placeholder={t("monitors.form.http.authentication.certificate_placeholder")} {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="authentication.tlsKey"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("monitors.form.http.authentication.key")}</FormLabel>
            <FormControl>
              <Textarea placeholder={t("monitors.form.http.authentication.key_placeholder")} {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="authentication.tlsCa"
        render={({ field }) => (
          <FormItem>
            <FormLabel>CA</FormLabel>
            <FormControl>
              <Textarea placeholder="CA" {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  );
};

const authenticationTypes = [
  { label: "None", value: "none" },
  { label: "HTTP Basic Auth", value: "basic" },
  { label: "OAuth2: Client Credentials", value: "oauth2-cc" },
  { label: "NTLM", value: "ntlm" },
  { label: "mTLS", value: "mtls" },
];

const Authentication = () => {
  const { t } = useLocalizedTranslation();
  const form = useFormContext();
  const authMethod = useWatch({
    control: form.control,
    name: "authentication.authMethod",
  });

  return (
    <>
      <TypographyH4>{t("monitors.form.http.authentication.title")}</TypographyH4>

      <FormField
        control={form.control}
        name="authentication.authMethod"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("monitors.form.http.authentication.method_label")}</FormLabel>
            <Select
              onValueChange={(v) => {
                if (!v) {
                  return;
                }
                field.onChange(v);
              }}
              value={field.value}
            >
              <FormControl>
                <SelectTrigger>
                  <SelectValue placeholder={t("monitors.form.http.authentication.method_placeholder")} />
                </SelectTrigger>
              </FormControl>

              <SelectContent>
                {authenticationTypes.map((item) => (
                  <SelectItem key={item.value} value={item.value}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FormMessage />
          </FormItem>
        )}
      />

      {authMethod === "basic" && <BasicAuth />}
      {authMethod === "oauth2-cc" && <OAuth2 />}
      {authMethod === "ntlm" && (
        <>
          <BasicAuth />
          <NTLM />
        </>
      )}
      {authMethod === "mtls" && <MTLS />}
    </>
  );
};

export default Authentication;
