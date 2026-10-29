import { useQuery } from "@tanstack/react-query";
import { getAuthConfigOptions } from "@/api/@tanstack/react-query.gen";

// The install's /auth/config, which is fetched before anyone signs in: whether it sends account
// emails, who may sign up and the Turnstile site key. `config` is undefined while it loads, and
// `failed` says when it could not be fetched.
export const useAuthConfig = () => {
  const { data, isError } = useQuery({ ...getAuthConfigOptions(), staleTime: 60_000 });
  return { config: data?.data, failed: isError && !data };
};
