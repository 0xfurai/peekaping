import { describe, expect, it } from "vitest";
import { AxiosError, type AxiosResponse } from "axios";
import { accountEmailFailure, isSuspendedUnverified, needsVerification } from "./account-email";

const failure = (status: number, message: string) =>
  new AxiosError("failed", "ERR_BAD_REQUEST", undefined, undefined, {
    status,
    data: { message },
  } as AxiosResponse);

describe("accountEmailFailure", () => {
  it.each([
    [400, "This link is invalid or has expired. Ask for a new one.", "invalid_link"],
    [400, "Password must be at least 8 characters", "error"],
    [429, "Too many requests", "rate_limited"],
    [409, "Email already verified", "already_verified"],
    [404, "Not found", "error"],
    [500, "Internal server error", "error"],
  ])("%i %j is %s", (status, message, expected) => {
    expect(accountEmailFailure(failure(status, message))).toBe(expected);
  });

  it("is a plain error for a failure without a response", () => {
    expect(accountEmailFailure(new Error("network"))).toBe("error");
    expect(accountEmailFailure(undefined)).toBe("error");
  });
});

describe("needsVerification", () => {
  const me = (accountEmails: boolean, emailVerified: boolean) => ({
    accountEmails,
    user: { emailVerified },
  });

  it("is true only for an install that sends account emails and an address not verified", () => {
    expect(needsVerification(me(true, false))).toBe(true);
    expect(needsVerification(me(true, true))).toBe(false);
    expect(needsVerification(me(false, false))).toBe(false);
    expect(needsVerification(me(false, true))).toBe(false);
  });

  it("is false while /me is not loaded", () => {
    expect(needsVerification(undefined)).toBe(false);
  });
});

describe("isSuspendedUnverified", () => {
  const ID = "11111111-1111-4111-8111-111111111111";
  const workspace = (suspended: boolean, suspendedReason: string | null) => [{ id: ID, suspended, suspendedReason }];

  it("is true for the workspace the address names when it is suspended as unverified", () => {
    expect(isSuspendedUnverified(workspace(true, "unverified"), ID)).toBe(true);
    expect(isSuspendedUnverified(workspace(true, "unverified"), ID.toUpperCase())).toBe(true);
  });

  it("is false for a running workspace, another reason, another workspace or no workspace", () => {
    expect(isSuspendedUnverified(workspace(false, null), ID)).toBe(false);
    expect(isSuspendedUnverified(workspace(true, "other"), ID)).toBe(false);
    expect(isSuspendedUnverified(workspace(true, "unverified"), "22222222-2222-4222-8222-222222222222")).toBe(false);
    expect(isSuspendedUnverified(workspace(true, "unverified"), null)).toBe(false);
    expect(isSuspendedUnverified(null, ID)).toBe(false);
  });
});
