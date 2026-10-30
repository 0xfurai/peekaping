import { useMe } from "@/hooks/useMe";

// Whether this install holds a custom domain back until its DNS records prove it
// (VERIFY_CUSTOM_DOMAINS), as /me states it. Off while /me loads, which is today's form.
export const useVerifyCustomDomains = (): boolean =>
  useMe().data?.data?.verifyCustomDomains === true;
