import { z } from "zod";
import { withSavedSecret } from "../../secrets";

export const googleChatSchema = z.object({
  type: z.literal("google_chat"),
  webhook_url: withSavedSecret(z.string().url({ message: "Valid webhook URL is required" })),
});

export type GoogleChatFormValues = z.infer<typeof googleChatSchema>;

export const googleChatDefaultValues: GoogleChatFormValues = {
  type: "google_chat",
  webhook_url: "",
};
