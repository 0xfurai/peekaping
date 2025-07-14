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
import {
  Select,
  SelectTrigger,
  SelectContent,
  SelectItem,
  SelectValue,
} from "@/components/ui/select";
import { useFormContext } from "react-hook-form";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { InfoIcon } from "lucide-react";

export const schema = z.object({
  type: z.literal("discord"),
  webhook_url: z.string().url({ message: "Valid webhook URL is required" }),
  bot_display_name: z.string().min(1, { message: "Bot display name is required" }),
  custom_message_prefix: z.string().optional(),
  message_type: z.enum(["send_to_channel", "send_to_new_forum_post", "send_to_thread"], { message: "Message type is required" }),
  thread_name: z.string().optional(),
  thread_id: z.string().optional(),
});

export type DiscordFormValues = z.infer<typeof schema>;

export const defaultValues: DiscordFormValues = {
  type: "discord",
  webhook_url: "",
  bot_display_name: "Peekaping",
  custom_message_prefix: "",
  message_type: "send_to_channel",
  thread_name: "",
  thread_id: "",
};

export const displayName = "Discord";

export default function DiscordForm() {
  const form = useFormContext();
  const messageType = form.watch("message_type");

  return (
    <>
      <FormField
        control={form.control}
        name="webhook_url"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              Discord Webhook URL
            </FormLabel>
            <FormControl>
              <Input
                placeholder="https://discord.com/api/webhooks/..."
                type="url"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
                <Alert>
                  <InfoIcon className="mr-2 h-4 w-4"/>
                  <AlertTitle className="font-bold">Setup Discord Webhook</AlertTitle>
                  <AlertDescription>
                    <ul className="list-inside list-disc text-sm mt-2">
                      <li>Get your webhook URL from Discord server settings → Integrations → Webhooks.</li>
                      <li>Create a new webhook and copy the URL. Make sure you selected the correct channel.</li>
                    </ul>
                  </AlertDescription>
                </Alert>
              </FormDescription>
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name="bot_display_name"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Bot Display Name</FormLabel>
            <FormControl>
              <Input
                placeholder="Peekaping"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              The name that will appear as the sender of the Discord messages.
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="custom_message_prefix"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Custom Message Prefix</FormLabel>
            <FormControl>
              <Input
                placeholder="Optional: Add a prefix to your messages"
                {...field}
              />
            </FormControl>
            <FormDescription>
              Optional prefix that will be added to the beginning of all messages.
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="message_type"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Sending Message To</FormLabel>
            <Select onValueChange={field.onChange} value={field.value}>
              <FormControl>
                <SelectTrigger>
                  <SelectValue placeholder="Select message type" />
                </SelectTrigger>
              </FormControl>
              <SelectContent>
                <SelectItem value="send_to_channel">Send to channel</SelectItem>
                <SelectItem value="send_to_new_forum_post">Send to new forum post</SelectItem>
                <SelectItem value="send_to_thread">Send to thread</SelectItem>
              </SelectContent>
            </Select>
            <FormDescription>
              Choose where to send Discord messages.
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      {messageType === "send_to_new_forum_post" && (
        <FormField
          control={form.control}
          name="thread_name"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Forum Post Name</FormLabel>
              <FormControl>
                <Input
                  placeholder="Enter forum post name"
                  required
                  {...field}
                />
              </FormControl>
              <FormDescription>
                The name of the new forum post that will be created.
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      )}

      {messageType === "send_to_thread" && (
        <FormField
          control={form.control}
          name="thread_id"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Thread ID</FormLabel>
              <FormControl>
                <Input
                  placeholder="Enter thread or post ID"
                  required
                  {...field}
                />
              </FormControl>
              <FormDescription>
                <Alert>
                  <InfoIcon className="mr-2 h-4 w-4"/>
                  <AlertTitle className="font-bold">The ID of the thread or forum post where messages will be sent</AlertTitle>
                  <AlertDescription>
                    <ul className="list-inside list-disc text-sm mt-2">
                      <li>Enable Developer Mode in Discord User Settings → Advanced → Developer Mode</li>
                      <li>Right-click on the thread or post and select "Copy Thread ID" or "Copy Channel ID"</li>
                    </ul>
                  </AlertDescription>
                </Alert>
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      )}
    </>
  );
}
