import type { NotificationChannelModel } from "@/api/types.gen";

// The channels a new monitor starts on: the workspace's channels marked as default, in the list's
// order. A channel that is switched off is still ticked, as it is when the user adds it by hand.
export const defaultChannelIds = (channels: Pick<NotificationChannelModel, "id" | "is_default">[] | null | undefined): string[] =>
  (channels ?? []).filter((channel) => channel.is_default).map((channel) => channel.id);
