import { defineChannelType } from "../definition";
import { pagerTreeSchema, pagerTreeDefaultValues } from "./schema";
import PagerTreeForm from "./form";

export const pagerTreeChannelType = defineChannelType({
  type: "pagertree",
  displayName: "PagerTree",
  schema: pagerTreeSchema,
  defaultValues: pagerTreeDefaultValues,
  Form: PagerTreeForm,
});
