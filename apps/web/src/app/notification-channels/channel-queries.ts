import type { QueryClient } from "@tanstack/react-query";
import { getNotificationChannelsQueryKey } from "@/api/@tanstack/react-query.gen";

// Refreshes every list of the workspace's channels, after a channel is created, changed or deleted.
// The plain list key is a prefix of the channels page's infinite list and of the monitor form's
// list of every channel (allNotificationChannelsOptions); the infinite key matches only the first.
export const invalidateChannelLists = (queryClient: QueryClient) =>
  queryClient.invalidateQueries({ queryKey: getNotificationChannelsQueryKey() });
