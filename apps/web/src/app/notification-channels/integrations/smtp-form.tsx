import { Input } from "@/components/ui/input";
import { PasswordInput } from "@/components/ui/password-input";
import {
  FormField,
  FormItem,
  FormLabel,
  FormControl,
  FormMessage,
  FormDescription,
} from "@/components/ui/form";
import { z } from "zod";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { useFormContext } from "react-hook-form";

export const schema = z.object({
  type: z.literal("smtp"),
  smtp_secure: z.boolean(),
  smtp_host: z.string(),
  smtp_port: z.coerce.number().min(1, { message: "Port is required" }),
  username: z.string().min(1, { message: "Username is required" }),
  password: z.string().min(1, { message: "Password is required" }),
  from: z.string().email({ message: "Sender email is required" }),
  to: z.string().min(1, { message: "Recipient(s) required" }),
  cc: z.string().optional(),
  bcc: z.string().optional(),
  custom_subject: z.string().optional(),
  custom_body: z.string().optional(),
});

export type SmtpFormValues = z.infer<typeof schema>;

export const defaultValues: SmtpFormValues = {
  type: "smtp",
  smtp_secure: false,
  smtp_host: "example.com",
  smtp_port: 587,
  username: "username",
  password: "password",
  from: "sender@example.com",
  to: "recipient@example.com",
  cc: "cc@example.com",
  bcc: "bcc@example.com",
  custom_subject: "{{ msg }}",
  custom_body: "{{ msg }}",
};

export const displayName = "Email (SMTP)";

export default function SmtpForm() {
  const form = useFormContext();

  return (
    <>
      <FormField
        control={form.control}
        name="smtp_host"
        render={({ field }) => (
          <FormItem>
            <FormLabel>SMTP Host</FormLabel>
            <FormControl>
              <Input placeholder="example.com" {...field} />
            </FormControl>
            <FormDescription>
              Either enter the hostname of the server you want to connect to or
              localhost if you intend to use a locally configured mail transfer
              agent
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
      <div className="flex space-x-4">
        <FormField
          control={form.control}
          name="smtp_port"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Port</FormLabel>
              <FormControl>
                <Input placeholder="587" type="number" {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name="smtp_secure"
          render={({ field }) => (
            <FormItem>
              <FormLabel>SSL/TLS</FormLabel>
              <FormControl>
                <Switch
                  checked={field.value || false}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
      </div>
      <FormField
        control={form.control}
        name="username"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Username</FormLabel>
            <FormControl>
              <Input placeholder="SMTP username" {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name="password"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Password</FormLabel>
            <FormControl>
              <PasswordInput placeholder="SMTP password" {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>

        )}
      />
      <FormField
        control={form.control}
        name="from"
        render={({ field }) => (
          <FormItem>
            <FormLabel>From (Sender Email)</FormLabel>
            <FormControl>
              <Input placeholder="sender@example.com" {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name="to"
        render={({ field }) => (
          <FormItem>
            <FormLabel>To (Recipient Email(s))</FormLabel>
            <FormControl>
              <Input placeholder="recipient@example.com, ..." {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name="cc"
        render={({ field }) => (
          <FormItem>
            <FormLabel>CC</FormLabel>
            <FormControl>
              <Input placeholder="cc@example.com, ..." {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name="bcc"
        render={({ field }) => (
          <FormItem>
            <FormLabel>BCC</FormLabel>
            <FormControl>
              <Input placeholder="bcc@example.com, ..." {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name="custom_subject"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Custom Subject</FormLabel>

            <FormDescription>
              Templatability is achieved via the <b>Liquid</b> templating
              language.
              <br />
              Please refer to the documentation for usage instructions.
              <br />
              <b>Available variables:</b>
              <span className="block">
                <code className="text-pink-500">{"{{ msg }}"}</code>: message of
                the notification
              </span>
              <span className="block">
                <code className="text-pink-500">{"{{ name }}"}</code>: service
                name
              </span>
              <span className="block">
                <code className="text-pink-500">{"{{ status }}"}</code>: service
                status (UP/DOWN/Certificate expiry notifications)
              </span>
            </FormDescription>
            <FormControl>
              <Input placeholder="{{ msg }}" {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name="custom_body"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Custom Body</FormLabel>

            <FormDescription>
              Templatability is achieved via the <b>Liquid</b> templating
              language.
              <br />
              Please refer to the documentation for usage instructions.
              <br />
              <b>Available variables:</b>
              <span className="block">
                <code className="text-pink-500">{"{{ msg }}"}</code>: message of
                the notification
              </span>
              <span className="block">
                <code className="text-pink-500">{"{{ name }}"}</code>: service
                name
              </span>
              <span className="block">
                <code className="text-pink-500">{"{{ status }}"}</code>: service
                status (UP/DOWN/Certificate expiry notifications)
              </span>
            </FormDescription>
            <FormControl>
              <Textarea placeholder="{{ msg }}" {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  );
}
