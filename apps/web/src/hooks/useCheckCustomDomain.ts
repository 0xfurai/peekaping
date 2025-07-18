import { getStatusPagesDomainByDomainOptions, getStatusPagesSlugBySlugQueryKey } from "@/api/@tanstack/react-query.gen";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";

export const useCheckCustomDomain = (domain: string) => {
  const queryClient = useQueryClient();

  const {
    data: customDomain,
    isLoading: isCustomDomainLoading,
    isFetched,
  } = useQuery({
    ...getStatusPagesDomainByDomainOptions({
      path: {
        domain,
      },
    }),
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
    isCustomDomainLoading,
    isFetched,
  };
};