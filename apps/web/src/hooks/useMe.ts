import { useQuery } from "@tanstack/react-query";
import { getMeOptions } from "@/api/@tanstack/react-query.gen";

// The signed-in user and their workspaces. It is the one query that doesn't
// belong to a workspace, so it is the one kept when the workspace changes.
export const useMe = () => useQuery(getMeOptions());
