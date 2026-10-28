export type CreateAndEnterResult =
  | { kind: "entered" }
  // The workspace was not made; nothing exists to enter
  | { kind: "create_failed" }
  // The workspace exists, but going to it failed. Retry with its id, which makes no second one.
  | { kind: "enter_failed"; workspaceId: string; error: unknown };

// Makes a workspace and goes to it, or, when a try already made it (`createdId`), only goes to
// it. Making and entering fail separately: a failed entry must not be answered by making another
// workspace. A failed create is reported by `create`'s caller, so it is only named here.
export const createAndEnterWorkspace = async ({
  createdId,
  create,
  enter,
}: {
  createdId: string | null;
  create: () => Promise<string>;
  enter: (workspaceId: string) => Promise<void>;
}): Promise<CreateAndEnterResult> => {
  let workspaceId = createdId;
  if (workspaceId === null) {
    try {
      workspaceId = await create();
    } catch {
      return { kind: "create_failed" };
    }
  }
  try {
    await enter(workspaceId);
    return { kind: "entered" };
  } catch (error) {
    return { kind: "enter_failed", workspaceId, error };
  }
};
