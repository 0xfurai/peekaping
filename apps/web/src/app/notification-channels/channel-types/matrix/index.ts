import { defineChannelType } from "../definition";
import { matrixSchema, matrixDefaultValues } from "./schema";
import MatrixForm from "./form";

export const matrixChannelType = defineChannelType({
  type: "matrix",
  displayName: "Matrix",
  schema: matrixSchema,
  defaultValues: matrixDefaultValues,
  Form: MatrixForm,
});
