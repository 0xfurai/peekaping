import { useQuery } from "@tanstack/react-query";
import { getProbesOptions } from "@/api/@tanstack/react-query.gen";
import type { ProbeProbeResponse } from "@/api/types.gen";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { useDelayedLoading } from "@/hooks/useDelayedLoading";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import Layout from "@/layout";
import ProbesTable from "./components/probes-table";

// How often the list refreshes, so a probe that has just started shows up as online.
const REFRESH_INTERVAL_MS = 10_000;

const ProbesPage = () => {
  const { t } = useLocalizedTranslation();

  const { data, isLoading, isError } = useQuery({
    ...getProbesOptions(),
    refetchInterval: REFRESH_INTERVAL_MS,
  });
  const shouldShowSkeleton = useDelayedLoading(isLoading, 200);

  const probes: ProbeProbeResponse[] = data?.data ?? [];

  return (
    <Layout pageName={t("probes.page_name")}>
      <div className="flex flex-col gap-4">
        <p className="text-sm text-muted-foreground">
          {t("probes.description")}
        </p>

        {isError && (
          <p className="text-sm text-destructive">
            {t("probes.messages.failed_to_load")}
          </p>
        )}

        {probes.length === 0 && shouldShowSkeleton && (
          <div className="flex flex-col space-y-2">
            {Array.from({ length: 3 }, (_, id) => (
              <Skeleton className="h-[68px] w-full rounded-xl" key={id} />
            ))}
          </div>
        )}

        {probes.length > 0 && (
          <Card>
            <CardContent>
              <ProbesTable probes={probes} />
            </CardContent>
          </Card>
        )}
      </div>
    </Layout>
  );
};

export default ProbesPage;
