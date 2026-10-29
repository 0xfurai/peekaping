import { isAxiosError } from "axios";
import type {
  AdminUpdateWorkspaceDto,
  AdminWorkspaceLimitsResponse,
  AdminWorkspaceResponse,
} from "@/api/types.gen";

// What an instance admin can do to a workspace on the Workspaces page (PP-372): set its limits
// (PP-160), suspend or resume it, and delete it. The server decides; these only keep the page from
// offering what changes nothing.
export type WorkspaceAction = "limits" | "suspend" | "resume" | "delete";

// The reason the server gives a hold. "admin" comes before "unverified" when both hold.
const BY_ADMIN = "admin";

// The actions a row offers, in the order of its menu. Resume lifts only an admin's hold, so a
// workspace that waits for its owner's email verification is offered Suspend, which puts an
// admin's hold on top of it, and no Resume.
export const workspaceActions = (workspace: AdminWorkspaceResponse): WorkspaceAction[] => [
  "limits",
  workspace.suspendedReason === BY_ADMIN ? "resume" : "suspend",
  "delete",
];

// The limits a workspace can have of its own, in the order the page shows them. The workspaces a
// user owns are counted against the install's defaults alone, so a workspace has no such limit of
// its own, and the page leaves it out.
export const OWN_LIMITS = [
  "monitors",
  "min_check_interval",
  "status_pages",
  "custom_domains",
  "members",
  "probes_per_monitor",
  "retention_days",
] as const;

export type OwnLimit = (typeof OWN_LIMITS)[number];

// How many limits the Limits column names before it says how many more there are
const SUMMARY_SIZE = 2;

/** The limits of a workspace the Limits column names, and how many more it has. */
export const limitsSummary = (
  limits: AdminWorkspaceLimitsResponse
): { shown: { name: OwnLimit; value: number }[]; more: number } => {
  const set = OWN_LIMITS.flatMap((name) => {
    const value = limits.values[name];
    return value === null ? [] : [{ name, value }];
  });
  return { shown: set.slice(0, SUMMARY_SIZE), more: Math.max(0, set.length - SUMMARY_SIZE) };
};

// What the Limits dialog gives a workspace: limits of its own, no limits at all (which is a value of
// its own), or the install's defaults again
export type LimitsChoice = "own" | "unlimited" | "defaults";

// The dialog's choice and its fields, as typed: a blank field is no limit
export type LimitsDraft = { choice: LimitsChoice; values: Record<OwnLimit, string> };

/**
 * The dialog as it opens: the workspace's own limits when it has them, else the install's defaults,
 * with the limits it is held to now to start its own from.
 */
export const limitsDraft = (limits: AdminWorkspaceLimitsResponse): LimitsDraft => {
  const values = Object.fromEntries(
    OWN_LIMITS.map((name) => [name, limits.values[name] === null ? "" : String(limits.values[name])])
  ) as Record<OwnLimit, string>;
  const choice: LimitsChoice =
    limits.source !== "workspace"
      ? "defaults"
      : OWN_LIMITS.some((name) => limits.values[name] !== null)
        ? "own"
        : "unlimited";
  return { choice, values };
};

const WHOLE = /^\d+$/;

/**
 * The request that saves the dialog's choice: limits of its own with the blank ones left out
 * (which the server takes as no limit), {} for no limits, or clearLimits for the install's
 * defaults. A limit that is no whole number of 1 or more is named instead, and nothing is sent.
 */
export const limitsBody = (draft: LimitsDraft): { body: AdminUpdateWorkspaceDto } | { invalid: OwnLimit[] } => {
  if (draft.choice === "defaults") return { body: { clearLimits: true } };
  if (draft.choice === "unlimited") return { body: { limits: {} } };
  const own: Partial<Record<OwnLimit, number>> = {};
  const invalid: OwnLimit[] = [];
  for (const name of OWN_LIMITS) {
    const typed = draft.values[name].trim();
    if (typed === "") continue;
    if (WHOLE.test(typed) && Number(typed) >= 1) own[name] = Number(typed);
    else invalid.push(name);
  }
  return invalid.length > 0 ? { invalid } : { body: { limits: own } };
};

/** Whether an instance admin suspended the workspace, as /me says for the workspace in the address. */
export const isSuspendedByAdmin = (
  workspaces: { id: string; suspended?: boolean; suspendedReason?: string | null }[] | null | undefined,
  workspaceId: string | null
): boolean => {
  const workspace = workspaces?.find((w) => w.id.toLowerCase() === workspaceId?.toLowerCase());
  return workspace?.suspended === true && workspace.suspendedReason === BY_ADMIN;
};

// The delete asks for the workspace's name, typed exactly: no trimming and no case folding, so a
// slip of the keyboard can't delete a workspace of a similar name.
export const canDeleteWorkspace = (typed: string, name: string): boolean => typed === name;

const EMAIL = /^[^\s@/:]+@[^\s@/]+\.[^\s@/]+$/;
const WEB_ADDRESS = /^https?:\/\/\S+$/i;

// How the banner of a suspended workspace shows the install's SUPPORT_CONTACT: a mailto link for
// an email address, a link for a web address, else the text as it is. Only http and https make a
// link, so a contact can't run a script. Null without a contact.
export const contactLink = (
  contact: string | null | undefined
): { text: string; href: string | null } | null => {
  const text = contact?.trim() ?? "";
  if (text === "") return null;
  if (EMAIL.test(text)) return { text, href: `mailto:${text}` };
  if (WEB_ADDRESS.test(text)) return { text, href: text };
  return { text, href: null };
};

// The code the public status page and badge routes answer 503 with when the page's workspace is
// suspended (status_page.api.go). It says nothing of why.
const UNAVAILABLE = "STATUS_PAGE_UNAVAILABLE";

/** Whether a public status page request failed because the page's workspace is suspended. */
export const isStatusPageUnavailable = (error: unknown): boolean => {
  if (!isAxiosError(error) || error.response?.status !== 503) return false;
  const body = error.response.data as { error?: { code?: unknown } } | undefined;
  return body?.error?.code === UNAVAILABLE;
};

// The page of the Workspaces list to show, counted from 0, when the list holds total workspaces:
// page, or the last page once the list has shrunk under it
export const workspacesPageWithin = (page: number, total: number, pageSize: number): number =>
  Math.min(page, Math.max(0, Math.ceil(total / pageSize) - 1));
