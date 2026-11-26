import { defineChannelType } from "../definition";
import { installEmailSchema, installEmailDefaultValues } from "./schema";
import InstallEmailForm from "./form";

// Sent through the install's sender for account emails (the SMTP channel, with a server of its
// own, is the "smtp" type). Offered only when the install has that sender.
export const installEmailChannelType = defineChannelType({
  type: "install_email",
  displayName: "Email",
  schema: installEmailSchema,
  defaultValues: installEmailDefaultValues,
  Form: InstallEmailForm,
});
