import { isAxiosError } from "axios";
import type { AdminUserResponse } from "@/api/types.gen";

// What an instance admin can do to a user on the Users page (docs/adr/0005, section 10). The
// server decides and answers 409 for the rules; these only keep the page from offering what it
// would refuse.
export type UserAction =
  | "grantAdmin"
  | "revokeAdmin"
  | "disable"
  | "enable"
  | "disableTwoFactor"
  | "resetLink"
  | "delete";

// Whether the user is the only active instance admin there is, as far as the list shows. Only a
// list that holds every user can say so: a page of search results may leave another one out.
export const isLastInstanceAdmin = (
  user: AdminUserResponse,
  users: AdminUserResponse[],
  complete: boolean
): boolean =>
  complete &&
  user.instanceAdmin &&
  user.active &&
  !users.some((other) => other.id !== user.id && other.instanceAdmin && other.active);

// The actions a row offers, in the order of its menu. Your own row has no disable and no delete,
// and no way to take your own flag while you are the last instance admin.
export const userActions = (
  user: AdminUserResponse,
  { self, onlyInstanceAdmin }: { self: boolean; onlyInstanceAdmin: boolean }
): UserAction[] => {
  const actions: UserAction[] = [];
  if (!user.instanceAdmin) actions.push("grantAdmin");
  else if (!(self && onlyInstanceAdmin)) actions.push("revokeAdmin");
  if (!self) actions.push(user.active ? "disable" : "enable");
  if (user.twoFactorEnabled) actions.push("disableTwoFactor");
  actions.push("resetLink");
  if (!self) actions.push("delete");
  return actions;
};

type Translate = (key: string, vars?: Record<string, unknown>) => string;

// The workspaces the server names in "The user is the last owner of: "A", "B". Make someone ...",
// as it writes them, quotes and commas included
const LAST_OWNER = /last owner of: (.+)\. Make someone else an owner/;

// The text to show for a refused or failed admin request: the translated reason for the refusals
// the server words in English (apps/server/internal/modules/admin/admin.controller.go), with the
// named workspaces in the last-owner one; else the server's message, else the fallback.
export const adminErrorMessage = (
  error: unknown,
  t: Translate,
  fallback: string
): string => {
  if (!isAxiosError(error) || !error.response) return fallback;
  const message = (error.response.data as { message?: unknown } | undefined)?.message;
  const text = typeof message === "string" ? message : "";
  if (error.response.status === 409) {
    const workspaces = LAST_OWNER.exec(text)?.[1];
    if (workspaces) return t("admin.users.errors.last_owner", { workspaces });
    if (/your own account/i.test(text)) return t("admin.users.errors.self");
    if (/at least one instance admin/i.test(text)) {
      return t("admin.users.errors.last_instance_admin");
    }
  }
  return text || fallback;
};
