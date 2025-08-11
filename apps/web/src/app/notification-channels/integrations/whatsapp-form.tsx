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
  type: z.literal("whatsapp"),
  server_url: z.string().url({ message: "Server URL must be a valid URL" }),
  api_key: z.string().optional(),
  phone_number: z.string().min(1, { message: "Phone number is required" }),
  session: z.string().min(1, { message: "Session is required" }),
  use_template: z.boolean().optional(),
  template: z.string().optional(),
  custom_message: z.string().optional(),
});

export type WhatsAppFormValues = z.infer<typeof schema>;

export const defaultValues: WhatsAppFormValues = {
  type: "whatsapp",
  server_url: "http://localhost:3000",
  api_key: "",
  phone_number: "",
  session: "",
  use_template: false,
  template: `🚨 Peekaping Alert

Monitor: {{ monitor.name }}
Status: {{ status }}
Message: {{ msg }}

Time: {{ heartbeat.created_at }}`,
  custom_message: "",
};

export const displayName = "WhatsApp (WAHA)";

export default function WhatsAppForm() {
  const form = useFormContext();
  const useTemplate = form.watch("use_template");

  return (
    <>
      <div className="space-y-6">
        <FormField
          control={form.control}
          name="server_url"
          render={({ field }) => (
            <FormItem>
              <FormLabel>API URL</FormLabel>
              <FormControl>
                <Input placeholder="http://localhost:3000" {...field} />
              </FormControl>
              <FormDescription>
                The URL of your WAHA server instance
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name="api_key"
          render={({ field }) => (
            <FormItem>
              <FormLabel>API Key</FormLabel>
              <FormControl>
                <PasswordInput
                  placeholder="Your WAHA API key"
                  {...field}
                />
              </FormControl>

              <FormDescription>
                The API key for your WAHA server instance
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name="session"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Session</FormLabel>
              <FormControl>
                <Input placeholder="default" {...field} />
              </FormControl>
              <FormDescription>
                From this session WAHA sends notifications to Chat ID. You can
                find it in WAHA Dashboard.
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name="phone_number"
          render={({ field }) => (
            <FormItem>
              <FormLabel>
                Chat ID (Phone Number / Contact ID / Group ID)
              </FormLabel>
              <FormControl>
                <Input placeholder="1234567890" {...field} />
              </FormControl>
              <FormDescription>
                Enter phone number to receive WhatsApp notifications. You can
                use:
                <br />• Phone number with country code (e.g., 1234567890)
                <br />• Contact ID format (e.g., 1234567890@c.us)
                <br />• Group ID format (e.g., 123456789012345678@g.us)
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={form.control}
          name="use_template"
          render={({ field }) => (
            <FormItem className="flex flex-row items-center justify-between rounded-lg border p-4">
              <div className="space-y-0.5">
                <FormLabel className="text-base">Use Custom Template</FormLabel>
                <FormDescription>
                  Enable to use a custom message template with variables
                </FormDescription>
              </div>
              <FormControl>
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
            </FormItem>
          )}
        />

        {useTemplate && (
          <FormField
            control={form.control}
            name="template"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Message Template</FormLabel>
                <FormControl>
                  <Textarea
                    placeholder="Enter your custom message template..."
                    className="min-h-[120px]"
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  Use Liquid template syntax. Available variables:{" "}
                  {"{{ monitor.name }}"}, {"{{ status }}"}, {"{{ msg }}"},{" "}
                  {"{{ heartbeat.created_at }}"}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        )}

        {!useTemplate && (
          <FormField
            control={form.control}
            name="custom_message"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Custom Message</FormLabel>
                <FormControl>
                  <Textarea
                    placeholder="Enter your custom message..."
                    className="min-h-[80px]"
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  Leave empty to use the default message. Use Liquid template
                  syntax for variables.
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        )}
      </div>
      <div className="space-y-4 p-4 bg-amber-50 dark:bg-amber-900/20 rounded-lg border border-amber-200 dark:border-amber-800">
        <p className="text-sm text-amber-800 dark:text-amber-200">
          <strong>Note:</strong> You need to have a WAHA server.
        </p>
        <p className="text-sm text-amber-800 dark:text-amber-200">
          You can check this URL to view how to set one up:
        </p>
        <p className="text-sm text-amber-800 dark:text-amber-200">
          <a
            href="https://github.com/devlikeapro/waha"
            target="_blank"
            rel="noopener noreferrer"
            className="underline hover:text-amber-900 dark:hover:text-amber-100"
          >
            https://github.com/devlikeapro/waha
          </a>
        </p>
      </div>
    </>
  );
}
