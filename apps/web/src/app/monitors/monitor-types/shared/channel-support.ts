import { queryOptions } from "@tanstack/react-query";
import { getNotificationChannels } from "@/api/sdk.gen";
import { getNotificationChannelsQueryKey } from "@/api/@tanstack/react-query.gen";
import type { NotificationChannelModel, UtilsApiResponseArrayNotificationChannelModel } from "@/api/types.gen";

// The channels a new monitor starts on: the workspace's channels marked as default, in the list's
// order. A channel that is switched off is still ticked, as it is when the user adds it by hand.
export const defaultChannelIds = (channels: Pick<NotificationChannelModel, "id" | "is_default">[] | null | undefined): string[] =>
  (channels ?? []).filter((channel) => channel.is_default).map((channel) => channel.id);

// The channels the monitor form's picker offers to add: the loaded ones that are not ticked yet
export const availableChannels = (
  channels: NotificationChannelModel[] | null | undefined,
  tickedIds: string[]
): NotificationChannelModel[] => (channels ?? []).filter((channel) => !tickedIds.includes(channel.id));

// The largest page the channel list answers (the server's utils.MaxPageSize)
const CHANNEL_PAGE_SIZE = 1000;

// Every channel of the workspace, page after page while a page comes back full. The list answers
// 10 a page unless asked for more, so a default channel past the 10th would be missed otherwise.
export const readAllChannels = async (
  fetchPage: (page: number, limit: number) => Promise<NotificationChannelModel[]>
): Promise<NotificationChannelModel[]> => {
  const all: NotificationChannelModel[] = [];
  for (let page = 0; ; page++) {
    const channels = await fetchPage(page, CHANNEL_PAGE_SIZE);
    all.push(...channels);
    if (channels.length < CHANNEL_PAGE_SIZE) return all;
  }
};

// The query of every channel of the workspace, which the monitor form ticks its default channels
// from and its channel field lists. Its key starts as the channel list's does, so invalidating the
// list refreshes it too.
export const allNotificationChannelsOptions = () =>
  queryOptions({
    queryKey: [{ ...getNotificationChannelsQueryKey()[0], allPages: true }],
    queryFn: async ({ signal }): Promise<UtilsApiResponseArrayNotificationChannelModel> => ({
      message: "success",
      data: await readAllChannels(
        async (page, limit) =>
          (await getNotificationChannels({ query: { page, limit }, signal, throwOnError: true })).data.data ?? []
      ),
    }),
  });
