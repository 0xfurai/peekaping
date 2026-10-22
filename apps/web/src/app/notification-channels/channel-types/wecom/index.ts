import { defineChannelType } from "../definition";
import { weComSchema, weComDefaultValues } from "./schema";
import WeComForm from "./form";

export const weComChannelType = defineChannelType({
  type: "wecom",
  displayName: "WeCom (企业微信)",
  schema: weComSchema,
  defaultValues: weComDefaultValues,
  Form: WeComForm,
});
