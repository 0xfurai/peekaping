import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

// What a figure shows when there is nothing to measure, e.g. the uptime of a period with no
// heartbeats. A dash with a "No data" tooltip, so it can't be read as a 0.
export function NoData() {
  const { t } = useLocalizedTranslation();

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span data-testid="no-data" tabIndex={0} className="cursor-default">
          &mdash;
        </span>
      </TooltipTrigger>
      <TooltipContent>{t("common.no_data")}</TooltipContent>
    </Tooltip>
  );
}
