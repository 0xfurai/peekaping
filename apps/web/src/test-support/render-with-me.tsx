import type { ReactElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { MemoryRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { getMeQueryKey } from "@/api/@tanstack/react-query.gen";
import "@/i18n";
import enUS from "@/i18n/locales/en-US.json";

export const WORKSPACE_ID = "11111111-1111-4111-8111-111111111111";

export type Access = {
  instanceAdmin: boolean;
  role: "member" | "admin" | "owner";
  // Workspaces the user belongs to besides the one in the address
  otherWorkspaces?: { id: string; name: string; role: "member" | "admin" | "owner" }[];
  // The monitor types the install turns off, as /me lists them
  disabledMonitorTypes?: string[];
  // The smallest check interval the install allows, as /me states it. Left out, /me carries 20.
  minCheckInterval?: number;
  // Whether the install verifies custom domains, as /me states it. Left out, /me carries false.
  verifyCustomDomains?: boolean;
};

export const USER_EMAIL = "user@example.com";

// The English text of a key, as the page shows it
export const text = (key: string): string => {
  const value = key
    .split(".")
    .reduce<unknown>(
      (node, part) => (node as Record<string, unknown>)?.[part],
      enUS
    );
  return value as string;
};

// A page as the signed-in user sees it: /me already loaded for the workspace in the address,
// as the workspace layout leaves it. Static markup runs no effects, so nothing is fetched.
export const renderAs = (
  access: Access,
  ui: ReactElement,
  path = `/w/${WORKSPACE_ID}/monitors`,
  seed?: (queryClient: QueryClient) => void
): string => {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  queryClient.setQueryData(getMeQueryKey(), {
    data: {
      user: { id: "u1", email: USER_EMAIL, instanceAdmin: access.instanceAdmin },
      workspaces: [{ id: WORKSPACE_ID, name: "Main", role: access.role }, ...(access.otherWorkspaces ?? [])],
      defaultWorkspaceId: WORKSPACE_ID,
      disabledMonitorTypes: access.disabledMonitorTypes ?? [],
      minCheckInterval: access.minCheckInterval ?? 20,
      verifyCustomDomains: access.verifyCustomDomains ?? false,
    },
  });
  seed?.(queryClient);
  return renderToStaticMarkup(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[path]}>{ui}</MemoryRouter>
    </QueryClientProvider>
  );
};
