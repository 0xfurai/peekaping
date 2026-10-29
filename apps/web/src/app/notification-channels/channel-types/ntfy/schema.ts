import { z } from "zod";
import { withSavedSecret } from "../../secrets";

export const ntfySchema = z.object({
  type: z.literal("ntfy"),
  server_url: withSavedSecret(z.string().url({ message: "Valid NTFY server URL is required" })),
  topic: z.string().min(1, { message: "Topic is required" }),
  authentication_type: z.enum(["none", "basic", "token"]),
  username: z.string().optional(),
  password: withSavedSecret(z.string()).optional(),
  token: withSavedSecret(z.string()).optional(),
  priority: z.coerce.number().min(1).max(5),
  tags: z.string().optional(),
  title: z.string().optional(),
  custom_message: z.string().optional(),
});

export type NtfyFormValues = z.infer<typeof ntfySchema>;

export const ntfyDefaultValues: NtfyFormValues = {
  type: "ntfy",
  server_url: "https://ntfy.sh",
  topic: "peekaping",
  authentication_type: "none",
  username: "",
  password: "",
  token: "",
  priority: 3,
  tags: "peekaping,monitoring",
  title: "Peekaping Alert - {{ name }}",
  custom_message: "{{ msg }}",
};
