import { getMonitorsOptions } from "@/api/@tanstack/react-query.gen";
import { FancyMultiSelect, type Option } from "./multiselect-3";
import { useQuery } from "@tanstack/react-query";
import { useState, useMemo, useEffect } from "react";
import { useDebounce } from "@/hooks/useDebounce";

const INITIAL_LOAD_SIZE = 20;

interface PaginationState {
  currentPage: number | null;
  totalItemsFetched: number;
  nextItemsToFetch: number;
}

const SearchableMonitorSelector = ({
  value,
  onSelect,
}: {
  value: Option[];
  onSelect: (value: Option[]) => void;
}) => {
  const [searchQuery, setSearchQuery] = useState("");
  const [allMonitors, setAllMonitors] = useState<Option[]>([]);
  const [monitorIds, setMonitorIds] = useState<Set<string>>(new Set());
  const [pagination, setPagination] = useState<PaginationState>({
    currentPage: 0,
    totalItemsFetched: 0,
    nextItemsToFetch: INITIAL_LOAD_SIZE,
  });
  const [shouldFetch, setShouldFetch] = useState(true);
  const debouncedSearch = useDebounce(searchQuery, 300);

  // Reset when search changes
  useEffect(() => {
    setAllMonitors([]);
    setMonitorIds(new Set());
    setPagination({
      currentPage: 0,
      totalItemsFetched: 0,
      nextItemsToFetch: INITIAL_LOAD_SIZE,
    });
    setShouldFetch(true);
  }, [debouncedSearch]);

  // Calculate the correct page number based on items already fetched
  const calculateNextPage = () => {
    // For initial load or after search reset
    if (pagination.totalItemsFetched === 0) return 0;
    
    // Fetch 1 item after item selection
    if (pagination.nextItemsToFetch < INITIAL_LOAD_SIZE) {
      return Math.floor(pagination.totalItemsFetched / pagination.nextItemsToFetch);
    }
    
    // Fetch 20 items for scrolling
    return Math.floor(pagination.totalItemsFetched / INITIAL_LOAD_SIZE);
  };

  const nextPage = calculateNextPage();

  // Fetch monitors using TanStack Query
  const { data: monitorsData, isLoading, isFetching } = useQuery({
    ...getMonitorsOptions({
      query: {
        limit: pagination.nextItemsToFetch,
        page: nextPage,
        q: debouncedSearch,
      },
    }),
    enabled: pagination.currentPage !== null && shouldFetch,
  });

  // Update monitors when data arrives
  useEffect(() => {
    if (shouldFetch && monitorsData) {
      if (!monitorsData.data || monitorsData.data.length === 0) {
        setPagination(prev => ({ ...prev, currentPage: null }));
        setShouldFetch(false);
        return;
      }

      const newOptions = monitorsData.data
        .filter((monitor) => !monitorIds.has(monitor.id || "")) // Filter out duplicates
        .map((monitor) => ({
          label: monitor.name || "Unnamed Monitor",
          value: monitor.id || "",
        }));

      const newIds = new Set(monitorIds);
      monitorsData.data.forEach((monitor) => {
        if (monitor.id) {
          newIds.add(monitor.id);
        }
      });
      setMonitorIds(newIds);
      setAllMonitors((prev) => [...prev, ...newOptions]);

      setPagination(prev => ({
        ...prev,
        totalItemsFetched: prev.totalItemsFetched + newOptions.length,
        currentPage: monitorsData.data.length === pagination.nextItemsToFetch ? prev.currentPage : null,
      }));

      setShouldFetch(false);
    }
  }, [monitorsData, pagination.nextItemsToFetch, shouldFetch, monitorIds]);

  // Filter out already selected monitors
  const availableOptions = useMemo(() => {
    return allMonitors.filter(
      (opt) => !value.some((v) => v.value === opt.value)
    );
  }, [allMonitors, value]);

  // Handle selection changes
  const handleSelect = (newSelection: Option[]) => {
    const selectionIncreased = newSelection.length > value.length;
    const itemsSelected = newSelection.length - value.length;
    
    // Call the parent's onSelect first
    onSelect(newSelection);
    
    // If selection increased and we need more items
    if (selectionIncreased && itemsSelected > 0 && availableOptions.length - itemsSelected < INITIAL_LOAD_SIZE && pagination.currentPage !== null) {
      setPagination(prev => ({
        ...prev,
        currentPage: prev.currentPage ? prev.currentPage + 1 : 0,
        nextItemsToFetch: Math.abs(itemsSelected),
      }));
      setShouldFetch(true);
    }
  };

  // Handle load more (scroll)
  const handleLoadMore = () => {
    if (pagination.currentPage !== null && !isFetching && !shouldFetch) {
      setPagination(prev => ({
        ...prev,
        currentPage: prev.currentPage ? prev.currentPage + 1 : 0,
        nextItemsToFetch: INITIAL_LOAD_SIZE,
      }));
      setShouldFetch(true);
    }
  };

  return (
    <FancyMultiSelect
      options={availableOptions}
      selected={value}
      onSelect={handleSelect}
      inputValue={searchQuery}
      setInputValue={setSearchQuery}
      placeholder="Select monitors..."
      onLoadMore={handleLoadMore}
      isLoading={isLoading || isFetching}
      nextPage={pagination.currentPage !== null}
    />
  );
};

export default SearchableMonitorSelector;
