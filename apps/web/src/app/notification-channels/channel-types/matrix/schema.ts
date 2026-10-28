import { z } from "zod";
import { withSavedSecret } from "../../secrets";

export const matrixSchema = z.object({
  type: z.literal("matrix"),
  homeserver_url: withSavedSecret(z.string().url({ message: "Valid homeserver URL is required" })),
  internal_room_id: z.string().min(1, { message: "Internal Room ID is required" }),
  access_token: withSavedSecret(z.string().min(1, { message: "Access Token is required" })),
  custom_message: z.string().optional(),
});

export type MatrixFormValues = z.infer<typeof matrixSchema>;

export const matrixDefaultValues: MatrixFormValues = {
  type: "matrix" as const,
  homeserver_url: "",
  internal_room_id: "",
  access_token: "",
  custom_message: "{{ msg }}",
};
