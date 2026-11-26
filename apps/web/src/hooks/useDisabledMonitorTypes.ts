import { useMe } from "@/hooks/useMe";

const NONE: string[] = [];

// The monitor types this install turns off (DISABLED_MONITOR_TYPES). Empty while /me loads, so
// nothing is hidden before the answer is known.
export const useDisabledMonitorTypes = (): string[] =>
  useMe().data?.data?.disabledMonitorTypes ?? NONE;
