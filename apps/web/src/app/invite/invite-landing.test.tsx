import { describe, expect, it } from "vitest";
import { getInvitesByTokenQueryKey } from "@/api/@tanstack/react-query.gen";
import { renderAs, text, USER_EMAIL } from "@/test-support/render-with-me";
import { InviteLandingView } from "./invite-landing";

const TOKEN = "tok_abc123";

const preview = (overrides: Record<string, unknown> = {}) => ({
  data: {
    workspaceName: "Acme",
    email: USER_EMAIL,
    role: "admin",
    status: "pending",
    valid: true,
    ...overrides,
  },
});

// The landing page for the invite, with the preview fetched, as a signed-in user or a visitor
const landing = (signedIn: boolean, overrides?: Record<string, unknown>) => {
  return renderAs(
    { instanceAdmin: false, role: "owner" },
    <InviteLandingView token={TOKEN} signedIn={signedIn} />,
    `/invite/${TOKEN}`,
    (queryClient) => {
      queryClient.setQueryData(getInvitesByTokenQueryKey({ path: { token: TOKEN } }), preview(overrides));
    }
  );
};

describe("InviteLanding", () => {
  it("offers Accept to a signed-in user whose email is the invited one, in any letter case", () => {
    const html = landing(true, { email: USER_EMAIL.toUpperCase() });
    expect(html).toContain("Acme");
    expect(html).toContain(`>${text("invite.accept")}<`);
    expect(html).not.toContain('type="password"');
    expect(html).not.toContain(text("invite.sign_out"));
  });

  it("tells a signed-in user with another email so, and offers to sign out instead of Accept", () => {
    const html = landing(true, { email: "other@example.com" });
    expect(html).toContain("other@example.com");
    expect(html).toContain(USER_EMAIL);
    expect(html).toContain(`>${text("invite.sign_out")}<`);
    expect(html).not.toContain(`>${text("invite.accept")}<`);
    expect(html).not.toContain('type="password"');
  });

  it("gives a visitor a signup form with the invited email filled in and locked, and a way to sign in instead", () => {
    const html = landing(false, { email: "newbie@example.com" });
    expect(html).toMatch(/<input[^>]*readOnly=""[^>]*value="newbie@example.com"|<input[^>]*value="newbie@example.com"[^>]*readOnly=""/);
    expect(html).toContain('type="password"');
    expect(html).toContain(`>${text("invite.signup_submit")}<`);
    expect(html).toContain(`href="/login?next=%2Finvite%2F${TOKEN}"`);
    expect(html).toContain(text("invite.sign_in_instead"));
    expect(html).not.toContain(`>${text("invite.accept")}<`);
  });

  it.each([
    ["accepted", "invite.unavailable.accepted"],
    ["expired", "invite.unavailable.expired"],
    ["revoked", "invite.unavailable.revoked"],
  ])("says a %s invite can't be used and offers nothing else", (status, key) => {
    for (const signedIn of [true, false]) {
      const html = landing(signedIn, { valid: false, status });
      expect(html).toContain(text(key));
      expect(html).not.toContain(`>${text("invite.accept")}<`);
      expect(html).not.toContain('type="password"');
    }
  });
});
