// After signing in, the login address may name where to go: only an invite page, which is where
// someone who chose "Sign in instead" came from. Any other value is ignored, so the parameter
// can't send a user to another site or page.
const INVITE_PATH = /^\/invite\/[A-Za-z0-9_.~-]+$/;

export const safeNextPath = (next: string | null | undefined): string | undefined =>
  next && INVITE_PATH.test(next) && !next.includes("..") ? next : undefined;

/** The login address that returns to the invite after signing in. */
export const inviteNextPath = (token: string): string =>
  `/login?next=${encodeURIComponent(`/invite/${token}`)}`;
