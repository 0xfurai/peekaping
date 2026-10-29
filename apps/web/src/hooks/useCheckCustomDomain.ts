import { getStatusPagesDomainByDomainOptions, getStatusPagesSlugBySlugQueryKey } from "@/api/@tanstack/react-query.gen";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { isStatusPageUnavailable } from "@/lib/admin-workspaces";

export const useCheckCustomDomain = (domain: string) => {
  const queryClient = useQueryClient();

  const { data: customDomain, isFetched, error } = useQuery({
    ...getStatusPagesDomainByDomainOptions({
      path: {
        domain,
      },
    }),
    refetchOnWindowFocus: false,
  });

  useEffect(() => {
    if (customDomain?.data?.slug) {
      queryClient.setQueryData(
        getStatusPagesSlugBySlugQueryKey({
          path: {
            slug: customDomain.data.slug,
          },
        }),
        customDomain
      );
    }
  }, [customDomain, queryClient]);

  return {
    customDomain,
    isFetched,
    // The domain is a status page's whose workspace is suspended
    unavailable: isStatusPageUnavailable(error),
  };
};
