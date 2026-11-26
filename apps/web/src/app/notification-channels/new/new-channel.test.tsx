import { describe, expect, it } from "vitest";
import { renderAs, text, WORKSPACE_ID } from "@/test-support/render-with-me";
import { channelCreateBody, createsDefaultChannel } from "../channel-payload";
import NewNotificationChannel from "./page";

const values = { name: "Team", type: "webhook" } as Parameters<typeof channelCreateBody>[0];

describe("The new channel page", () => {
  const open = (search: string) =>
    renderAs({ instanceAdmin: false, role: "owner" }, <NewNotificationChannel />, `/w/${WORKSPACE_ID}/notification-channels/new${search}`);

  it("makes a default channel when the address asks for one", () => {
    expect(createsDefaultChannel(new URLSearchParams("?default=true"))).toBe(true);
    expect(channelCreateBody(values, createsDefaultChannel(new URLSearchParams("?default=true"))).is_default).toBe(true);
  });

  it("makes a channel that isn't a default one otherwise", () => {
    for (const search of ["", "?default=false", "?default=1"]) {
      expect(createsDefaultChannel(new URLSearchParams(search))).toBe(false);
      expect(channelCreateBody(values, createsDefaultChannel(new URLSearchParams(search))).is_default).toBe(false);
    }
  });

  it("says a default channel will be ticked on new monitors", () => {
    expect(open("?default=true")).toContain(text("notifications.new.default_hint"));
    expect(open("")).not.toContain(text("notifications.new.default_hint"));
  });
});
