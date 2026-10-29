import { useState } from "react";
import { Navigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { getMonitorsOptions } from "@/api/@tanstack/react-query.gen";
import Layout from "@/layout";
import { Loading } from "@/components/loading";
import { useCurrentAccess } from "@/hooks/useCurrentAccess";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useWorkspacePath } from "@/hooks/useWorkspacePath";
import { getStartedGate } from "@/lib/get-started";
import { ChannelStep } from "./channel-step";
import { DoneStep } from "./done-step";
import { MonitorStep } from "./monitor-step";
import { NameStep } from "./name-step";

// The steps, in order. Skipping any of the later three, or leaving, ends on the monitors list.
type Step = "name" | "monitor" | "channel" | "done";

function GetStarted() {
  const { t } = useLocalizedTranslation();
  // Only an admin or owner renames the workspace, so anyone else starts at the monitor
  const { canManageWorkspace } = useCurrentAccess();
  const [step, setStep] = useState<Step>(canManageWorkspace ? "name" : "monitor");
  const [monitorId, setMonitorId] = useState<string | null>(null);

  return (
    <Layout pageName={t("get_started.page_name")}>
      <div className="max-w-xl flex flex-col gap-4">
        {step === "name" && <NameStep onDone={() => setStep("monitor")} />}
        {step === "monitor" && (
          <MonitorStep
            onCreated={(id) => {
              setMonitorId(id);
              setStep("channel");
            }}
          />
        )}
        {step === "channel" && <ChannelStep monitorId={monitorId} onSaved={() => setStep("done")} />}
        {step === "done" && <DoneStep monitorId={monitorId} />}
      </div>
    </Layout>
  );
}

// /w/<id>/get-started. It shows while the workspace has no monitor and nothing is stored about it:
// a workspace that has one (an upgraded install's, say) goes to its monitors list. The check is
// made once, on the first answer, so that the monitor step 2 creates doesn't send the user away.
export default function GetStartedPage() {
  const workspacePath = useWorkspacePath();
  const { data, isPending, isError } = useQuery({
    ...getMonitorsOptions({ query: { limit: 1 } }),
    // An answer from an earlier visit must not decide this one
    gcTime: 0,
    refetchOnWindowFocus: false,
  });
  const gate = getStartedGate({ pending: isPending, error: isError, monitors: data?.data });
  const [shown, setShown] = useState(false);
  if (gate === "show" && !shown) setShown(true);

  if (shown || gate === "show") return <GetStarted />;
  if (gate === "loading") return <Loading />;
  return <Navigate to={workspacePath("/monitors")} replace />;
}
