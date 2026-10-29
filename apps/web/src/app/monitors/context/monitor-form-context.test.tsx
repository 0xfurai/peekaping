import { describe, expect, it } from "vitest";
import type { QueryClient } from "@tanstack/react-query";
import { getMonitorsByIdQueryKey } from "@/api/@tanstack/react-query.gen";
import type { NotificationChannelModel } from "@/api/types.gen";
import { renderAs } from "@/test-support/render-with-me";
import { deserializeMonitor, monitorTypes, type MonitorFormValues } from "../monitor-types/registry";
import { allNotificationChannelsOptions } from "../monitor-types/shared/channel-support";
import { MonitorFormProvider, useMonitorFormContext } from "./monitor-form-context";

const channel = (id: string, isDefault: boolean): NotificationChannelModel => ({
  id,
  name: id,
  type: "webhook",
  config: null,
  active: true,
  is_default: isDefault,
  created_at: "2026-10-28T00:00:00Z",
  updated_at: "2026-10-28T00:00:00Z",
});

const channels = [channel("alerts", true), channel("pager", false), channel("team", true)];

const defaults = monitorTypes[0].defaultValues as MonitorFormValues;

// The channels the form starts on, as the provider holds them in the form
const StartingChannels = () => {
  const { form } = useMonitorFormContext();
  return <output>{JSON.stringify(form.getValues("notification_ids"))}</output>;
};

const startingChannels = (html: string): string[] => JSON.parse(html.match(/<output>(.*)<\/output>/)![1].replace(/&quot;/g, '"'));

const seedChannels = (list: NotificationChannelModel[] | null) => (queryClient: QueryClient) =>
  queryClient.setQueryData(allNotificationChannelsOptions().queryKey, { message: "success", data: list });

describe("The channels a monitor form starts on", () => {
  const create = (initial?: Partial<MonitorFormValues>, list: NotificationChannelModel[] | null = channels) =>
    renderAs(
      { instanceAdmin: false, role: "member" },
      <MonitorFormProvider mode="create" initialValues={initial ? ({ ...defaults, ...initial } as MonitorFormValues) : undefined}>
        <StartingChannels />
      </MonitorFormProvider>,
      undefined,
      seedChannels(list)
    );

  it("are the workspace's default channels for a new monitor", () => {
    expect(startingChannels(create())).toEqual(["alerts", "team"]);
  });

  it("are none when no channel is a default one", () => {
    expect(startingChannels(create(undefined, [channel("pager", false)]))).toEqual([]);
  });

  it("are none while the channels haven't loaded, or the workspace has none", () => {
    expect(startingChannels(create(undefined, null))).toEqual([]);
    expect(startingChannels(create(undefined, []))).toEqual([]);
  });

  it("are the source's for a clone, whether or not it has any", () => {
    expect(startingChannels(create({ notification_ids: ["pager"] }))).toEqual(["pager"]);
    expect(startingChannels(create({ notification_ids: [] }))).toEqual([]);
  });

  describe("an edit", () => {
    const monitor = {
      id: "m1",
      name: "Site",
      type: "http",
      interval: 60,
      timeout: 16,
      max_retries: 3,
      retry_interval: 60,
      resend_interval: 10,
      config: JSON.stringify({ url: "https://example.com", method: "GET", encoding: "json", accepted_statuscodes: ["2XX"] }),
      notification_ids: ["pager"],
    };
    const edit = (saved: typeof monitor) =>
      renderAs(
        { instanceAdmin: false, role: "member" },
        <MonitorFormProvider mode="edit" monitorId="m1" initialValues={deserializeMonitor(saved as never)}>
          <StartingChannels />
        </MonitorFormProvider>,
        undefined,
        (queryClient) => {
          seedChannels(channels)(queryClient);
          queryClient.setQueryData(getMonitorsByIdQueryKey({ path: { id: "m1" } }), { data: saved });
        }
      );

    it("keeps the monitor's own channels", () => {
      expect(startingChannels(edit(monitor))).toEqual(["pager"]);
    });

    it("keeps a monitor with none on none", () => {
      expect(startingChannels(edit({ ...monitor, notification_ids: [] }))).toEqual([]);
    });
  });
});
