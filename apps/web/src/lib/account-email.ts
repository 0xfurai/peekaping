import { isAxiosError } from "axios";

// How a call of the account email endpoints (forgot and reset password, verify, resend) failed,
// by the status and message the server answers with (auth.account.controller). The page shows
// the translated text of each.
export type AccountEmailFailure =
  | "invalid_link"
  | "rate_limited"
  | "already_verified"
  | "error";

export const accountEmailFailure = (error: unknown): AccountEmailFailure => {
  if (!isAxiosError(error)) return "error";
  const status = error.response?.status;
  const message = String(error.response?.data?.message ?? "").toLowerCase();
  // A 400 is also a password the rules refuse; only the answer about the link means "used or expired"
  if (status === 400 && message.includes("link")) return "invalid_link";
  if (status === 429) return "rate_limited";
  if (status === 409) return "already_verified";
  return "error";
};

/** Whether the signed-in app asks the user to verify their address: the install sends emails and this one isn't verified. */
export const needsVerification = (
  me: { accountEmails: boolean; user: { emailVerified: boolean } } | undefined
): boolean => Boolean(me?.accountEmails) && me?.user.emailVerified === false;
