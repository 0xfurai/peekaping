import { defineChannelType } from "../definition";
import { lineSchema, lineDefaultValues } from "./schema";
import LineForm from "./form";

export const lineChannelType = defineChannelType({
  type: "line",
  displayName: "LINE messaging",
  schema: lineSchema,
  defaultValues: lineDefaultValues,
  Form: LineForm,
});
