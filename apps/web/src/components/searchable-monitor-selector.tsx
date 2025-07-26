import { getMonitorsInfiniteOptions } from "@/api/@tanstack/react-query.gen";
import { SearchableMultiSelect, type Option } from "./searchable-multi-select";
import { useInfiniteQuery } from "@tanstack/react-query";
import { useState, useMemo } from "react";
import { useDebounce } from "@/hooks/useDebounce";

const INITIAL_LOAD_SIZE = 20;

const SearchableMonitorSelector = ({
  value,
  onSelect,
}: {
  value: Option[];
  onSelect: (value: Option[]) => void;
}) => {
  const [searchQuery, setSearchQuery] = useState("");
  const debouncedSearch = useDebounce(searchQuery, 300);

  // Fetch monitors using TanStack Query Infinite
  const {
    data: monitorsData,
    isLoading,
    isFetchingNextPage,
    fetchNextPage,
    hasNextPage,
  } = useInfiniteQuery({
    ...getMonitorsInfiniteOptions({
      query: {
        limit: INITIAL_LOAD_SIZE,
        q: debouncedSearch || undefined,
      },
    }),
    getNextPageParam: (lastPage, pages) => {
      const lastLength = lastPage.data?.length || 0;
      if (lastLength < INITIAL_LOAD_SIZE) return undefined;
      return pages.length;
    },
    initialPageParam: 0,
  });

  // Transform monitors data into options array
  const allMonitors = useMemo(
    () =>
      monitorsData?.pages
        ?.flatMap((page) => page.data || [])
        ?.filter((monitor) => Boolean(monitor.id))
        ?.map((monitor) => ({
          label: monitor.name || "Unnamed Monitor",
          value: monitor.id || "",
        })) || [],
    [monitorsData]
  );

  // Handle selection changes
  const handleSelect = (newSelection: Option[]) => {
    onSelect(newSelection);
  };

  // Handle load more (scroll)
  const handleLoadMore = () => {
    if (hasNextPage && !isFetchingNextPage) {
      fetchNextPage();
    }
  };

  return (
    <SearchableMultiSelect
      options={allMonitors}
      selected={value}
      onSelect={handleSelect}
      inputValue={searchQuery}
      setInputValue={setSearchQuery}
      placeholder="Select monitors..."
      onLoadMore={handleLoadMore}
      isLoading={isLoading || isFetchingNextPage}
      nextPage={hasNextPage || false}
    />
  );
};

export default SearchableMonitorSelector;
