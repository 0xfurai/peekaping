import { z } from "zod";
import { withSavedSecret } from "../../secrets";

export const gotifySchema = z.object({
  type: z.literal("gotify"),
  server_url: z.string().url({ message: "Valid server URL is required" }),
  application_token: withSavedSecret(z.string().min(1, { message: "Application token is required" })),
  priority: z.coerce.number().min(0).max(10).optional(),
  title: z.string().optional(),
  custom_message: z.string().optional(),
});

export type GotifyFormValues = z.infer<typeof gotifySchema>;

export const gotifyDefaultValues: GotifyFormValues = {
  type: "gotify" as const,
  server_url: "",
  application_token: "",
  priority: 8,
  title: "",
  custom_message: "",
};
