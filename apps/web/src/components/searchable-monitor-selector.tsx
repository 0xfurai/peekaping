import { getMonitorsOptions } from "@/api/@tanstack/react-query.gen";
import { FancyMultiSelect, type Option } from "./multiselect-3";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useDebounce } from "@/hooks/useDebounce";

const SearchableMonitorSelector = ({
  value,
  onSelect,
}: {
  value: Option[];
  onSelect: (value: Option[]) => void;
}) => {
  const [searchQuery, setSearchQuery] = useState("");
  const debouncedSearch = useDebounce(searchQuery, 300);

  // Fetch monitors using TanStack Query
  const { data: monitorsData } = useQuery({
    ...getMonitorsOptions({
      query: {
        limit: 20,
        q: debouncedSearch,
      },
    }),
  });

  const monitorOptions =
    monitorsData?.data?.map((monitor) => ({
      label: monitor.name || "Unnamed Monitor",
      value: monitor.id || "",
    })) || [];

  return (
    <FancyMultiSelect
      options={monitorOptions}
      selected={value}
      onSelect={onSelect}
      inputValue={searchQuery}
      setInputValue={setSearchQuery}
      placeholder="Select monitors..."
    />
  );
};

export default SearchableMonitorSelector;
