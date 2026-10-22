import { z } from "zod";
import { withSavedSecret } from "../../secrets";

export const pushbulletSchema = z.object({
  type: z.literal("pushbullet"),
  pushbullet_access_token: withSavedSecret(
    z.string().min(1, {
      message: "Access Token is required",
    })
  ),
  pushbullet_device_id: z.string().optional(),
  pushbullet_channel_tag: z.string().optional(),
  pushbullet_custom_template: z.string().optional(),
});

export type PushbulletFormValues = z.infer<typeof pushbulletSchema>;

export const pushbulletDefaultValues: PushbulletFormValues = {
  type: "pushbullet",
  pushbullet_access_token: "",
  pushbullet_device_id: "",
  pushbullet_channel_tag: "",
  pushbullet_custom_template: "{{ msg }}\n\nMonitor: {{ name }}\nStatus: {{ status }}\nTime: {{ heartbeat.time }}",
};
