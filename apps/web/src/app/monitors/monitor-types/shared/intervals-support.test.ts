import { describe, expect, it } from "vitest";
import { intervalMinimumErrors, withMinimumIntervals } from "./intervals-support";
import { INTERVAL_TOO_SHORT } from "./schema";

describe("withMinimumIntervals", () => {
  const defaults = { name: "x", interval: 20, retry_interval: 20, timeout: 16 };

  it("raises the interval and the retry interval to the install's minimum", () => {
    expect(withMinimumIntervals(defaults, 60)).toMatchObject({ interval: 60, retry_interval: 60, timeout: 16 });
  });

  it("leaves the defaults at 20 when the minimum is 20", () => {
    expect(withMinimumIntervals(defaults, 20)).toEqual(defaults);
  });

  it("never lowers a value", () => {
    expect(withMinimumIntervals({ ...defaults, interval: 300 }, 60).interval).toBe(300);
  });
});

describe("intervalMinimumErrors", () => {
  it("flags an interval and a retry interval below the minimum, with the number", () => {
    const errors = intervalMinimumErrors({ interval: 30, retry_interval: 59 }, 60);
    expect(errors.interval).toMatchObject({ message: INTERVAL_TOO_SHORT });
    expect(errors.retry_interval).toMatchObject({ message: INTERVAL_TOO_SHORT });
  });

  it("flags nothing at or above the minimum", () => {
    expect(intervalMinimumErrors({ interval: 60, retry_interval: 61 }, 60)).toEqual({});
  });

  it("reads the form's strings, as the input hands them over", () => {
    const errors = intervalMinimumErrors({ interval: "30", retry_interval: "" }, 60);
    expect(Object.keys(errors).sort()).toEqual(["interval", "retry_interval"]);
  });
});
