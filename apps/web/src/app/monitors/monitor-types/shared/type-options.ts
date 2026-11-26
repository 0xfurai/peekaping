import { useMemo } from "react";
import { useDisabledMonitorTypes } from "@/hooks/useDisabledMonitorTypes";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useMonitorFormContext } from "../../context/monitor-form-context";
import { selectableMonitorTypes } from "../disabled";
import { monitorTypes } from "../registry";

/**
 * The options of the form's Type select. A new monitor is offered the types the install doesn't
 * turn off. A saved monitor keeps the type it has even when the install turned it off, so that the
 * form still shows it; the server lets the monitor keep that type but not take another turned-off one.
 */
export const useMonitorTypeOptions = (): { value: string; label: string }[] => {
  const { mode, monitor } = useMonitorFormContext();
  const { t } = useLocalizedTranslation();
  const disabledTypes = useDisabledMonitorTypes();
  const savedType = mode === "edit" ? monitor?.data?.type : undefined;

  return useMemo(
    () =>
      selectableMonitorTypes(monitorTypes, { disabled: disabledTypes, keep: savedType }).map((definition) => ({
        value: definition.type,
        label: t(definition.labelKey),
      })),
    [t, disabledTypes, savedType]
  );
};
