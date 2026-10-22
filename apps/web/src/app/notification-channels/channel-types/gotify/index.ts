import { defineChannelType } from "../definition";
import { gotifySchema, gotifyDefaultValues } from "./schema";
import GotifyForm from "./form";

export const gotifyChannelType = defineChannelType({
  type: "gotify",
  displayName: "Gotify",
  schema: gotifySchema,
  defaultValues: gotifyDefaultValues,
  Form: GotifyForm,
});
