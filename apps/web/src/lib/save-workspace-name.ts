import type { UtilsApiResponseAuthMeResponse } from "@/api/types.gen";

export type RenamedWorkspace = { id: string; name: string };

export type SaveWorkspaceNameResult = { kind: "unchanged" } | { kind: "renamed"; name: string };

// Renames the workspace when the name differs from the current one, remembers the name the
// rename answers with (in the cached /me, which the workspace switcher and the current name come
// from), then refreshes /me. A failed rename throws, for the caller to show. A failed refresh
// doesn't undo the rename: the name is saved and remembered, and /me catches up on its next fetch.
export const saveWorkspaceName = async ({
  name,
  current,
  rename,
  remember,
  refreshMe,
}: {
  name: string;
  current: string;
  // Resolves to the renamed workspace, as the rename's answer has it
  rename: (name: string) => Promise<RenamedWorkspace>;
  remember: (workspace: RenamedWorkspace) => void;
  refreshMe: () => Promise<unknown>;
}): Promise<SaveWorkspaceNameResult> => {
  const trimmed = name.trim();
  if (trimmed === current) return { kind: "unchanged" };
  const saved = await rename(trimmed);
  remember(saved);
  try {
    await refreshMe();
  } catch (error) {
    console.error("Failed to refresh the workspaces after a rename", error);
  }
  return { kind: "renamed", name: saved.name };
};

// me with the workspace renamed; a /me that isn't loaded stays as it is
export const meWithWorkspaceName = (
  me: UtilsApiResponseAuthMeResponse | undefined,
  { id, name }: RenamedWorkspace
): UtilsApiResponseAuthMeResponse | undefined =>
  me && {
    ...me,
    data: {
      ...me.data,
      workspaces:
        me.data.workspaces?.map((w) => (w.id.toLowerCase() === id.toLowerCase() ? { ...w, name } : w)) ?? null,
    },
  };
