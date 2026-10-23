import { z } from "zod";
import { withSavedSecret } from "../../secrets";

export const pushoverSchema = z.object({
  type: z.literal("pushover"),
  pushover_user_key: withSavedSecret(z.string().min(1, { message: "User key is required" })),
  pushover_app_token: withSavedSecret(z.string().min(1, { message: "Application token is required" })),
  pushover_device: z.string().optional(),
  pushover_title: z.string().optional(),
  pushover_priority: z.coerce.number().min(-2).max(2).optional(),
  pushover_sounds: z.string().optional(),
  pushover_sounds_up: z.string().optional(),
  pushover_ttl: z.coerce.number().min(0).optional(),
});

export type PushoverFormValues = z.infer<typeof pushoverSchema>;

export const pushoverDefaultValues: PushoverFormValues = {
  type: "pushover",
  pushover_user_key: "",
  pushover_app_token: "",
  pushover_device: "",
  pushover_title: "",
  pushover_priority: 0,
  pushover_sounds: "pushover",
  pushover_sounds_up: "pushover",
  pushover_ttl: 0,
};
