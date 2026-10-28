import type { WorkspacehttpMemberResponse } from "@/api/types.gen";

// The addresses the recipient select offers: the workspace's members, the signed-in user first.
// A recipient the list lacks, such as a member who has left since the channel was saved, stays
// in it so that the select shows what is stored.
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
  const known = (email: string) => choices.some((c) => c.toLowerCase() === email.toLowerCase());
  return current && !known(current) ? [...choices, current] : choices;
};

// The recipient the form shows and sends: the one chosen, else the signed-in user's own address
export const recipientValue = (current: string | undefined, userEmail: string | undefined): string =>
  current || userEmail || "";
