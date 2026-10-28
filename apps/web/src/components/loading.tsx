import { useLocalizedTranslation } from "@/hooks/useTranslation";

// The loading state the pages show while their data loads.
export function Loading() {
  const { t } = useLocalizedTranslation();
  return <div className="p-4 w-full">{t("common.loading")}</div>;
}
