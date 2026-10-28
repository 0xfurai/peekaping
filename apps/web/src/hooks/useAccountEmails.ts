import { useQuery } from "@tanstack/react-query";
import { getAuthConfigOptions } from "@/api/@tanstack/react-query.gen";

// Whether the install sends account emails (a password reset link, a verification link). It is
// false while /auth/config loads and when it fails, so a page offers nothing it can't deliver.
export const useAccountEmails = (): boolean => {
  const { data } = useQuery({ ...getAuthConfigOptions(), staleTime: 60_000 });
  return data?.data?.accountEmails === true;
};
