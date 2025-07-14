import { Input } from "@/components/ui/input";
import {
  FormField,
  FormItem,
  FormLabel,
  FormControl,
  FormMessage,
  FormDescription,
} from "@/components/ui/form";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { z } from "zod";
import { useFormContext } from "react-hook-form";

export const schema = z.object({
  type: z.literal("mattermost"),
  webhook_url: z.string().url({ message: "Valid webhook URL is required" }),
  username: z.string().optional(),
  channel: z.string().optional(),
  icon_url: z.union([z.string().url({ message: "Valid icon URL is required" }), z.literal("")]).optional(),
  icon_emoji: z.string().optional(),
  use_template: z.boolean().optional(),
  template: z.string().optional(),
});

export const defaultValues = {
  type: "mattermost" as const,
  webhook_url: "",
  username: "Peekaping",
  channel: "",
  icon_url: "",
  icon_emoji: "",
  use_template: false,
  template: "",
};

export const displayName = "Mattermost";

export default function MattermostForm() {
  const form = useFormContext();
  const useTemplate = form.watch("use_template");

  return (
    <>
      <FormField
        control={form.control}
        name="webhook_url"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              Webhook URL <span className="text-red-500">*</span>
            </FormLabel>
            <FormControl>
              <Input
                placeholder="https://your-mattermost-server.com/hooks/..."
                type="url"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              <span className="text-red-500">*</span> Required
              <br />
              <span className="mt-2 block">
                Learn more about webhooks:{" "}
                <a
                  href="https://developers.mattermost.com/integrate/webhooks/incoming/"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="underline text-blue-600"
                >
                  https://developers.mattermost.com/integrate/webhooks/incoming/
                </a>
              </span>
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="username"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Username</FormLabel>
            <FormControl>
              <Input placeholder="Peekaping" {...field} />
            </FormControl>
            <FormDescription>
              The username that will appear as the sender of the message.
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="channel"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Channel Name</FormLabel>
            <FormControl>
              <Input placeholder="general" {...field} />
            </FormControl>
            <FormDescription>
              The channel where notifications will be sent. Leave empty to use the default channel configured in the webhook.
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="icon_url"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Icon URL</FormLabel>
            <FormControl>
              <Input
                placeholder="https://example.com/icon.png"
                type="url"
                {...field}
              />
            </FormControl>
            <FormDescription>
              URL of an image to use as the icon for the notification message.
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="icon_emoji"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Icon Emoji</FormLabel>
            <FormControl>
              <Input placeholder=":white_check_mark: :x:" {...field} />
            </FormControl>
            <FormDescription>
              Emoji to use as the icon. You can specify two emojis separated by a space (first for up status, second for down status).
              <br />
              <span className="mt-2 block">
                Emoji cheat sheet:{" "}
                <a
                  href="https://www.webfx.com/tools/emoji-cheat-sheet/"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="underline text-blue-600"
                >
                  https://www.webfx.com/tools/emoji-cheat-sheet/
                </a>
              </span>
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="use_template"
        render={({ field }) => (
          <FormItem>
            <div className="flex items-center gap-2">
              <FormControl>
                <Switch
                  checked={field.value || false}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
              <FormLabel>Use Message Template</FormLabel>
            </div>
            <FormDescription>
              Enable to use a custom message template and format.
            </FormDescription>
            <FormMessage />
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
                  placeholder="Enter your custom message template"
                  className="min-h-[100px]"
                  {...field}
                />
              </FormControl>
              <FormDescription>
                Customize the message format. Available variables:{" "}
                <code>{"{{ msg }}"}</code>, <code>{"{{ monitor.name }}"}</code>, <code>{"{{ status }}"}</code>, <code>{"{{ monitor.* }}"}</code>
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      )}
    </>
  );
}
