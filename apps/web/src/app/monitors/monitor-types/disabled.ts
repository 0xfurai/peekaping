import type { TFunction } from "i18next";
import { getMonitorType } from "./registry";

/**
 * The monitor types a form offers. An install can turn types off (DISABLED_MONITOR_TYPES), and the
 * server refuses to create a monitor of one or to change a monitor's type to one. `keep` is the type
 * a saved monitor opened with, which stays so that the form still shows it.
 */
export const selectableMonitorTypes = <Definition extends { type: string }>(
  definitions: readonly Definition[],
  { disabled, keep }: { disabled: readonly string[]; keep?: string }
): Definition[] =>
  definitions.filter((definition) => !disabled.includes(definition.type) || definition.type === keep);

/**
 * Why a monitor of a turned-off type doesn't run, named by the type's label in the Type select.
 * A type the app doesn't know (a newer server's) is named as the server gave it.
 */
export const monitorTypeDisabledReason = (t: TFunction, type: string): string => {
  const definition = getMonitorType(type);
  return t("monitors.type_disabled", { type: definition ? t(definition.labelKey) : type });
};
