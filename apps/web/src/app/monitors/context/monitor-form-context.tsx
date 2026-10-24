import React, {
  createContext,
  useContext,
  useMemo,
  useState,
  useEffect,
} from "react";
import { useForm, type UseFormReturn } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import {
  useMutation,
  useQueryClient,
  type UseMutationResult,
  useQuery,
} from "@tanstack/react-query";
import {
  getMonitorsInfiniteQueryKey,
  getMonitorsQueryKey,
  postMonitorsMutation,
  putMonitorsByIdMutation,
  getMonitorsByIdOptions,
  getMonitorsByIdQueryKey,
} from "@/api/@tanstack/react-query.gen";
import { toast } from "sonner";
import { useNavigate } from "react-router-dom";
import { AxiosError } from "axios";
import type {
  Options,
  PostMonitorsData,
  HttpxErrorBody,
  PutMonitorsByIdResponse,
  PutMonitorsByIdError,
  PutMonitorsByIdData,
} from "@/api";
import type { UtilsApiResponseMonitorMonitorResponseDto } from "@/api/types.gen";
import { commonMutationErrorHandler } from "@/lib/utils";
import {
  deserializeMonitor,
  checkedMonitorFormSchema,
  monitorTypes,
  type MonitorFormValues,
} from "../monitor-types/registry";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useProbes } from "@/hooks/useProbes";
import { probeTypeError, withEffectiveProbes } from "../monitor-types/shared/probe-support";

type Mode = "create" | "edit";

interface MonitorFormContextType {
  form: UseFormReturn<MonitorFormValues>;
  notifierSheetOpen: boolean;
  setNotifierSheetOpen: React.Dispatch<React.SetStateAction<boolean>>;
  proxySheetOpen: boolean;
  setProxySheetOpen: React.Dispatch<React.SetStateAction<boolean>>;
  monitor?: UtilsApiResponseMonitorMonitorResponseDto;
  mode: Mode;
  isPending: boolean;
  createMonitorMutation: UseMutationResult<
    UtilsApiResponseMonitorMonitorResponseDto,
    AxiosError<HttpxErrorBody, unknown>,
    Options<PostMonitorsData>,
    unknown
  >;
  editMonitorMutation: UseMutationResult<
    PutMonitorsByIdResponse,
    AxiosError<PutMonitorsByIdError>,
    Options<PutMonitorsByIdData>,
    unknown
  >;
  monitorId?: string;
}

const MonitorFormContext = createContext<MonitorFormContextType | undefined>(
  undefined
);

interface MonitorFormProviderProps {
  children: React.ReactNode;
  mode: Mode;
  monitorId?: string;
  initialValues?: MonitorFormValues;
}

export const MonitorFormProvider: React.FC<MonitorFormProviderProps> = ({
  children,
  mode,
  monitorId,
  initialValues,
}) => {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { t } = useLocalizedTranslation();
  const [notifierSheetOpen, setNotifierSheetOpen] = useState(false);
  const [proxySheetOpen, setProxySheetOpen] = useState(false);

  // Only fetch monitor in edit mode
  const { data: monitor } = useQuery({
    ...getMonitorsByIdOptions({ path: { id: monitorId! } }),
    enabled: mode === "edit" && !!monitorId,
  });

  const { probes } = useProbes();

  const form = useForm<MonitorFormValues>({
    // A new monitor starts as the first type the registry lists. Read here, not at module
    // level: the type forms import this module, so the registry is still loading then.
    defaultValues: initialValues ?? monitorTypes[0].defaultValues,
    // The schema, and a check of the probe that it can't make: which probes run which types
    // comes from the probes query
    resolver: async (values, context, options) => {
      // A push monitor is checked and submitted on the local probe, as its probe field shows it
      // (the form keeps the earlier selection for when the type changes back)
      const effective = withEffectiveProbes(values);
      const result = await zodResolver(checkedMonitorFormSchema)(effective, context, options);
      const message = probeTypeError(effective, probes, monitor?.data);
      if (!message) return result;
      return { values: {}, errors: { ...result.errors, probe_ids: { type: "probe", message } } };
    },
  });

  // In edit mode the form shows the saved monitor once it is loaded
  useEffect(() => {
    if (mode !== "edit" || !monitor?.data) return;
    try {
      form.reset(deserializeMonitor(monitor.data));
    } catch (error) {
      console.error("Failed to deserialize monitor data:", error);
      toast.error("Failed to load monitor data");
    }
  }, [mode, monitor, form]);

  const createMonitorMutation = useMutation({
    ...postMonitorsMutation(),
    onSuccess: (res) => {
      toast.success("Monitor created successfully");
      queryClient.invalidateQueries({
        queryKey: getMonitorsInfiniteQueryKey(),
      });
      queryClient.invalidateQueries({
        queryKey: getMonitorsQueryKey(),
      });
      navigate(`/monitors/${res.data.id}`);
    },
    onError: commonMutationErrorHandler("Failed to create monitor"),
  });

  const editMonitorMutation = useMutation({
    ...putMonitorsByIdMutation({
      path: {
        id: monitorId!,
      },
    }),
    onSuccess: () => {
      toast.success("Monitor updated successfully");
      queryClient.invalidateQueries({
        queryKey: getMonitorsInfiniteQueryKey(),
      });
      queryClient.removeQueries({
        queryKey: getMonitorsByIdQueryKey({ path: { id: monitorId! } }),
      });

      navigate(`/monitors/${monitorId}`);
    },
    onError: commonMutationErrorHandler("Failed to update monitor"),
  });

  const value = useMemo(
    () => ({
      form,
      notifierSheetOpen,
      setNotifierSheetOpen,
      proxySheetOpen,
      setProxySheetOpen,
      monitor,
      mode,
      isPending:
        mode === "create"
          ? createMonitorMutation.isPending
          : editMonitorMutation.isPending,
      createMonitorMutation,
      editMonitorMutation,
      monitorId,
    }),
    [
      form,
      createMonitorMutation,
      editMonitorMutation,
      notifierSheetOpen,
      proxySheetOpen,
      monitor,
      mode,
      monitorId,
    ]
  );

  if (mode === "edit" && !monitorId) {
    throw new Error("Monitor ID is required in edit mode");
  }

  // For edit mode, don't render children until monitor data is available
  if (mode === "edit" && !monitor) {
    return (
      <MonitorFormContext.Provider value={value}>
        <div>{t("common.loading")}</div>
      </MonitorFormContext.Provider>
    );
  }

  return (
    <MonitorFormContext.Provider value={value}>
      {children}
    </MonitorFormContext.Provider>
  );
};

export const useMonitorFormContext = () => {
  const ctx = useContext(MonitorFormContext);
  if (!ctx) {
    throw new Error(
      "useMonitorFormContext must be used within a MonitorFormProvider"
    );
  }
  return ctx;
};
