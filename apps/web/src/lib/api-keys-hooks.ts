// React Query hooks for API Keys
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createAPIKey,
  getAPIKeys,
  getAPIKey,
  updateAPIKey,
  deleteAPIKey,
} from "./api-keys";
import type { UpdateAPIKeyRequest } from "./api-keys";

// Query keys
export const apiKeysQueryKey = ["api-keys"] as const;
export const apiKeyQueryKey = (id: string) => ["api-keys", id] as const;

// Hooks
export const useAPIKeys = () => {
  return useQuery({
    queryKey: apiKeysQueryKey,
    queryFn: getAPIKeys,
  });
};

export const useAPIKey = (id: string) => {
  return useQuery({
    queryKey: apiKeyQueryKey(id),
    queryFn: () => getAPIKey(id),
    enabled: !!id,
  });
};

export const useCreateAPIKey = () => {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: createAPIKey,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: apiKeysQueryKey });
    },
  });
};

export const useUpdateAPIKey = () => {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ id, data }: { id: string; data: UpdateAPIKeyRequest }) =>
      updateAPIKey(id, data),
    onSuccess: (_, { id }) => {
      queryClient.invalidateQueries({ queryKey: apiKeysQueryKey });
      queryClient.invalidateQueries({ queryKey: apiKeyQueryKey(id) });
    },
  });
};

export const useDeleteAPIKey = () => {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: deleteAPIKey,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: apiKeysQueryKey });
    },
  });
};
