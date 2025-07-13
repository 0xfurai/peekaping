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
  type: z.literal("google_chat"),
  webhook_url: z.string().url({ message: "Valid webhook URL is required" }),
});

export type GoogleChatFormValues = z.infer<typeof schema>;

export const defaultValues: GoogleChatFormValues = {
  type: "google_chat",
  webhook_url: "",
};

export const displayName = "Google Chat";

export default function GoogleChatForm() {
  const form = useFormContext();

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
                placeholder="https://chat.googleapis.com/v1/spaces/..."
                type="url"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              <span className="text-red-500">*</span> Required
              <br />
              <span className="mt-2 block">
                More info about Webhooks:{" "}
                <a
                  href="https://developers.google.com/chat/how-tos/webhooks"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="underline text-blue-600"
                >
                  https://developers.google.com/chat/how-tos/webhooks
                </a>
              </span>
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  );
}