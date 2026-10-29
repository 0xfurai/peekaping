import { isAxiosError } from "axios";
import type { AdminWorkspaceResponse } from "@/api/types.gen";

// What an instance admin can do to a workspace on the Workspaces page (PP-372). The server decides;
// these only keep the page from offering what changes nothing.
export type WorkspaceAction = "suspend" | "resume" | "delete";

// The reason the server gives a hold. "admin" comes before "unverified" when both hold.
const BY_ADMIN = "admin";

// The actions a row offers, in the order of its menu. Resume lifts only an admin's hold, so a
// workspace that waits for its owner's email verification is offered Suspend, which puts an
// admin's hold on top of it, and no Resume.
export const workspaceActions = (workspace: AdminWorkspaceResponse): WorkspaceAction[] => [
  workspace.suspendedReason === BY_ADMIN ? "resume" : "suspend",
  "delete",
];

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
