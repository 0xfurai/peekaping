import { defineChannelType } from "../definition";
import { ntfySchema, ntfyDefaultValues } from "./schema";
import NtfyForm from "./form";

export const ntfyChannelType = defineChannelType({
  type: "ntfy",
  displayName: "NTFY",
  schema: ntfySchema,
  defaultValues: ntfyDefaultValues,
  Form: NtfyForm,
});
