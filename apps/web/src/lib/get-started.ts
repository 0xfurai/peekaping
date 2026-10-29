import type { NotificationChannelCreateUpdateDto, MonitorCreateUpdateDto } from "@/api";
import { httpDefaultValues } from "@/app/monitors/monitor-types/http/schema";
import { serializeMonitor } from "@/app/monitors/monitor-types/registry";
import { channelCreateBody } from "@/app/notification-channels/channel-payload";
import type { NotificationChannelFormValues } from "@/app/notification-channels/channel-types/registry";

// Get started: the first-run page of a workspace (`/w/<id>/get-started`). It stores nothing about
// onboarding: it shows while the workspace has no monitor.
export { GET_STARTED_PATH } from "@/lib/workspace-path";

// The address of a monitor to watch, as an HTTP(S) URL, and the host it is named after. null when
// the input isn't one.
export const parseMonitorUrl = (input: string): { url: string; host: string } | null => {
  const url = input.trim();
  try {
    const parsed = new URL(url);
    if ((parsed.protocol !== "http:" && parsed.protocol !== "https:") || !parsed.host) return null;
    return { url, host: parsed.host };
  } catch {
    return null;
  }
};

// The request that creates the first monitor: an HTTP monitor with the full form's defaults, named
// after the host. It names no probe (an empty probe_ids is left out of the body), so the monitor
// runs on the install's default probes.
export const firstMonitorBody = ({ url, host }: { url: string; host: string }): MonitorCreateUpdateDto =>
  serializeMonitor({ ...httpDefaultValues, url, name: host, probe_ids: [] });

// The channel types step 3 offers, in order. Email, sent by the install, needs a sender for
// account emails.
export const getStartedChannelTypes = (accountEmails: boolean) =>
  [...(accountEmails ? (["install_email"] as const) : []), "telegram", "slack", "discord", "webhook"] as const;

export type CreateChannelAndLinkResult =
  | { kind: "linked"; channelId: string }
  // No monitor was made in step 2, so there is nothing to link
  | { kind: "created"; channelId: string }
  // The channel exists but linking it to the monitor failed
  | { kind: "link_failed"; channelId: string };

// Step 3's save: creates the channel as the default for new monitors, then links it to the monitor
// step 2 made. A failed create throws, for the caller to show; a failed link is a result of its
// own, since the channel exists by then and creating it again would make a second one.
export const createChannelAndLink = async ({
  values,
  monitorId,
  create,
  link,
}: {
  values: NotificationChannelFormValues;
  monitorId: string | null;
  create: (body: NotificationChannelCreateUpdateDto) => Promise<{ id: string }>;
  link: (monitorId: string, channelId: string) => Promise<unknown>;
}): Promise<CreateChannelAndLinkResult> => {
  const { id: channelId } = await create(channelCreateBody(values, true));
  if (monitorId === null) return { kind: "created", channelId };
  return linkChannel({ monitorId, channelId, link });
};

// Links a channel step 3 made to the monitor step 2 made: the save's second half, and the step's
// "Try linking again" after a failed link, which must not create the channel a second time.
export const linkChannel = async ({
  monitorId,
  channelId,
  link,
}: {
  monitorId: string;
  channelId: string;
  link: (monitorId: string, channelId: string) => Promise<unknown>;
}): Promise<Extract<CreateChannelAndLinkResult, { kind: "linked" | "link_failed" }>> => {
  try {
    await link(monitorId, channelId);
    return { kind: "linked", channelId };
  } catch (error) {
    console.error("Failed to link the channel to the monitor", error);
    return { kind: "link_failed", channelId };
  }
};

// What the page does with the workspace's monitors query. The page decides once, on the first
// answer: creating a monitor in step 2 must not send the user away from the page.
export const getStartedGate = ({
  pending,
  error,
  monitors,
}: {
  pending: boolean;
  error: boolean;
  // null is how the API can answer an empty list
  monitors: readonly unknown[] | null | undefined;
}): "loading" | "show" | "monitors" => {
  if (pending) return "loading";
  // Without an answer the monitors list, which reports its own errors, is the safe place
  if (error || monitors === undefined) return "monitors";
  return !monitors || monitors.length === 0 ? "show" : "monitors";
};

export type NextStepLink = {
  key: "monitor" | "monitors" | "status_pages" | "members" | "probes";
  // In the workspace, unless outsideWorkspace
  path: string;
  outsideWorkspace?: true;
};

// Where step 4 leads: the new monitor (or the list, when step 2 made none), status pages and
// members, and for an instance admin the admin area's probes.
export const nextStepLinks = ({
  monitorId,
  instanceAdmin,
}: {
  monitorId: string | null;
  instanceAdmin: boolean;
}): NextStepLink[] => [
  monitorId ? { key: "monitor", path: `/monitors/${monitorId}` } : { key: "monitors", path: "/monitors" },
  { key: "status_pages", path: "/status-pages" },
  { key: "members", path: "/settings/members" },
  ...(instanceAdmin ? [{ key: "probes", path: "/admin/probes", outsideWorkspace: true } as const] : []),
];
