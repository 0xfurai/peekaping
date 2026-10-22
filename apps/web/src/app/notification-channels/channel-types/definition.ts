import type { ComponentType } from "react";
import type { z } from "zod";

// A channel type's schema: an object whose `type` literal tells it apart in the form's union.
export type ChannelTypeSchema = z.ZodDiscriminatedUnionOption<"type">;

// Everything the app needs to know about one notification channel type. The registry lists
// these; the channel form and its Type select read them from there.
export interface ChannelTypeDefinition<S extends ChannelTypeSchema = ChannelTypeSchema> {
  // The type as the API stores it, the same literal as the schema's `type` field
  type: z.infer<S>["type"];
  // The name the Type select shows
  displayName: string;
  schema: S;
  defaultValues: z.infer<S>;
  // Cross-field rules the form applies on top of the schema: a refined schema can't be a member
  // of the form's discriminated union
  refine?: (values: z.infer<S>, ctx: z.RefinementCtx) => void;
  // The type's fields. The form element, the name and type fields and the buttons belong to
  // the wrapper (components/create-edit-notification-channel.tsx).
  Form: ComponentType;
}

export const defineChannelType = <S extends ChannelTypeSchema>(
  definition: ChannelTypeDefinition<S>
): ChannelTypeDefinition<S> => definition;
