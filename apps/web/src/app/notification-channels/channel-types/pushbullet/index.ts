import { defineChannelType } from "../definition";
import { pushbulletSchema, pushbulletDefaultValues } from "./schema";
import PushbulletForm from "./form";

export const pushbulletChannelType = defineChannelType({
  type: "pushbullet",
  displayName: "Pushbullet",
  schema: pushbulletSchema,
  defaultValues: pushbulletDefaultValues,
  Form: PushbulletForm,
});
