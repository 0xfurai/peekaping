import { Input } from "@/components/ui/input";
import {
  FormField,
  FormItem,
  FormLabel,
  FormControl,
  FormMessage,
  FormDescription,
} from "@/components/ui/form";
import { z } from "zod";
import { useFormContext } from "react-hook-form";

export const schema = z.object({
  type: z.literal("sendgrid"),
  api_key: z.string().min(1, { message: "API key is required" }),
  from_email: z.string().email({ message: "Valid sender email is required" }),
  to_email: z.string().min(1, { message: "Recipient email(s) required" }),
  cc_email: z.string().optional(),
  bcc_email: z.string().optional(),
  subject: z.string().optional(),
});

export type SendGridFormValues = z.infer<typeof schema>;

export const defaultValues: SendGridFormValues = {
  type: "sendgrid",
  api_key: "",
  from_email: "noreply@example.com",
  to_email: "recipient@example.com",
  cc_email: "",
  bcc_email: "",
  subject: "{{ name }} - {{ status }}",
};

export const displayName = "SendGrid";

export default function SendGridForm() {
  const form = useFormContext();

  return (
    <>
      <FormField
        control={form.control}
        name="api_key"
        render={({ field }) => (
          <FormItem>
            <FormLabel>API Key</FormLabel>
            <FormControl>
              <Input
                placeholder="SG.xxxxxxxxxxxxxxxxxxxx"
                type="password"
                {...field}
              />
            </FormControl>
            <FormDescription>
              Your SendGrid API key. You can create one in your SendGrid account
              under Settings → API Keys. Make sure it has "Mail Send" permissions.
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name="from_email"
        render={({ field }) => (
          <FormItem>
            <FormLabel>From Email</FormLabel>
            <FormControl>
              <Input placeholder="noreply@example.com" {...field} />
            </FormControl>
            <FormDescription>
              The sender email address. This must be a verified sender in your
              SendGrid account.
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name="to_email"
        render={({ field }) => (
          <FormItem>
            <FormLabel>To Email</FormLabel>
            <FormControl>
              <Input placeholder="recipient@example.com" {...field} />
            </FormControl>
            <FormDescription>
              Primary recipient email address. For multiple recipients, use CC or
              BCC fields.
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name="cc_email"
        render={({ field }) => (
          <FormItem>
            <FormLabel>CC Email (Optional)</FormLabel>
            <FormControl>
              <Input placeholder="cc1@example.com, cc2@example.com" {...field} />
            </FormControl>
            <FormDescription>
              Carbon copy recipients. Separate multiple emails with commas.
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name="bcc_email"
        render={({ field }) => (
          <FormItem>
            <FormLabel>BCC Email (Optional)</FormLabel>
            <FormControl>
              <Input placeholder="bcc1@example.com, bcc2@example.com" {...field} />
            </FormControl>
            <FormDescription>
              Blind carbon copy recipients. Separate multiple emails with commas.
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name="subject"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Custom Subject (Optional)</FormLabel>
            <FormControl>
              <Input placeholder="{{ name }} - {{ status }}" {...field} />
            </FormControl>
            <FormDescription>
              Subject line for the email. Supports Liquid templating.
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
                status (UP/DOWN)
              </span>
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  );
}