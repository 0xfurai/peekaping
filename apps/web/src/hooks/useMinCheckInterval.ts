import { useQuery } from "@tanstack/react-query";
import { getWorkspaceLimitsOptions } from "@/api/@tanstack/react-query.gen";
import { useMe } from "@/hooks/useMe";

// The smallest check interval and retry interval, in seconds, that any install allows
export const ABSOLUTE_MIN_CHECK_INTERVAL = 20;

// The smallest check interval and retry interval a monitor of the workspace can be saved with,
// which the monitor form and Get started use as their minimum and their default: what
// GET /workspace/limits says applies, the stricter of the install's MIN_CHECK_INTERVAL and the
// workspace's limit (PP-160). While the limits load, or when they can't be read, the install's own
// from /me; 20 while that loads too.
export const useMinCheckInterval = (): number => {
  const fromMe = useMe().data?.data?.minCheckInterval;
  const fromLimits = useQuery(getWorkspaceLimitsOptions()).data?.data?.limits.min_check_interval.used;
  return Math.max(fromLimits ?? fromMe ?? ABSOLUTE_MIN_CHECK_INTERVAL, ABSOLUTE_MIN_CHECK_INTERVAL);
};
