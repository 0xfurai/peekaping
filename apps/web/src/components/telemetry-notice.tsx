import { Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Info } from "lucide-react";
import {
  getErrorReportingOptions,
  getTelemetryOptions,
  getTelemetryQueryKey,
  postTelemetryNoticeMutation,
} from "@/api/@tanstack/react-query.gen";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { TelemetryDocsLink } from "@/components/telemetry-docs-link";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { commonMutationErrorHandler } from "@/lib/utils";

// The one-time notice for an install that upgraded to the version where the usage ping and error
// reports are on by default. The server says it is pending (GET /telemetry) until the admin
// dismisses it or makes a choice for either feature, and it is never pending when the environment
// switches both features off. It names only the features that are really on.
export function TelemetryNotice() {
  const { t } = useLocalizedTranslation();
  const queryClient = useQueryClient();
  const { data: usage } = useQuery(getTelemetryOptions());
  const pending = usage?.notice_pending === true;
  // Only an admin who still has the notice to read needs to know what is on
  const { data: errors } = useQuery({
    ...getErrorReportingOptions(),
    enabled: pending,
  });

  const dismiss = useMutation({
    ...postTelemetryNoticeMutation(),
    onSuccess: (state) => {
      queryClient.setQueryData(getTelemetryQueryKey(), state);
    },
    onError: commonMutationErrorHandler(t("messages.setting_update_error")),
  });

  if (!pending || !usage || !errors) return null;
  const usageOn = usage.enabled && !usage.forced_off;
  const errorsOn = errors.enabled && !errors.forced_off;
  if (!usageOn && !errorsOn) return null;

  return (
    <Alert role="region" aria-label={t("telemetry_notice.title")}>
      <Info className="h-4 w-4" />
      <AlertTitle>{t("telemetry_notice.title")}</AlertTitle>
      <AlertDescription className="flex flex-col gap-3">
        {usageOn && (
          <div className="flex flex-col gap-1">
            <p>{t("telemetry_notice.usage_statistics")}</p>
            <Link to="/settings#usage-statistics" className="underline w-fit">
              {t("telemetry_notice.settings_usage")}
            </Link>
          </div>
        )}
        {errorsOn && (
          <div className="flex flex-col gap-1">
            <p>{t("telemetry_notice.error_reports")}</p>
            <Link to="/settings#error-reports" className="underline w-fit">
              {t("telemetry_notice.settings_errors")}
            </Link>
          </div>
        )}
        <div className="flex flex-wrap items-center gap-4">
          <Button
            size="sm"
            variant="outline"
            disabled={dismiss.isPending}
            onClick={() => dismiss.mutate({})}
          >
            {t("common.dismiss")}
          </Button>
          <TelemetryDocsLink />
        </div>
      </AlertDescription>
    </Alert>
  );
}
