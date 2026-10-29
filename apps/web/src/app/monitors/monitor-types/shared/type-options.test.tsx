import { describe, expect, it } from "vitest";
import { getMonitorsByIdQueryKey } from "@/api/@tanstack/react-query.gen";
import { renderAs } from "@/test-support/render-with-me";
import { MonitorFormProvider } from "../../context/monitor-form-context";
import { monitorTypes } from "../registry";
import { useMonitorTypeOptions } from "./type-options";

// The values of the Type select's options, as the form offers them
const Options = () => <output>{JSON.stringify(useMonitorTypeOptions().map((option) => option.value))}</output>;

const offered = (html: string): string[] =>
  JSON.parse(html.match(/<output>(.*)<\/output>/)![1].replace(/&quot;/g, '"'));

const ALL = monitorTypes.map((definition) => definition.type);

describe("The monitor types the form offers", () => {
  const create = (disabledMonitorTypes: string[]) =>
    renderAs(
      { instanceAdmin: false, role: "member", disabledMonitorTypes },
      <MonitorFormProvider mode="create">
        <Options />
      </MonitorFormProvider>
    );

  it("are all of them when the install turns none off", () => {
    expect(offered(create([]))).toEqual(ALL);
  });

  it("leave out the types the install turns off, for a new monitor", () => {
    const types = offered(create(["docker", "snmp"]));
    expect(types).toEqual(ALL.filter((type) => type !== "docker" && type !== "snmp"));
  });

  describe("for an edit", () => {
    const saved = (type: string) => ({ id: "m1", name: "Site", type, interval: 60, timeout: 16, config: "{}", notification_ids: [] });
    const edit = (type: string, disabledMonitorTypes: string[]) =>
      renderAs(
        { instanceAdmin: false, role: "member", disabledMonitorTypes },
        <MonitorFormProvider mode="edit" monitorId="m1">
          <Options />
        </MonitorFormProvider>,
        undefined,
        (queryClient) => queryClient.setQueryData(getMonitorsByIdQueryKey({ path: { id: "m1" } }), { data: saved(type) })
      );

    it("keep the monitor's own type when the install turned it off, and hide the other turned-off types", () => {
      const types = offered(edit("docker", ["docker", "snmp"]));
      expect(types).toContain("docker");
      expect(types).not.toContain("snmp");
      expect(types).toContain("http");
    });

    it("hide a turned-off type that is not the monitor's", () => {
      expect(offered(edit("http", ["docker"]))).toEqual(ALL.filter((type) => type !== "docker"));
    });
  });
});
