import { z } from "zod";
import { monitorBaseSchema, monitorBaseDefaultValues } from "../shared/schema";

export const grpcKeywordSchema = z
  .object({
    type: z.literal("grpc-keyword"),
    grpcUrl: z.string().min(1, "gRPC URL is required"),
    grpcProtobuf: z.string().min(1, "Proto content is required"),
    grpcServiceName: z.string().min(1, "Proto service name is required"),
    grpcMethod: z.string().min(1, "Proto method is required"),
    grpcEnableTls: z.boolean(),
    grpcBody: z.string(),
    keyword: z.string(),
    invertKeyword: z.boolean(),
  })
  .merge(monitorBaseSchema);

export type GrpcKeywordFormValues = z.infer<typeof grpcKeywordSchema>;

export const grpcKeywordDefaultValues: GrpcKeywordFormValues = {
  type: "grpc-keyword",
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
  ...monitorBaseDefaultValues,
};
