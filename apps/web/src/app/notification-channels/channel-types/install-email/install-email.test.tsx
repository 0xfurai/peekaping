import { describe, expect, it } from "vitest";
import { FormProvider, useForm } from "react-hook-form";
import { getWorkspaceMembersQueryKey } from "@/api/@tanstack/react-query.gen";
import { renderAs, text, USER_EMAIL } from "@/test-support/render-with-me";
import type { WorkspacehttpMemberResponse } from "@/api/types.gen";
import InstallEmailForm from "./form";
import { installEmailChannelType } from "./index";
import { recipientChoices, recipientValue } from "./recipients";

const members: WorkspacehttpMemberResponse[] = [
  { userId: "u2", email: "bob@example.com", role: "member", joinedAt: "2026-10-01T10:00:00Z" },
  { userId: "u1", email: USER_EMAIL, role: "owner", joinedAt: "2026-10-01T09:00:00Z" },
];

function Harness({ to }: { to: string }) {
  const form = useForm({ defaultValues: { type: "install_email", to } });
  return (
    <FormProvider {...form}>
      <InstallEmailForm />
    </FormProvider>
  );
}

const render = (to: string) =>
  renderAs({ instanceAdmin: false, role: "owner" }, <Harness to={to} />, undefined, (queryClient) => {
    queryClient.setQueryData(getWorkspaceMembersQueryKey(), { data: members });
  });

// The address of the option the recipient select has chosen
const selected = (html: string): string | undefined =>
  /<option[^>]*value="([^"]*)"[^>]*selected=""/.exec(html)?.[1] ??
  /<option[^>]*selected=""[^>]*value="([^"]*)"/.exec(html)?.[1];

describe("install email channel form", () => {
  it("starts on the signed-in user's own address", () => {
    const html = render("");
    expect(selected(html)).toBe(USER_EMAIL);
  });

  it("keeps the recipient the channel was saved with", () => {
    const html = render("bob@example.com");
    expect(selected(html)).toBe("bob@example.com");
  });

  it("says that it is sent through this install and once the member has verified the address", () => {
    const html = render("");
    expect(html).toContain(text("notifications.form.install_email.description"));
    expect(html).toContain(text("notifications.form.install_email.to_description"));
  });

  it("is the type the API calls install_email, whose config is the recipient", () => {
    expect(installEmailChannelType.type).toBe("install_email");
    expect(Object.keys(installEmailChannelType.schema.shape).sort()).toEqual(["to", "type"]);
  });
});

describe("recipientChoices", () => {
  it("lists the members with the signed-in user first, once", () => {
    expect(recipientChoices({ members, userEmail: USER_EMAIL, current: USER_EMAIL })).toEqual([
      USER_EMAIL,
      "bob@example.com",
    ]);
  });

  it("keeps a saved recipient who has since left the workspace", () => {
    expect(recipientChoices({ members, userEmail: USER_EMAIL, current: "gone@example.com" })).toEqual([
      USER_EMAIL,
      "bob@example.com",
      "gone@example.com",
    ]);
  });

  it("doesn't list a member twice that differs in case", () => {
    expect(recipientChoices({ members, userEmail: "User@Example.com", current: USER_EMAIL })).toEqual([
      "User@Example.com",
      "bob@example.com",
    ]);
  });
});

describe("recipientValue", () => {
  it("defaults to the signed-in user's address", () => {
    expect(recipientValue("", USER_EMAIL)).toBe(USER_EMAIL);
    expect(recipientValue(undefined, USER_EMAIL)).toBe(USER_EMAIL);
    expect(recipientValue("bob@example.com", USER_EMAIL)).toBe("bob@example.com");
  });
});
