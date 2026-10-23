import { defineMonitorType } from "../definition";
import { tcpSchema, tcpDefaultValues } from "./schema";
import { deserialize, serialize, formatTarget } from "./codec";
import TcpForm from "./form";

export const tcpMonitorType = defineMonitorType({
  type: "tcp",
  labelKey: "monitors.form.type.tcp",
  schema: tcpSchema,
  defaultValues: tcpDefaultValues,
  deserialize,
  serialize,
  formatTarget,
  Form: TcpForm,
});
