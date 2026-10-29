import { useMe } from "@/hooks/useMe";

// The smallest check interval and retry interval, in seconds, that any install allows
export const ABSOLUTE_MIN_CHECK_INTERVAL = 20;

// The smallest check interval and retry interval this install lets a monitor be saved with
// (MIN_CHECK_INTERVAL), which the monitor form uses as its minimum and its default. 20 while /me
// loads.
export const useMinCheckInterval = (): number =>
  Math.max(useMe().data?.data?.minCheckInterval ?? ABSOLUTE_MIN_CHECK_INTERVAL, ABSOLUTE_MIN_CHECK_INTERVAL);
