export type SaveWorkspaceNameResult = { kind: "unchanged" } | { kind: "renamed"; name: string };

// Renames the workspace when the name differs from the current one, then refreshes /me, which the
// workspace switcher reads the name from. A failed rename throws, for the caller to show. A failed
// refresh doesn't undo the rename: the name is saved, and /me catches up on its next fetch.
export const saveWorkspaceName = async ({
  name,
  current,
  rename,
  refreshMe,
}: {
  name: string;
  current: string;
  rename: (name: string) => Promise<unknown>;
  refreshMe: () => Promise<unknown>;
}): Promise<SaveWorkspaceNameResult> => {
  const trimmed = name.trim();
  if (trimmed === current) return { kind: "unchanged" };
  await rename(trimmed);
  try {
    await refreshMe();
  } catch (error) {
    console.error("Failed to refresh the workspaces after a rename", error);
  }
  return { kind: "renamed", name: trimmed };
};
