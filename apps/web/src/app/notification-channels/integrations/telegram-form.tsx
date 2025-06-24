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
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import {
  Select,
  SelectTrigger,
  SelectContent,
  SelectItem,
  SelectValue,
} from "@/components/ui/select";
import { useFormContext } from "react-hook-form";
import * as React from "react";
import { Button } from "@/components/ui/button";

export const schema = z.object({
  type: z.literal("telegram"),
  bot_token: z.string().min(1, { message: "Bot token is required" }),
  chat_id: z.string().min(1, { message: "Chat ID is required" }),
  message_thread_id: z.string().optional(),
  server_url: z.string().min(1, { message: "Server URL is required" }),
  use_template: z.boolean().optional(),
  template_parse_mode: z.enum(["plain", "HTML", "MarkdownV2"]).optional(),
  template: z.string().optional(),
  send_silently: z.boolean().optional(),
  protect_content: z.boolean().optional(),
});

export type TelegramFormValues = z.infer<typeof schema>;

export const defaultValues: TelegramFormValues = {
  type: "telegram",
  bot_token: "",
  chat_id: "",
  message_thread_id: "",
  server_url: "https://api.telegram.org",
  use_template: false,
  template_parse_mode: "plain",
  template: `Peekaping Alert - {{ monitor.name }}\n\n{{ msg }}`,
  send_silently: false,
  protect_content: false,
};

export const displayName = "Telegram";

function telegramGetUpdatesURL(
  token: string,
  serverUrl: string,
  mode: "masked" | "withToken" = "masked"
) {
  const displayToken = token
    ? mode === "withToken"
      ? token
      : "*".repeat(token.length)
    : "<YOUR BOT TOKEN HERE>";
  return `${serverUrl}/bot${displayToken}/getUpdates`;
}

export default function TelegramForm() {
  const form = useFormContext();
  const botToken = form.watch("bot_token");
  const serverUrl = form.watch("server_url") || "https://api.telegram.org";
  const useTemplate = form.watch("use_template");

  // Auto-get chat ID logic
  const [loadingChatId, setLoadingChatId] = React.useState(false);

  async function autoGetTelegramChatID() {
    if (!botToken) return;
    setLoadingChatId(true);
    try {
      const url = telegramGetUpdatesURL(botToken, serverUrl, "withToken");
      const res = await fetch(url);
      const data = await res.json();
      if (data.result && data.result.length >= 1) {
        const update = data.result[data.result.length - 1];
        if (update.channel_post) {
          form.setValue("chat_id", String(update.channel_post.chat.id));
        } else if (update.message) {
          form.setValue("chat_id", String(update.message.chat.id));
        } else {
          alert("Chat ID not found in the latest update.");
        }
      } else {
        alert("No updates found. Please send a message to your bot first.");
      }
    } catch (error: unknown) {
      if (error instanceof Error) {
        alert(error.message || "Failed to fetch chat ID");
      } else {
        alert("Failed to fetch chat ID");
      }
    } finally {
      setLoadingChatId(false);
    }
  }

  return (
    <>
      <FormField
        control={form.control}
        name="bot_token"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Bot Token</FormLabel>
            <FormControl>
              <Input
                placeholder="Enter your Telegram bot token"
                type="password"
                autoComplete="new-password"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              Get your bot token from{" "}
              <a
                href="https://t.me/BotFather"
                target="_blank"
                rel="noopener noreferrer"
                className="underline"
              >
                https://t.me/BotFather
              </a>
              .
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="chat_id"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Chat ID</FormLabel>
            <div className="flex gap-2">
              <FormControl>
                <Input placeholder="Enter your chat ID" required {...field} />
              </FormControl>

              {botToken && (
                <Button
                  type="button"
                  onClick={autoGetTelegramChatID}
                  disabled={loadingChatId}
                >
                  {loadingChatId ? "Loading..." : "Auto Get"}
                </Button>
              )}
            </div>
            <FormDescription>
              You can get your chat ID by sending a message to your bot and
              clicking "Auto Get" above, or manually via the Telegram API.
              <br />
              <span className="block mt-2">To get it manually, visit:</span>
              <a
                href={telegramGetUpdatesURL(botToken, serverUrl, "withToken")}
                target="_blank"
                rel="noopener noreferrer"
                className="block underline break-all"
              >
                {telegramGetUpdatesURL(
                  botToken,
                  serverUrl,
                  botToken ? "masked" : "withToken"
                )}
              </a>
              <span className="block mt-2">
                See{" "}
                <a
                  href="https://core.org/bots/api#getting-updates"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="underline"
                >
                  Telegram docs
                </a>{" "}
                for more info.
              </span>
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="message_thread_id"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Message Thread ID</FormLabel>
            <FormControl>
              <Input placeholder="Optional: For topics in groups" {...field} />
            </FormControl>
            <FormDescription>
              Optional. Used for sending messages to a specific thread (topic)
              in a forum supergroup.
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="server_url"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Telegram Server URL</FormLabel>
            <FormControl>
              <Input placeholder="https://api.telegram.org" {...field} />
            </FormControl>
            <FormDescription>
              The default is{" "}
              <a
                href="https://api.telegram.org"
                target="_blank"
                rel="noopener noreferrer"
                className="underline"
              >
                https://api.telegram.org
              </a>
              . You can use a custom server. See{" "}
              <a
                href="https://core.org/bots/api#using-a-local-bot-api-server"
                target="_blank"
                rel="noopener noreferrer"
                className="underline"
              >
                here
              </a>{" "}
              for details.
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
        <>
          <FormField
            control={form.control}
            name="template_parse_mode"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Template Parse Mode</FormLabel>
                <Select onValueChange={field.onChange} value={field.value}>
                  <FormControl>
                    <SelectTrigger>
                      <SelectValue placeholder="Select parse mode" />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent>
                    <SelectItem value="plain">Plain Text</SelectItem>
                    <SelectItem value="HTML">HTML</SelectItem>
                    <SelectItem value="MarkdownV2">MarkdownV2</SelectItem>
                  </SelectContent>
                </Select>
                <FormDescription>
                  Choose how the message should be parsed. See{" "}
                  <a
                    href="https://core.org/bots/api#formatting-options"
                    target="_blank"
                    rel="noopener noreferrer"
                    className="underline"
                  >
                    Telegram formatting docs
                  </a>{" "}
                  for details.
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

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
                  <code>{"{{ msg }}"}</code>, <code>{"{{ monitorJSON }}"}</code>
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </>
      )}

      <FormField
        control={form.control}
        name="send_silently"
        render={({ field }) => (
          <FormItem>
            <div className="flex items-center gap-2">
              <FormControl>
                <Switch
                  checked={field.value || false}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
              <FormLabel>Send Silently</FormLabel>
            </div>
            <FormDescription>
              If enabled, messages will be sent silently (users will receive a
              notification with no sound).
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="protect_content"
        render={({ field }) => (
          <FormItem>
            <div className="flex items-center gap-2">
              <FormControl>
                <Switch
                  checked={field.value || false}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
              <FormLabel>Protect Content</FormLabel>
            </div>
            <FormDescription>
              If enabled, the message content will be protected from forwarding
              and saving.
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  );
}
