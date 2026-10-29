import { describe, expect, it } from "vitest";
import type { NotificationChannelModel } from "@/api/types.gen";
import { defaultChannelIds, readAllChannels } from "./channel-support";

const channel = (n: number, isDefault: boolean): NotificationChannelModel => ({
  id: `c${n}`,
  name: `c${n}`,
  type: "webhook",
  config: null,
  active: true,
  is_default: isDefault,
  created_at: "2026-10-28T00:00:00Z",
  updated_at: "2026-10-28T00:00:00Z",
});

// The channel list as GET /notification-channels pages it: 10 a page unless the request says
// otherwise, and at most 1000
const server = (channels: NotificationChannelModel[]) => {
  const asked: { page: number; limit: number | undefined }[] = [];
  const fetchPage = async (page: number, limit?: number) => {
    asked.push({ page, limit });
    const size = limit ?? 10;
    if (size > 1000) throw new Error("400 Invalid limit parameter");
    return channels.slice(page * size, (page + 1) * size);
  };
  return { asked, fetchPage };
};

describe("readAllChannels", () => {
  it("reads a default channel past the first ten", async () => {
    const channels = Array.from({ length: 12 }, (_, i) => channel(i + 1, i === 11));
    const { fetchPage } = server(channels);
    expect(defaultChannelIds(await readAllChannels(fetchPage))).toEqual(["c12"]);
  });

  it("asks for the next page while a page is full", async () => {
    const channels = Array.from({ length: 1005 }, (_, i) => channel(i + 1, i === 1002));
    const { asked, fetchPage } = server(channels);
    const all = await readAllChannels(fetchPage);
    expect(all).toHaveLength(1005);
    expect(defaultChannelIds(all)).toEqual(["c1003"]);
    expect(asked).toEqual([
      { page: 0, limit: 1000 },
      { page: 1, limit: 1000 },
    ]);
  });

  it("reads a workspace without channels in one request", async () => {
    const { asked, fetchPage } = server([]);
    expect(await readAllChannels(fetchPage)).toEqual([]);
    expect(asked).toHaveLength(1);
  });
});
