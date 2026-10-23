import { defineChannelType } from "../definition";
import { signalSchema, signalDefaultValues } from "./schema";
import SignalForm from "./form";

export const signalChannelType = defineChannelType({
  type: "signal",
  displayName: "Signal",
  schema: signalSchema,
  defaultValues: signalDefaultValues,
  Form: SignalForm,
});
