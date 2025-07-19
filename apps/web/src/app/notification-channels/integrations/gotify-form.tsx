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
import { Textarea } from "@/components/ui/textarea";
import {
  Select,
  SelectTrigger,
  SelectContent,
  SelectItem,
  SelectValue,
} from "@/components/ui/select";
import { useFormContext } from "react-hook-form";

export const schema = z.object({
  type: z.literal("gotify"),
  server_url: z.string().url({ message: "Valid server URL is required" }),
  application_token: z.string().min(1, { message: "Application token is required" }),
  priority: z.coerce.number().min(0).max(10).optional(),
  title: z.string().optional(),
  custom_message: z.string().optional(),
});

export const defaultValues = {
  type: "gotify" as const,
  server_url: "",
  application_token: "",
  priority: 8,
  title: "",
  custom_message: "",
};

export const displayName = "Gotify";

export default function GotifyForm() {
  const form = useFormContext();

  return (
    <>
      <FormField
        control={form.control}
        name="server_url"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Server URL</FormLabel>
            <FormControl>
              <Input
                placeholder="https://gotify.yourdomain.com"
                type="url"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              The URL of your Gotify server. Make sure it's accessible from this application.
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="application_token"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Application Token</FormLabel>
            <FormControl>
              <Input
                placeholder="ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
                type="password"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              The application token from your Gotify application. You can find this in your Gotify web interface under Apps.
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="priority"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Priority</FormLabel>
            <Select
              onValueChange={(val) => {
                if (!val) {
                  return;
                }
                field.onChange(parseInt(val));
              }}
              value={field.value?.toString()}
            >
              <FormControl>
                <SelectTrigger>
                  <SelectValue placeholder="Select priority" />
                </SelectTrigger>
              </FormControl>
              <SelectContent>
                <SelectItem value="0">0 - Lowest</SelectItem>
                <SelectItem value="1">1 - Very Low</SelectItem>
                <SelectItem value="2">2 - Low</SelectItem>
                <SelectItem value="3">3 - Below Normal</SelectItem>
                <SelectItem value="4">4 - Normal</SelectItem>
                <SelectItem value="5">5 - Above Normal</SelectItem>
                <SelectItem value="6">6 - Moderate</SelectItem>
                <SelectItem value="7">7 - High</SelectItem>
                <SelectItem value="8">8 - Very High (Default)</SelectItem>
                <SelectItem value="9">9 - Emergency</SelectItem>
                <SelectItem value="10">10 - Highest</SelectItem>
              </SelectContent>
            </Select>
            <FormDescription>
              Message priority (0-10). Higher numbers represent higher priority. Default is 8.
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="title"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Custom Title</FormLabel>
            <FormControl>
              <Input
                placeholder="Peekaping Alert - {{ monitor.name }}"
                {...field}
              />
            </FormControl>
            <FormDescription>
              Custom title for the notification. Available variables: {"{{ msg }}"}, {"{{ monitor.name }}"}, {"{{ status }}"}, {"{{ heartbeat.* }}"}.
              Leave empty to use the default "Peekaping" title.
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="custom_message"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Custom Message</FormLabel>
            <FormControl>
              <Textarea
                placeholder="Alert: {{ monitor.name }} is {{ status }} - {{ msg }}"
                className="min-h-[100px]"
                {...field}
              />
            </FormControl>
            <FormDescription>
              Custom message template. Available variables: {"{{ msg }}"}, {"{{ monitor.name }}"}, {"{{ status }}"}, {"{{ heartbeat.* }}"}.
              Leave empty to use the default message.
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <div className="space-y-4 p-4 bg-blue-50 dark:bg-blue-900/20 rounded-lg border border-blue-200 dark:border-blue-800">
        <p className="text-sm text-blue-800 dark:text-blue-200">
          <strong>Note:</strong> You need to have a Gotify server running to use this integration.
        </p>
        <p className="text-sm text-blue-800 dark:text-blue-200">
          You can learn more about Gotify and how to set it up at:
        </p>
        <p className="text-sm text-blue-800 dark:text-blue-200">
          <a
            href="https://gotify.net/"
            target="_blank"
            rel="noopener noreferrer"
            className="underline hover:text-blue-900 dark:hover:text-blue-100"
          >
            https://gotify.net/
          </a>
        </p>
      </div>
    </>
  );
}
