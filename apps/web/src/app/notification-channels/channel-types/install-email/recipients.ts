import type { WorkspacehttpMemberResponse } from "@/api/types.gen";

// The addresses the recipient select offers: the workspace's members, the signed-in user first.
// A recipient the list lacks, such as a member who has left since the channel was saved, stays
// in it so that the select shows what is stored. A recipient saved in another case than its
// member's email is that member, and is listed in their place as it is stored, so that the select
// shows the address the form saves and tests.
export const recipientChoices = ({
  members,
  userEmail,
  current,
}: {
  members: Pick<WorkspacehttpMemberResponse, "email">[];
  userEmail: string | undefined;
  current: string | undefined;
}): string[] => {
  const emails = members.map((member) => member.email);
  const rest = emails.filter((email) => email.toLowerCase() !== userEmail?.toLowerCase());
  const choices = userEmail ? [userEmail, ...rest] : rest;
  if (!current) return choices;
  const same = choices.findIndex((email) => email.toLowerCase() === current.toLowerCase());
  if (same === -1) return [...choices, current];
  return choices.map((email, i) => (i === same ? current : email));
};

// The recipient the form shows and sends: the one chosen, else the signed-in user's own address
export const recipientValue = (current: string | undefined, userEmail: string | undefined): string =>
  current || userEmail || "";
