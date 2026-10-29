import { isAxiosError } from "axios";
import { toast } from "sonner";

// A write over one of the workspace's limits (PP-160) answers 403 with the code LIMIT_REACHED, the
// limit and its value (limits.ReachedError in apps/server/internal/limits). These word it in the
// user's language, instead of the server's English message.

// The limits a write can go over, as the server names them
export const REACHABLE_LIMITS = [
  "monitors",
  "status_pages",
  "custom_domains",
  "members",
  "owned_workspaces",
  "probes",
] as const;

export type ReachableLimit = (typeof REACHABLE_LIMITS)[number];

/** The limit a refused write would go over, and its value, or undefined for any other answer. */
export const limitReached = (error: unknown): { limit: ReachableLimit; max: number } | undefined => {
  if (!isAxiosError(error)) return undefined;
  const body = (error.response?.data as { error?: { code?: unknown; limit?: unknown; max?: unknown } } | undefined)
    ?.error;
  if (body?.code !== "LIMIT_REACHED" || typeof body.max !== "number") return undefined;
  const limit = REACHABLE_LIMITS.find((name) => name === body.limit);
  return limit ? { limit, max: body.max } : undefined;
};

type Translate = (key: string, vars?: Record<string, unknown>) => string;

/**
 * The translated message of a write over a limit, such as "This workspace can have at most 10
 * monitors running", or undefined when the error is no such refusal and the caller words it as
 * before.
 */
export const limitReachedMessage = (error: unknown, t: Translate): string | undefined => {
  const reached = limitReached(error);
  return reached ? t(`limits.reached.${reached.limit}`, { max: reached.max }) : undefined;
};

/**
 * A mutation's onError that shows the translated message of a write over a limit, and leaves any
 * other error to otherwise
 */
export const showLimitReached =
  (t: Translate, otherwise: (error: unknown) => void) =>
  (error: unknown): void => {
    const message = limitReachedMessage(error, t);
    if (message) toast.error(message);
    else otherwise(error);
  };
