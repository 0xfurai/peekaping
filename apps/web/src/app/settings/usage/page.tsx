import { useQuery } from "@tanstack/react-query";
import { Navigate } from "react-router-dom";
import { getWorkspaceLimitsOptions } from "@/api/@tanstack/react-query.gen";
import Layout from "@/layout";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { useCurrentAccess } from "@/hooks/useCurrentAccess";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useWorkspacePath } from "@/hooks/useWorkspacePath";
import { cn } from "@/lib/utils";
import { hasLimits, usageRows, usageSource, type UsageLevel, type UsageRow } from "@/lib/workspace-usage";

// The bar's colour and the note's, by how close the count is to its limit
const BAR: Record<UsageLevel, string> = {
  ok: "bg-primary",
  near: "bg-amber-500",
  reached: "bg-destructive",
};
const NOTE: Record<Exclude<UsageLevel, "ok">, string> = {
  near: "text-amber-600 dark:text-amber-400",
  reached: "text-destructive",
};

function Row({ row }: { row: UsageRow }) {
  const { t } = useLocalizedTranslation();
  if (row.kind === "value") {
    const value =
      row.name === "min_check_interval"
        ? t("usage.seconds", { value: row.value })
        : t("usage.days", { value: row.value });
    return (
      <div data-limit={row.name} className="flex flex-wrap items-baseline justify-between gap-2">
        <span className="text-sm font-medium">{t(`usage.labels.${row.name}`)}</span>
        <span className="text-sm">{value}</span>
      </div>
    );
  }

  const label = t(`limits.names.${row.name}`);
  return (
    <div data-limit={row.name} className="flex flex-col gap-2">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <span className="text-sm font-medium">{label}</span>
        <span className="text-sm">{t(`usage.count.${row.name}`, { used: row.used, max: row.max })}</span>
      </div>
      <div
        role="progressbar"
        aria-label={label}
        aria-valuemin={0}
        aria-valuenow={row.used}
        aria-valuemax={row.max}
        data-level={row.level}
        className="h-2 w-full overflow-hidden rounded-full bg-muted"
      >
        <div className={cn("h-full rounded-full", BAR[row.level])} style={{ width: `${row.percent}%` }} />
      </div>
      {row.level !== "ok" && (
        <p className={cn("text-xs font-medium", NOTE[row.level])}>
          {row.level === "near" ? t("usage.near") : t("usage.reached")}
        </p>
      )}
    </div>
  );
}

// What the workspace uses of its limits (/w/<id>/settings/usage), for an owner or admin. A
// workspace without limits has no such page, and anyone else goes to the settings, as there.
const UsagePage = () => {
  const { t } = useLocalizedTranslation();
  const { canManageWorkspace } = useCurrentAccess();
  const workspacePath = useWorkspacePath();
  const { data, isLoading, isError } = useQuery({
    ...getWorkspaceLimitsOptions(),
    enabled: canManageWorkspace,
  });
  const report = data?.data;

  if (!canManageWorkspace) return <Navigate to={workspacePath("/settings")} replace />;
  if (isLoading) return <Layout pageName={t("usage.title")} isLoading>{null}</Layout>;
  if (isError || !report) {
    return (
      <Layout pageName={t("usage.title")}>
        <p>{t("messages.unexpected_error")}</p>
      </Layout>
    );
  }
  if (!hasLimits(report)) return <Navigate to={workspacePath("/settings")} replace />;

  const source = usageSource(report, t);
  return (
    <Layout pageName={t("usage.title")}>
      <div className="max-w-xl flex flex-col gap-6">
        <Card>
          <CardHeader>
            <CardTitle>{t("usage.title")}</CardTitle>
            <CardDescription>{t("usage.description")}</CardDescription>
            {source && (
              <p className="flex flex-wrap gap-2 text-sm">
                <span className="text-muted-foreground">{t("usage.plan")}</span>
                <span className="font-medium">{source}</span>
              </p>
            )}
          </CardHeader>
          <CardContent className="flex flex-col gap-5">
            {usageRows(report).map((row) => (
              <Row key={row.name} row={row} />
            ))}
          </CardContent>
        </Card>
      </div>
    </Layout>
  );
};

export default UsagePage;
