import { defineChannelType } from "../definition";
import { googleChatSchema, googleChatDefaultValues } from "./schema";
import GoogleChatForm from "./form";

export const googleChatChannelType = defineChannelType({
  type: "google_chat",
  displayName: "Google Chat",
  schema: googleChatSchema,
  defaultValues: googleChatDefaultValues,
  Form: GoogleChatForm,
});
