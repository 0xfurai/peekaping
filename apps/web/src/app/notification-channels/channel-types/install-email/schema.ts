import { z } from "zod";

// The recipient is a member of the workspace: the server checks that, and that they have
// verified the address when it sends.
export const installEmailSchema = z.object({
  type: z.literal("install_email"),
  to: z.string().trim().email({ message: "Recipient is required" }),
});

export type InstallEmailFormValues = z.infer<typeof installEmailSchema>;

// The recipient starts empty; the form fills in the signed-in user's address
export const installEmailDefaultValues: InstallEmailFormValues = {
  type: "install_email",
  to: "",
};
