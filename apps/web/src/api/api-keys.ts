// Manual API client for API Keys
// This will be replaced when the swagger generation is fixed

import { client } from "./client.gen";

export interface CreateAPIKeyRequest {
  name: string;
  expires_at?: string;
  max_usage_count?: number;
}

export interface APIKeyResponse {
  id: string;
  name: string;
  display_key: string;
  last_used: string | null;
  expires_at: string | null;
  usage_count: number;
  max_usage_count: number | null;
  created_at: string;
  updated_at: string;
}

export interface APIKeyWithTokenResponse extends APIKeyResponse {
  token: string;
}

export interface UpdateAPIKeyRequest {
  name?: string;
  expires_at?: string | null;
  max_usage_count?: number | null;
}

// API Key functions
export const createAPIKey = async (
  data: CreateAPIKeyRequest
): Promise<APIKeyWithTokenResponse> => {
  const response = await client.post({
    url: "/api-keys",
    body: data,
  });
  return (response.data as any).data;
};

export const getAPIKeys = async (): Promise<APIKeyResponse[]> => {
  const response = await client.get({
    url: "/api-keys",
  });
  return (response.data as any).data;
};

export const getAPIKey = async (id: string): Promise<APIKeyResponse> => {
  const response = await client.get({
    url: "/api-keys/{id}",
    path: { id },
  });
  return (response.data as any).data;
};

export const updateAPIKey = async (
  id: string,
  data: UpdateAPIKeyRequest
): Promise<APIKeyResponse> => {
  const response = await client.put({
    url: "/api-keys/{id}",
    path: { id },
    body: data,
  });
  return (response.data as any).data;
};

export const deleteAPIKey = async (id: string): Promise<void> => {
  await client.delete({
    url: "/api-keys/{id}",
    path: { id },
  });
};
