import type { ComponentType } from "react";
import type { z } from "zod";
import type { MonitorCreateUpdateDto, MonitorMonitorResponseDto } from "@/api";
import type { MonitorConfig, MonitorTarget } from "./monitor-target";

// A monitor type's schema: an object whose `type` literal tells it apart in the form's union.
export type MonitorTypeSchema = z.ZodDiscriminatedUnionOption<"type">;

// Everything the app needs to know about one monitor type. The registry lists these; the form,
// the Type select, cloning and the monitor page header read them from there.
export interface MonitorTypeDefinition<S extends MonitorTypeSchema = MonitorTypeSchema> {
  // The type as the API stores it, the same literal as the schema's `type` field
  type: z.infer<S>["type"];
  // The i18n key of the name the Type select shows
  labelKey: string;
  schema: S;
  defaultValues: z.infer<S>;
  // The form values of a saved monitor
  deserialize: (monitor: MonitorMonitorResponseDto) => z.infer<S>;
  // The request body for form values
  serialize: (values: z.infer<S>) => MonitorCreateUpdateDto;
  // The target the monitor page header shows. Unset for types whose target is not safe to show,
  // such as a connection string with credentials or a push token.
  formatTarget?: (config: MonitorConfig) => MonitorTarget | null;
  // The type's fields. The form element, the submit button and the type switch belong to the
  // wrapper (components/create-edit-form.tsx), not to the type.
  Form: ComponentType;
}

export const defineMonitorType = <S extends MonitorTypeSchema>(
  definition: MonitorTypeDefinition<S>
): MonitorTypeDefinition<S> => definition;
