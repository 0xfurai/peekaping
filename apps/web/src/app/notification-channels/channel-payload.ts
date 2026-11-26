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

// Whether the new channel page makes a default channel: when its address has `default=true`, as the
// monitors empty state's link does. A new monitor ticks only the default channels, so a channel made
// before the first monitor alerts for it without being ticked by hand.
export const createsDefaultChannel = (searchParams: URLSearchParams): boolean => searchParams.get("default") === "true";
