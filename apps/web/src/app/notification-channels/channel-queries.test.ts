import { describe, expect, it } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import {
  getNotificationChannelsByIdQueryKey,
  getNotificationChannelsInfiniteQueryKey,
} from "@/api/@tanstack/react-query.gen";
import { allNotificationChannelsOptions } from "@/app/monitors/monitor-types/shared/channel-support";
import { invalidateChannelLists } from "./channel-queries";

describe("invalidateChannelLists", () => {
  // The monitor form's channel picker reads every page at once: after a channel is made in the sheet
  // beside it, the picker lists it
  it("refreshes the channels page's list and the monitor form's list of every channel", async () => {
    const queryClient = new QueryClient();
    const infinite = getNotificationChannelsInfiniteQueryKey({ query: { limit: 20 } });
    const allPages = allNotificationChannelsOptions().queryKey;
    const byId = getNotificationChannelsByIdQueryKey({ path: { id: "c1" } });
    queryClient.setQueryData(infinite, { pages: [], pageParams: [] });
    queryClient.setQueryData(allPages, { message: "success", data: [] });
    queryClient.setQueryData(byId, { message: "success" });

    await invalidateChannelLists(queryClient);

    expect(queryClient.getQueryState(infinite)?.isInvalidated).toBe(true);
    expect(queryClient.getQueryState(allPages)?.isInvalidated).toBe(true);
    expect(queryClient.getQueryState(byId)?.isInvalidated).toBe(false);
  });
});
