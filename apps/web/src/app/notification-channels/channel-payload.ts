import type { NotificationChannelCreateUpdateDto } from "@/api";
import type { NotificationChannelFormValues } from "./channel-types/registry";

// The request body that creates a channel from the form. `isDefault` marks it as the default for
// new monitors.
export const channelCreateBody = (
  data: NotificationChannelFormValues,
  isDefault = false
): NotificationChannelCreateUpdateDto => ({
  name: data.name,
  type: data.type,
  config: JSON.stringify(data),
  active: true,
  is_default: isDefault,
});
