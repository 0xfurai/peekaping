import { useSearchParams as useRouterSearchParams } from "react-router-dom";
import { useCallback } from "react";

export const useSearchParams = () => {
  const [searchParams, setSearchParams] = useRouterSearchParams();

  const updateSearchParams = useCallback(
    (updates: Record<string, string | null>) => {
      const newSearchParams = new URLSearchParams(searchParams);

      Object.entries(updates).forEach(([key, value]) => {
        if (value === null || value === "" || value === "all") {
          newSearchParams.delete(key);
        } else {
          newSearchParams.set(key, value);
        }
      });

      setSearchParams(newSearchParams, { replace: true });
    },
    [searchParams, setSearchParams]
  );

  const clearAllParams = useCallback(() => {
    setSearchParams({}, { replace: true });
  }, [setSearchParams]);

  return {
    searchParams,
    updateSearchParams,
    getParam: (key: string) => searchParams.get(key),
    getAllParams: () => Object.fromEntries(searchParams.entries()),
    clearAllParams,
    hasParams: () => searchParams.toString().length > 0,
  };
};
