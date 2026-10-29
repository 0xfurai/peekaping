import { describe, expect, it } from "vitest";
import { AxiosError } from "axios";
import { apiErrorCode } from "./utils";

const failed = (data: unknown) =>
  new AxiosError("Request failed with status code 400", "ERR_BAD_REQUEST", undefined, undefined, {
    status: 400,
    statusText: "Bad Request",
    headers: {},
    config: {} as never,
    data,
  });

describe("apiErrorCode", () => {
  it("reads the code of a structured error body", () => {
    expect(apiErrorCode(failed({ error: { code: "PUSH_TOKEN_EXISTS" } }))).toBe("PUSH_TOKEN_EXISTS");
  });

  it("has none for a body without one", () => {
    expect(apiErrorCode(failed({ message: "bad" }))).toBeUndefined();
    expect(apiErrorCode(failed({ error: { code: 5 } }))).toBeUndefined();
    expect(apiErrorCode(failed(undefined))).toBeUndefined();
  });

  it("has none for an error that is not a response", () => {
    expect(apiErrorCode(new Error("boom"))).toBeUndefined();
    expect(apiErrorCode(undefined)).toBeUndefined();
  });
});
