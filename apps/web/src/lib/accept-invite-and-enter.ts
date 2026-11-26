export type AcceptInviteResult =
  | { kind: "entered" }
  // The invite was not accepted (used, revoked, another email, or the request failed)
  | { kind: "accept_failed"; error: unknown }
  // The invite was accepted, so the user is a member, but going to the workspace failed. Retry with
  // its id: the invite is single-use, so accepting it again answers 410.
  | { kind: "enter_failed"; workspaceId: string; error: unknown };

// Accepts an invite and goes to the workspace it joined, or, when a try already accepted it
// (`acceptedId`), only goes to that workspace. Accepting and entering fail separately: a failed
// entry must not send the used invite again, and must not fall back to some other workspace.
export const acceptInviteAndEnter = async ({
  acceptedId,
  accept,
  enter,
}: {
  acceptedId: string | null;
  accept: () => Promise<string>;
  enter: (workspaceId: string) => Promise<void>;
}): Promise<AcceptInviteResult> => {
  let workspaceId = acceptedId;
  if (workspaceId === null) {
    try {
      workspaceId = await accept();
    } catch (error) {
      return { kind: "accept_failed", error };
    }
  }
  try {
    await enter(workspaceId);
    return { kind: "entered" };
  } catch (error) {
    return { kind: "enter_failed", workspaceId, error };
  }
};
