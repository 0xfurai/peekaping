import React, { createContext, useContext, useMemo, useState } from "react";
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
  postMonitorsMutation,
  putMonitorsByIdMutation,
  getMonitorsByIdOptions,
  getMonitorsByIdQueryKey,
} from "@/api/@tanstack/react-query.gen";
import { toast } from "sonner";
import { useNavigate } from "react-router-dom";
import { AxiosError } from "axios";
import { pushSchema, type PushForm } from "../components/push";
import type {
  Options,
  PostMonitorsData,
  UtilsApiError,
  UtilsApiResponseMonitorModel,
  PutMonitorsByIdResponse,
  PutMonitorsByIdError,
  PutMonitorsByIdData,
} from "@/api";
import type { UtilsApiResponseMonitorMonitorResponseDto } from "@/api/types.gen";
import {
  httpDefaultValues,
  httpSchema,
  type HttpForm,
} from "../components/http/schema";
import { z } from "zod";
import { commonMutationErrorHandler } from "@/lib/utils";

const formSchema = z.discriminatedUnion("type", [httpSchema, pushSchema]);

export type MonitorForm = HttpForm | PushForm;

export const formDefaultValues: MonitorForm = httpDefaultValues;

type Mode = "create" | "edit";

interface MonitorFormContextType {
  form: UseFormReturn<MonitorForm>;
  mutation:
    | UseMutationResult<
        UtilsApiResponseMonitorModel,
        AxiosError<UtilsApiError, unknown>,
        Options<PostMonitorsData>,
        unknown
      >
    | UseMutationResult<
        PutMonitorsByIdResponse,
        AxiosError<PutMonitorsByIdError>,
        Options<PutMonitorsByIdData>,
        unknown
      >;
  notifierSheetOpen: boolean;
  setNotifierSheetOpen: React.Dispatch<React.SetStateAction<boolean>>;
  proxySheetOpen: boolean;
  setProxySheetOpen: React.Dispatch<React.SetStateAction<boolean>>;
  monitor?: UtilsApiResponseMonitorMonitorResponseDto;
  mode: Mode;
  isPending: boolean;
  createMonitorMutation: UseMutationResult<
    UtilsApiResponseMonitorModel,
    AxiosError<UtilsApiError, unknown>,
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
}

export const MonitorFormProvider: React.FC<MonitorFormProviderProps> = ({
  children,
  mode,
  monitorId,
}) => {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [notifierSheetOpen, setNotifierSheetOpen] = useState(false);
  const [proxySheetOpen, setProxySheetOpen] = useState(false);

  // Only fetch monitor in edit mode
  const { data: monitor } = useQuery({
    ...getMonitorsByIdOptions({ path: { id: monitorId! } }),
    enabled: mode === "edit" && !!monitorId,
  });

  const form = useForm<MonitorForm>({
    defaultValues: formDefaultValues,
    resolver: zodResolver(formSchema),
  });

  // Mutations
  const createMonitorMutation = useMutation({
    ...postMonitorsMutation(),
    onSuccess: () => {
      toast.success("Monitor created successfully");
      queryClient.invalidateQueries({
        queryKey: getMonitorsInfiniteQueryKey(),
      });
      navigate("/monitors");
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
      mutation: mode === "create" ? createMonitorMutation : editMonitorMutation,
      notifierSheetOpen,
      setNotifierSheetOpen,
      proxySheetOpen,
      setProxySheetOpen,
      monitor,
      // onSubmit,
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
      // onSubmit,
      mode,
      monitorId,
    ]
  );

  if (mode === "edit" && !monitorId) {
    throw new Error("Monitor ID is required in edit mode");
  }

  return (
    <MonitorFormContext.Provider value={value}>
      {children}
    </MonitorFormContext.Provider>
  );
};

export const useMonitorFormContext = () => {
  const ctx = useContext(MonitorFormContext);
  if (!ctx)
    throw new Error(
      "useMonitorFormContext must be used within a MonitorFormProvider"
    );
  return ctx;
};
