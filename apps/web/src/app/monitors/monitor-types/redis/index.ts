import { defineMonitorType } from "../definition";
import { redisSchema, redisDefaultValues } from "./schema";
import { deserialize, serialize } from "./codec";
import RedisForm from "./form";

export const redisMonitorType = defineMonitorType({
  type: "redis",
  labelKey: "monitors.form.type.redis",
  schema: redisSchema,
  defaultValues: redisDefaultValues,
  deserialize,
  serialize,
  Form: RedisForm,
});
