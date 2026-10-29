import React, {
  createContext,
  useContext,
  useMemo,
  useRef,
  useState,
  useEffect,
} from "react";
import { useForm, type UseFormReturn } from "react-hook-form";
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
  PostMonitorsError,
  PutMonitorsByIdResponse,
  PutMonitorsByIdError,
  PutMonitorsByIdData,
} from "@/api";
import type { UtilsApiResponseMonitorMonitorResponseDto } from "@/api/types.gen";
import { apiErrorCode, commonMutationErrorHandler } from "@/lib/utils";
import {
  deserializeMonitor,
  monitorTypes,
  type MonitorFormValues,
} from "../monitor-types/registry";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useProbes } from "@/hooks/useProbes";
import { useCurrentAccess } from "@/hooks/useCurrentAccess";
import { checkMonitorValues } from "../monitor-types/shared/check-values";
import { allNotificationChannelsOptions, defaultChannelIds } from "../monitor-types/shared/channel-support";
import { useWorkspacePath } from "@/hooks/useWorkspacePath";
import { useDisabledMonitorTypes } from "@/hooks/useDisabledMonitorTypes";
import { selectableMonitorTypes } from "../monitor-types/disabled";

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
    AxiosError<PostMonitorsError, unknown>,
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
  // A new monitor that isn't a clone starts on the default probes (every enabled probe that runs
  // its type). The user's own choice ends that, so it is kept here: the type forms, and the probe
  // field with them, mount again when the type changes.
  presetsProbes: boolean;
  probesChosenByHand: React.MutableRefObject<boolean>;
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
  const workspacePath = useWorkspacePath();
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
  const { instanceAdmin } = useCurrentAccess();
  const probesChosenByHand = useRef(false);

  // A new monitor that isn't a clone starts with the workspace's default channels ticked: from the
  // channels already loaded, and again from the answer to this page's own request, since the loaded
  // ones may be old (see below).
  const presetsChannels = mode === "create" && !initialValues;
  const { data: channels, isFetchedAfterMount: channelsFetched } = useQuery({
    ...allNotificationChannelsOptions(),
    enabled: presetsChannels,
  });
  const startingChannelIds = useRef(defaultChannelIds(channels?.data));
  const channelsPreset = useRef(false);

  // A new monitor starts as the first type the registry lists that the install doesn't turn off.
  // Read here, not at module level: the type forms import this module, so the registry is still
  // loading then.
  const disabledTypes = useDisabledMonitorTypes();
  const startingType = selectableMonitorTypes(monitorTypes, { disabled: disabledTypes })[0] ?? monitorTypes[0];

  const form = useForm<MonitorFormValues>({
    defaultValues:
      initialValues ??
      (channels?.data
        ? { ...startingType.defaultValues, notification_ids: startingChannelIds.current }
        : startingType.defaultValues),
    // The schema, and a check of the probe that it can't make: which probes run which types
    // comes from the probes query
    resolver: (values, context, options) =>
      checkMonitorValues(values, context, options, {
        probes,
        saved: monitor?.data,
        // Someone who isn't an instance admin may leave a new monitor on no probe
        mayUseDefaultProbes: !instanceAdmin && mode === "create",
      }),
  });

  // The default channels are ticked once, from this page's own answer. Not over a choice of the
  // user's: the channels they changed, or one they just made, stay.
  useEffect(() => {
    if (!presetsChannels || channelsPreset.current || !channelsFetched || !channels?.data) return;
    channelsPreset.current = true;
    const current = form.getValues("notification_ids");
    const untouched =
      !form.getFieldState("notification_ids").isDirty &&
      current.length === startingChannelIds.current.length &&
      current.every((id) => startingChannelIds.current.includes(id));
    if (untouched) form.setValue("notification_ids", defaultChannelIds(channels.data));
  }, [presetsChannels, channelsFetched, channels, form]);

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

  // Another monitor holding the push token has a message of its own, since the response carries
  // only its code
  const handleMutationError = (fallbackMessage: string) => (error: unknown) => {
    if (apiErrorCode(error) === "PUSH_TOKEN_EXISTS") {
      toast.error(t("monitors.toasts.push_token_already_used"));
      console.error(error);
      return;
    }
    commonMutationErrorHandler(fallbackMessage)(error);
  };

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
      navigate(workspacePath(`/monitors/${res.data.id}`));
    },
    onError: handleMutationError("Failed to create monitor"),
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

      navigate(workspacePath(`/monitors/${monitorId}`));
    },
    onError: handleMutationError("Failed to update monitor"),
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
      presetsProbes: mode === "create" && !initialValues,
      probesChosenByHand,
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
      initialValues,
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
