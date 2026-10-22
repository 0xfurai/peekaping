import { z } from "zod";
import { monitorBaseSchema, monitorBaseDefaultValues } from "../shared/schema";

export const pushSchema = z
  .object({
    type: z.literal("push"),
    pushToken: z.string(),
  })
  .merge(monitorBaseSchema);

export type PushFormValues = z.infer<typeof pushSchema>;

export const pushDefaultValues: PushFormValues = {
  type: "push",
  pushToken: "",
  ...monitorBaseDefaultValues,
};
