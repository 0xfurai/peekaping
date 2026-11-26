import type { MonitorCreateUpdateDto, MonitorMonitorResponseDto } from "@/api";
import type { GrpcKeywordFormValues } from "./schema";
import { getString, textTarget, type MonitorConfig, type MonitorTarget } from "../monitor-target";
import { deserializeMonitorBase, serializeMonitorBase } from "../shared/codec";

interface GrpcKeywordConfig {
  grpcUrl: string;
  grpcProtobuf: string;
  grpcServiceName: string;
  grpcMethod: string;
  grpcEnableTls: boolean;
  grpcBody: string;
  keyword: string;
  invertKeyword: boolean;
}

export const formatTarget = (config: MonitorConfig): MonitorTarget | null =>
  textTarget(getString(config, "grpcUrl"));

export const deserialize = (
  data: MonitorMonitorResponseDto
): GrpcKeywordFormValues => {
  let config: GrpcKeywordConfig = {
    grpcUrl: "localhost:50051",
    grpcProtobuf: `syntax = "proto3";

package grpc.health.v1;

service Health {
  rpc Check(HealthCheckRequest) returns (HealthCheckResponse);
  rpc Watch(HealthCheckRequest) returns (stream HealthCheckResponse);
}`,
    grpcServiceName: "Health",
    grpcMethod: "check",
    grpcEnableTls: false,
    grpcBody: `{
  "key": "value"
}`,
    keyword: "",
    invertKeyword: false,
  };

  if (data.config) {
    try {
      const parsedConfig = JSON.parse(data.config);
      config = {
        grpcUrl: parsedConfig.grpcUrl || "localhost:50051",
        grpcProtobuf: parsedConfig.grpcProtobuf || config.grpcProtobuf,
        grpcServiceName: parsedConfig.grpcServiceName || "Health",
        grpcMethod: parsedConfig.grpcMethod || "check",
        grpcEnableTls: parsedConfig.grpcEnableTls ?? false,
        grpcBody: parsedConfig.grpcBody || config.grpcBody,
        keyword: parsedConfig.keyword || "",
        invertKeyword: parsedConfig.invertKeyword ?? false,
      };
    } catch (error) {
      console.error("Failed to parse gRPC monitor config:", error);
    }
  }

  return {
    type: "grpc-keyword",
    ...deserializeMonitorBase(data),
    grpcUrl: config.grpcUrl,
    grpcProtobuf: config.grpcProtobuf,
    grpcServiceName: config.grpcServiceName,
    grpcMethod: config.grpcMethod,
    grpcEnableTls: config.grpcEnableTls,
    grpcBody: config.grpcBody,
    keyword: config.keyword,
    invertKeyword: config.invertKeyword,
  };
};

export const serialize = (
  values: GrpcKeywordFormValues
): MonitorCreateUpdateDto => {
  const config: GrpcKeywordConfig = {
    grpcUrl: values.grpcUrl,
    grpcProtobuf: values.grpcProtobuf,
    grpcServiceName: values.grpcServiceName,
    grpcMethod: values.grpcMethod,
    grpcEnableTls: values.grpcEnableTls,
    grpcBody: values.grpcBody,
    keyword: values.keyword,
    invertKeyword: values.invertKeyword,
  };

  return {
    type: "grpc-keyword",
    ...serializeMonitorBase(values),
    config: JSON.stringify(config),
  };
};
