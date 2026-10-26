import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  getProbesAvailabilityOptions,
  getProbesOptions,
} from "@/api/@tanstack/react-query.gen";
import type {
  ProbeProbeResponse,
  ProbeProbeWithTokenResponse,
} from "@/api/types.gen";
import EmptyList from "@/components/empty-list";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { useDelayedLoading } from "@/hooks/useDelayedLoading";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import Layout from "@/layout";
import CreateProbeDialog from "./components/create-probe-dialog";
import DeleteProbeDialog from "./components/delete-probe-dialog";
import EditProbeDialog from "./components/edit-probe-dialog";
import ProbeTokenAlert, {
  type IssuedProbeToken,
} from "./components/probe-token-alert";
import ProbesTable from "./components/probes-table";
import RotateProbeDialog from "./components/rotate-probe-dialog";

// How often the list refreshes, so a probe that has just started shows up as online.
const REFRESH_INTERVAL_MS = 10_000;

// How often the availability refreshes. A probe's bar changes only when it goes online or offline.
const AVAILABILITY_REFRESH_INTERVAL_MS = 30_000;

// The probe a dialog is about. It stays set after the dialog closes, so the
// dialog keeps its text while it fades out.
const useProbeDialog = () => {
  const [state, setState] = useState<{
    open: boolean;
    probe: ProbeProbeResponse | null;
  }>({ open: false, probe: null });

  return {
    probe: state.probe,
    open: state.open,
    show: (probe: ProbeProbeResponse) => setState({ open: true, probe }),
    onOpenChange: (open: boolean) => setState((prev) => ({ ...prev, open })),
  };
};

const ProbesPage = () => {
  const { t } = useLocalizedTranslation();

  const [showCreateDialog, setShowCreateDialog] = useState(false);
  const editDialog = useProbeDialog();
  const rotateDialog = useProbeDialog();
  const deleteDialog = useProbeDialog();
  const [issued, setIssued] = useState<IssuedProbeToken | null>(null);

  const { data, isLoading, isError } = useQuery({
    ...getProbesOptions(),
    refetchInterval: REFRESH_INTERVAL_MS,
  });
  // The bars and percentages of every row come from this one request, not one per probe
  const { data: availabilityData } = useQuery({
    ...getProbesAvailabilityOptions({ query: { range: "24h" } }),
    refetchInterval: AVAILABILITY_REFRESH_INTERVAL_MS,
  });
  const availability = useMemo(
    () =>
      new Map(
        (availabilityData?.data ?? []).map((entry) => [entry.probe_id, entry])
      ),
    [availabilityData]
  );
  const shouldShowSkeleton = useDelayedLoading(isLoading, 200);

  const probes: ProbeProbeResponse[] = data?.data ?? [];

  const handleCreated = (probe: ProbeProbeWithTokenResponse) =>
    setIssued({ kind: "created", probe });

  const handleRotated = (probe: ProbeProbeWithTokenResponse) =>
    setIssued({ kind: "rotated", probe });

  const handleDeleted = (probe: ProbeProbeResponse) =>
    // The token of a probe that is gone is of no use.
    setIssued((prev) => (prev?.probe.id === probe.id ? null : prev));

  return (
    <Layout
      pageName={t("probes.page_name")}
      onCreate={() => setShowCreateDialog(true)}
    >
      <div className="flex flex-col gap-4">
        <p className="text-sm text-muted-foreground">
          {t("probes.description")}
        </p>

        {issued && (
          <ProbeTokenAlert issued={issued} onDismiss={() => setIssued(null)} />
        )}

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
              <ProbesTable
                probes={probes}
                availability={availability}
                onEdit={editDialog.show}
                onRotate={rotateDialog.show}
                onDelete={deleteDialog.show}
              />
            </CardContent>
          </Card>
        )}

        {probes.length === 0 && !isLoading && !isError && (
          <EmptyList
            title={t("probes.empty_list.title")}
            text={t("probes.empty_list.text")}
            actionText={t("probes.empty_list.action_text")}
            onClick={() => setShowCreateDialog(true)}
          />
        )}
      </div>

      <CreateProbeDialog
        open={showCreateDialog}
        onOpenChange={setShowCreateDialog}
        onCreated={handleCreated}
      />

      {editDialog.probe && (
        <EditProbeDialog
          probe={editDialog.probe}
          open={editDialog.open}
          onOpenChange={editDialog.onOpenChange}
        />
      )}

      {rotateDialog.probe && (
        <RotateProbeDialog
          probe={rotateDialog.probe}
          open={rotateDialog.open}
          onOpenChange={rotateDialog.onOpenChange}
          onRotated={handleRotated}
        />
      )}

      {deleteDialog.probe && (
        <DeleteProbeDialog
          probe={deleteDialog.probe}
          open={deleteDialog.open}
          onOpenChange={deleteDialog.onOpenChange}
          onDeleted={handleDeleted}
        />
      )}
    </Layout>
  );
};

export default ProbesPage;
