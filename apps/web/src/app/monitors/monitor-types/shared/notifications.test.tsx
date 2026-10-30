import { beforeEach, describe, expect, it, vi } from "vitest";
import type { QueryClient } from "@tanstack/react-query";
import { FormProvider, useForm } from "react-hook-form";
import type { NotificationChannelModel } from "@/api/types.gen";
import { renderAs, text } from "@/test-support/render-with-me";
import { allNotificationChannelsOptions } from "./channel-support";
import Notifications from "./notifications";

const { asked, clicks } = vi.hoisted(() => ({
  asked: vi.fn(),
  clicks: new Map<string, () => void>(),
}));

// The request for the channel list, answered by a spy so that a retry can be seen
vi.mock("@/api/sdk.gen", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/sdk.gen")>()),
  getNotificationChannels: asked,
}));

// Static markup has no clicks: the buttons hand their handlers to the test by their text
vi.mock("@/components/ui/button", () => ({
  Button: ({ children, onClick }: { children?: unknown; onClick?: () => void }) => {
    if (typeof children === "string" && onClick) clicks.set(children, onClick);
    return <button type="button">{children as never}</button>;
  },
}));

const channel = (n: number): NotificationChannelModel => ({
  id: `c${n}`,
  name: `Channel ${n}`,
  type: "webhook",
  config: null,
  active: true,
  is_default: false,
  created_at: "2026-10-28T00:00:00Z",
  updated_at: "2026-10-28T00:00:00Z",
});

const Picker = ({ ids }: { ids: string[] }) => {
  const form = useForm({ defaultValues: { notification_ids: ids } });
  return (
    <FormProvider {...form}>
      <Notifications onNewNotifier={() => {}} />
    </FormProvider>
  );
};

const picker = (seed: (client: QueryClient) => void, ids: string[] = []) =>
  renderAs({ instanceAdmin: false, role: "admin" }, <Picker ids={ids} />, undefined, seed);

const loaded = (channels: NotificationChannelModel[]) => (client: QueryClient) =>
  client.setQueryData(allNotificationChannelsOptions().queryKey, { message: "success", data: channels });

// The channel list asked for and refused, as the query leaves it after a failed request. A
// mounting query would ask again at once, so it is told to leave the failure as it is.
const failedRequest = (client: QueryClient) => {
  const { queryKey } = allNotificationChannelsOptions();
  client.setQueryDefaults(queryKey, { retryOnMount: false });
  client
    .getQueryCache()
    .build(client, { queryKey })
    .setState({ status: "error", error: new Error("boom"), fetchStatus: "idle" });
};

const LOAD_FAILED = text("monitors.form.shared.notifications.load_failed");
const NO_CHANNELS = text("monitors.form.shared.notifications.no_channels");

describe("The monitor form's notification picker", () => {
  beforeEach(() => {
    asked.mockReset();
    asked.mockReturnValue(new Promise(() => {}));
    clicks.clear();
  });

  it("works from every loaded channel, the twelfth included", () => {
    const channels = Array.from({ length: 12 }, (_, i) => channel(i + 1));
    const html = picker(loaded(channels), ["c12"]);
    expect(html).toContain("Channel 12");
    expect(html).toContain(text("monitors.form.shared.notifications.add_notifier"));
    expect(html).not.toContain(LOAD_FAILED);
  });

  it("says there are no channels when the workspace has none", () => {
    const html = picker(loaded([]));
    expect(html).toContain(NO_CHANNELS);
    expect(html).not.toContain(LOAD_FAILED);
  });

  it("says the channels could not be loaded, not that there are none, when the request failed", () => {
    const html = picker(failedRequest);
    expect(html).toContain(LOAD_FAILED);
    expect(html).not.toContain(NO_CHANNELS);
    expect(html).toContain(text("common.retry"));
  });

  it("asks for the channels again when the retry button is clicked", () => {
    picker(failedRequest);
    const retry = clicks.get(text("common.retry"));
    expect(retry).toBeDefined();
    expect(asked).not.toHaveBeenCalled();
    retry?.();
    expect(asked).toHaveBeenCalledTimes(1);
  });
});
