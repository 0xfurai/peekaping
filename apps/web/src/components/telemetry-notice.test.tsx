import { describe, expect, it } from "vitest";
import { getErrorReportingQueryKey, getTelemetryQueryKey } from "@/api/@tanstack/react-query.gen";
import { renderAs, text } from "@/test-support/render-with-me";
import { TelemetryNotice } from "./telemetry-notice";

const notice = (access: Parameters<typeof renderAs>[0]) =>
  renderAs(access, <TelemetryNotice />, undefined, (queryClient) => {
    queryClient.setQueryData(getTelemetryQueryKey(), {
      enabled: true,
      forced_off: false,
      notice_pending: true,
      payload: {},
    });
    queryClient.setQueryData(getErrorReportingQueryKey(), { enabled: true, forced_off: false });
  });

describe("TelemetryNotice", () => {
  it("is not shown to a user who isn't an instance admin", () => {
    expect(notice({ instanceAdmin: false, role: "owner" })).not.toContain(text("telemetry_notice.title"));
  });

  it("is shown to an instance admin", () => {
    expect(notice({ instanceAdmin: true, role: "owner" })).toContain(text("telemetry_notice.title"));
  });

  it("links each feature to its card on Admin → Settings, outside every workspace", () => {
    const html = notice({ instanceAdmin: true, role: "owner" });
    expect(html).toContain('href="/admin/settings#usage-statistics"');
    expect(html).toContain('href="/admin/settings#error-reports"');
  });
});
