import { useAuthConfig } from "@/hooks/useAuthConfig";

// Whether the install sends account emails (a password reset link, a verification link). It is
// false while /auth/config loads and when it fails, so a page offers nothing it can't deliver.
export const useAccountEmails = (): boolean => useAuthConfig().config?.accountEmails === true;
