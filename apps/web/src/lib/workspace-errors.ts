import { isAxiosError } from "axios";
import { limitReachedMessage } from "./limit-errors";

// The translation key of a workspace, member or invite answer the server words in English, by
// its status and message (workspacehttp.Fail, auth.controller). undefined when the answer isn't
// one of those, and the caller shows the server's own message.
export const workspaceErrorKey = (status: number, message: string): string | undefined => {
  const m = message.toLowerCase();
  switch (status) {
    case 403:
      return m.includes("another email")
        ? "invite.errors.other_email"
        : m.includes("role")
          ? "members.errors.forbidden"
          : undefined;
    case 404:
      return m.includes("invite")
        ? "invite.unavailable.not_found"
        : m.includes("member")
          ? "members.errors.member_not_found"
          : undefined;
    case 409:
      return m.includes("at least one owner")
        ? "members.errors.last_owner"
        : m.includes("already a member")
          ? "members.errors.already_member"
          : m.includes("account with this email")
            ? "members.errors.account_exists"
            : undefined;
    case 410:
      return m.includes("already used")
        ? "invite.unavailable.accepted"
        : m.includes("expired")
          ? "invite.unavailable.expired"
          : m.includes("revoked")
            ? "invite.unavailable.revoked"
            : "invite.unavailable.unknown";
    default:
      return undefined;
  }
};

// The text to show for a failed workspace request: a limit the request would go over (an invite,
// accepting one, signing up through one, a new workspace), else the translated reason when the
// server gave one we know, else the server's message, else the fallback.
export const workspaceErrorMessage = (
  error: unknown,
  t: (key: string, vars?: Record<string, unknown>) => string,
  fallback: string
): string => {
  if (!isAxiosError(error)) return fallback;
  const limit = limitReachedMessage(error, t);
  if (limit) return limit;
  const message = (error.response?.data as { message?: unknown } | undefined)?.message;
  const text = typeof message === "string" ? message : "";
  const key = error.response ? workspaceErrorKey(error.response.status, text) : undefined;
  return key ? t(key) : text || fallback;
};
