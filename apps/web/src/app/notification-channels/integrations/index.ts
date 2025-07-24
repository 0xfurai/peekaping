import { lazy } from "react";
import { z } from "zod";

const importIntegration = (name: string) =>
  lazy(() => import(`./${name}-form`));

export const integrations = {
  webhook: importIntegration("webhook"),
  slack: importIntegration("slack"),
  discord: importIntegration("discord"),
  telegram: importIntegration("telegram"),
  wecom: importIntegration("wecom"),
} as const;

export const getIntegrationSchema = async (type: keyof typeof integrations) => {
  const module = await import(`./${type}-form`);
  return module.schema;
};

export const getIntegrationDefaults = async (type: keyof typeof integrations) => {
  const module = await import(`./${type}-form`);
  return module.defaultValues;
};

export const getIntegrationDisplayName = async (type: keyof typeof integrations) => {
  const module = await import(`./${type}-form`);
  return module.displayName;
};

export type IntegrationType = keyof typeof integrations;
