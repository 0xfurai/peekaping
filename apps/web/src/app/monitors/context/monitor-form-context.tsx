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
import { httpKeywordSchema, type HttpKeywordForm } from "../components/http-keyword/schema";
import { httpJsonQuerySchema, type HttpJsonQueryForm } from "../components/http-json-query/schema";
import { tcpSchema, type TCPForm } from "../components/tcp";
import { pingSchema, type PingForm } from "../components/ping";
import { dnsSchema, type DNSForm } from "../components/dns";
import { dockerSchema, type DockerForm } from "../components/docker";
import {
    grpcKeywordSchema,
    type GRPCKeywordForm,
} from "../components/grpc-keyword";
import { snmpSchema, type SnmpForm } from "../components/snmp";
import { mysqlSchema, type MySQLForm } from "../components/mysql";
import { mongodbSchema, type MongoDBForm } from "../components/mongodb";
import { redisSchema, type RedisForm } from "../components/redis";
import { z } from "zod";
import { commonMutationErrorHandler } from "@/lib/utils";
import { deserializeMonitor } from "../components/monitor-registry";
import {
    postgresSchema,
    type PostgresForm,
} from "../components/postgres/schema";
import {
    sqlServerSchema,
    type SQLServerForm,
} from "../components/sqlserver/schema";
import { mqttSchema, type MQTTForm } from "../components/mqtt";
import { rabbitMQSchema, type RabbitMQForm } from "../components/rabbitmq";
import { kafkaProducerSchema, type KafkaProducerForm } from "../components/kafka-producer/schema";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useOrganizationStore } from "@/store/organization";

const formSchema = z.discriminatedUnion("type", [
    httpSchema,
    httpKeywordSchema,
    httpJsonQuerySchema,
    tcpSchema,
    pingSchema,
    dnsSchema,
    pushSchema,
    dockerSchema,
    grpcKeywordSchema,
    snmpSchema,
    mysqlSchema,
    postgresSchema,
    sqlServerSchema,
    mongodbSchema,
    redisSchema,
    mqttSchema,
    rabbitMQSchema,
    kafkaProducerSchema,
]);

export type MonitorForm =
    | HttpForm
    | HttpKeywordForm
    | HttpJsonQueryForm
    | TCPForm
    | PingForm
    | DNSForm
    | PushForm
    | DockerForm
    | SnmpForm
    | GRPCKeywordForm
    | PostgresForm
    | SQLServerForm
    | MySQLForm
    | MongoDBForm
    | RedisForm
    | MQTTForm
    | RabbitMQForm
    | KafkaProducerForm;

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
    initialValues?: MonitorForm;
}

export const MonitorFormProvider: React.FC<MonitorFormProviderProps> = ({
    children,
    mode,
    monitorId,
    initialValues = formDefaultValues,
}) => {
    const navigate = useNavigate();
    const queryClient = useQueryClient();
    const { t } = useLocalizedTranslation();
    const [notifierSheetOpen, setNotifierSheetOpen] = useState(false);
    const [proxySheetOpen, setProxySheetOpen] = useState(false);
    const { currentOrganization } = useOrganizationStore();
    const slug = currentOrganization?.slug;
    const listPath = slug ? `/${slug}/monitors` : "/monitors";

    // Only fetch monitor in edit mode
    const { data: monitor } = useQuery({
        ...getMonitorsByIdOptions({ path: { id: monitorId! } }),
        enabled: mode === "edit" && !!monitorId,
    });

    const form = useForm<MonitorForm>({
        defaultValues: initialValues,
        resolver: zodResolver(formSchema),
    });

    // Handle form population for edit mode only
    useEffect(() => {
        let formData: MonitorForm | undefined;

        try {
            if (mode === "edit" && monitor?.data) {
                // Use registry deserialize function for edit mode
                formData = deserializeMonitor(monitor.data);
            }

            if (formData) {
                form.reset(formData);
            }
        } catch (error) {
            console.error("Failed to deserialize monitor data:", error);
            toast.error("Failed to load monitor data");
        }
    }, [mode, monitor, form]);

    // Mutations
    const createMonitorMutation = useMutation({
        ...postMonitorsMutation(),
        onSuccess: (res) => {
            toast.success("Monitor created successfully");
            queryClient.invalidateQueries({
                queryKey: getMonitorsInfiniteQueryKey({}),
            });
            queryClient.invalidateQueries({
                queryKey: getMonitorsQueryKey({}),
            });
            navigate(`${listPath}/${res.data.id}`);
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
                queryKey: getMonitorsInfiniteQueryKey({}),
            });
            queryClient.removeQueries({
                queryKey: getMonitorsByIdQueryKey({ path: { id: monitorId! } }),
            });

            navigate(`${listPath}/${monitorId}`);
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
