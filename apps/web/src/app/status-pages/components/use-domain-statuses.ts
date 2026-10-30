import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  getStatusPagesDomainsByIdOptions,
  getStatusPagesDomainsByIdQueryKey,
  postStatusPagesDomainsByIdByDomainVerifyMutation,
} from "@/api/@tanstack/react-query.gen";
import type { UtilsApiResponseStatusPageDomainStatusesDto } from "@/api/types.gen";
import { refetchIntervalFor, withCheckedDomain } from "./domain-state";

// The saved domains of a status page with their verification state. Nothing is asked for a
// page that isn't saved yet, or when `enabled` is false (verification is off). The list is asked
// for again every minute while a domain is pending.
export const useDomainStatuses = (pageId: string | undefined, enabled: boolean) => {
  const query = useQuery({
    ...getStatusPagesDomainsByIdOptions({ path: { id: pageId ?? "" } }),
    enabled: enabled && !!pageId,
    refetchInterval: (query) => refetchIntervalFor(query.state.data?.data?.domains),
  });
  return { domains: query.data?.data?.domains, isLoading: query.isLoading };
};

// Checks one domain's DNS records now and puts the answer in the page's list of domains
export const useCheckDomain = (pageId: string, onError: () => void) => {
  const queryClient = useQueryClient();
  const queryKey = getStatusPagesDomainsByIdQueryKey({ path: { id: pageId } });
  return useMutation({
    ...postStatusPagesDomainsByIdByDomainVerifyMutation(),
    onSuccess: ({ data: checked }) => {
      queryClient.setQueryData<UtilsApiResponseStatusPageDomainStatusesDto>(queryKey, (old) =>
        old ? { ...old, data: withCheckedDomain(old.data, checked) } : old
      );
    },
    onError,
  });
};
