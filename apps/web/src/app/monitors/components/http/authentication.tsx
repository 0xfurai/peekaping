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
  const form = useFormContext();

  return (
    <>
      <FormField
        control={form.control}
        name="authentication.basic_auth_user"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Username</FormLabel>
            <FormControl>
              <Input placeholder="Username" {...field} />
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
            <FormLabel>Password</FormLabel>
            <FormControl>
              <Input placeholder="Password" {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  );
};

const OAuth2 = () => {
  const form = useFormContext();

  return (
    <>
      <FormField
        control={form.control}
        name="authentication.oauth_auth_method"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Authentication Method</FormLabel>
            <Select onValueChange={field.onChange} value={field.value}>
              <FormControl>
                <SelectTrigger>
                  <SelectValue placeholder="Select authentication type" />
                </SelectTrigger>
              </FormControl>

              <SelectContent>
                <SelectItem
                  key="client_secret_basic"
                  value="client_secret_basic"
                >
                  Authorization Header
                </SelectItem>
                <SelectItem key="client_secret_post" value="client_secret_post">
                  Form Data Body
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
            <FormLabel>OAuth Token URL</FormLabel>
            <FormControl>
              <Input placeholder="OAuth Token URL" {...field} />
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
            <FormLabel>Client ID</FormLabel>
            <FormControl>
              <Input placeholder="Client ID" {...field} />
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
            <FormLabel>Client Secret</FormLabel>
            <FormControl>
              <Input placeholder="Client Secret" {...field} type="password" />
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
            <FormLabel>OAuth Scope</FormLabel>
            <FormControl>
              <Input
                placeholder="Optional: Space separated list of scopes"
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
  const form = useFormContext();

  return (
    <>
      <FormField
        control={form.control}
        name="authentication.authDomain"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Domain</FormLabel>
            <FormControl>
              <Input placeholder="Domain" {...field} />
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
            <FormLabel>Workstation</FormLabel>
            <FormControl>
              <Input placeholder="Workstation" {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  );
};

const MTLS = () => {
  const form = useFormContext();

  return (
    <>
      <FormField
        control={form.control}
        name="authentication.tlsCert"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Certificate</FormLabel>
            <FormControl>
              <Textarea placeholder="Certificate" {...field} />
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
            <FormLabel>Key</FormLabel>
            <FormControl>
              <Textarea placeholder="Key" {...field} />
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
  const form = useFormContext();
  const authMethod = useWatch({
    control: form.control,
    name: "authentication.authMethod",
  });

  return (
    <>
      <TypographyH4>Authentication</TypographyH4>

      <FormField
        control={form.control}
        name="authentication.authMethod"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Method</FormLabel>
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
                  <SelectValue placeholder="Select authentication type" />
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
