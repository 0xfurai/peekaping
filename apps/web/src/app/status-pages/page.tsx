import { useInfiniteQuery } from "@tanstack/react-query";
import { getStatusPagesInfiniteOptions } from "@/api/@tanstack/react-query.gen";
import Layout from "@/layout";
import { useNavigate } from "react-router-dom";
import type { StatusPageModel } from "@/api";
import { useState, useCallback, useEffect } from "react";
import { useDebounce } from "@/hooks/useDebounce";
import { useSearchParams } from "@/hooks/useSearchParams";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import StatusPageCard from "./components/status-page-card";
import { Label } from "@/components/ui/label";
import { useIntersectionObserver } from "@/hooks/useIntersectionObserver";
import EmptyList from "@/components/empty-list";
import { Button } from "@/components/ui/button";

const StatusPagesPage = () => {
  const navigate = useNavigate();
  const { getParam, updateSearchParams, clearAllParams, hasParams } =
    useSearchParams();

  // Initialize search from URL params
  const [search, setSearch] = useState(getParam("q") || "");
  const debouncedSearch = useDebounce(search, 400);

  // Update URL when debounced search changes
  useEffect(() => {
    updateSearchParams({ q: debouncedSearch });
  }, [debouncedSearch, updateSearchParams]);

  const clearAllFilters = () => {
    setSearch("");
    clearAllParams();
  };

  const { data, isLoading, fetchNextPage, hasNextPage, isFetchingNextPage } =
    useInfiniteQuery({
      ...getStatusPagesInfiniteOptions({
        query: {
          limit: 20,
          q: debouncedSearch || undefined,
        },
      }),
      getNextPageParam: (lastPage, pages) => {
        const lastLength = lastPage.data?.length || 0;
        if (lastLength < 20) return undefined;
        return pages.length;
      },
      initialPageParam: 0,
      enabled: true,
    });

  const statusPages = (data?.pages.flatMap((page) => page.data || []) ||
    []) as StatusPageModel[];

  const handleObserver = useCallback(
    (entries: IntersectionObserverEntry[]) => {
      const [entry] = entries;
      if (entry.isIntersecting && hasNextPage && !isFetchingNextPage) {
        fetchNextPage();
      }
    },
    [fetchNextPage, hasNextPage, isFetchingNextPage]
  );

  const { ref: sentinelRef } =
    useIntersectionObserver<HTMLDivElement>(handleObserver);

  return (
    <Layout
      pageName="Status pages"
      onCreate={() => {
        navigate("/status-pages/new");
      }}
    >
      <div>
        <div className="mb-4 space-y-4">
          <div className="flex flex-col gap-4 sm:flex-row sm:justify-end sm:gap-4 items-end">
            {hasParams() && (
              <div className="flex justify-start">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={clearAllFilters}
                  className="w-fit h-[36px]"
                >
                  Clear all filters
                </Button>
              </div>
            )}
            <div className="flex flex-col gap-1 w-full sm:w-auto">
              <Label htmlFor="search-status-pages">Search</Label>
              <Input
                id="search-status-pages"
                placeholder="Search status pages by title..."
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="w-full sm:w-[400px]"
              />
            </div>
          </div>
        </div>

        {statusPages.length === 0 && isLoading && (
          <div className="flex flex-col space-y-2 mb-2">
            {Array.from({ length: 7 }, (_, id) => (
              <Skeleton className="h-[68px] w-full rounded-xl" key={id} />
            ))}
          </div>
        )}

        {/* Status pages list */}
        {statusPages.map((statusPage) => (
          <StatusPageCard
            key={statusPage.id}
            statusPage={statusPage}
            onClick={() => navigate(`/status-pages/${statusPage.id}/edit`)}
          />
        ))}

        {/* Sentinel for infinite scroll */}
        <div ref={sentinelRef} style={{ height: 1 }} />
        {isFetchingNextPage && (
          <div className="flex flex-col space-y-2 mb-2">
            {Array.from({ length: 3 }, (_, i) => (
              <Skeleton key={i} className="h-[68px] w-full rounded-xl" />
            ))}
          </div>
        )}

        {/* Empty state */}
        {statusPages.length === 0 && !isLoading && (
          <EmptyList
            title="No status pages found"
            text="Get started by creating your first status page."
            actionText="Create your first status page"
            onClick={() => navigate("/status-pages/new")}
          />
        )}
      </div>
    </Layout>
  );
};

export default StatusPagesPage;
