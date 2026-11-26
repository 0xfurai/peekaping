import { defineMonitorType } from "../definition";
import { grpcKeywordSchema, grpcKeywordDefaultValues } from "./schema";
import { deserialize, serialize, formatTarget } from "./codec";
import GrpcKeywordForm from "./form";

export const grpcKeywordMonitorType = defineMonitorType({
  type: "grpc-keyword",
  labelKey: "monitors.form.type.grpc",
  schema: grpcKeywordSchema,
  defaultValues: grpcKeywordDefaultValues,
  deserialize,
  serialize,
  formatTarget,
  Form: GrpcKeywordForm,
});
