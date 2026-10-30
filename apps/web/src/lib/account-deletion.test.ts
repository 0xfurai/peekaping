import { describe, expect, it } from "vitest";
import { AxiosError, AxiosHeaders } from "axios";
import { accountDeletionError } from "./account-deletion";

const refusalWith = (status: number, data: unknown) => {
  const config = { headers: new AxiosHeaders() };
  return new AxiosError("Request failed", "ERR_BAD_REQUEST", config, null, {
    status,
    statusText: "",
    headers: {},
    config,
    data,
  });
};

// Shows the key and its variables, so that a test reads which text the page would show
const t = (key: string, vars?: Record<string, unknown>) => (vars ? `${key} ${JSON.stringify(vars)}` : key);
const FALLBACK = "fallback";

describe("accountDeletionError", () => {
  it("names the workspaces the user is the only owner of, in quotes", () => {
    const error = refusalWith(409, {
      error: {
        code: "LAST_OWNER",
        workspaces: [
          { id: "w1", name: "Main" },
          { id: "w2", name: "Side project" },
        ],
      },
    });
    expect(accountDeletionError(error, t, FALLBACK)).toBe(
      'security.delete_account.errors.last_owner {"workspaces":"\\"Main\\", \\"Side project\\""}'
    );
  });

  it("asks for another instance admin when the user is the last one", () => {
    const error = refusalWith(409, { message: "The install needs at least one instance admin" });
    expect(accountDeletionError(error, t, FALLBACK)).toBe("security.delete_account.errors.last_instance_admin");
  });

  it("says the password is wrong for the server's 400", () => {
    const error = refusalWith(400, { message: "current password is incorrect" });
    expect(accountDeletionError(error, t, FALLBACK)).toBe("security.delete_account.errors.wrong_password");
  });

  it("shows the server's own message for any other refusal, else the fallback", () => {
    expect(accountDeletionError(refusalWith(500, { message: "Internal server error" }), t, FALLBACK)).toBe(
      "Internal server error"
    );
    expect(accountDeletionError(refusalWith(409, {}), t, FALLBACK)).toBe(FALLBACK);
    expect(accountDeletionError(new Error("offline"), t, FALLBACK)).toBe(FALLBACK);
  });
});
