import { Link, useLocation } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { useCurrentAccess } from "@/hooks/useCurrentAccess";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useMe } from "@/hooks/useMe";
import { useWorkspacePath } from "@/hooks/useWorkspacePath";
import { isSuspendedUnverified } from "@/lib/account-email";
import { nextStepLinks } from "@/lib/get-started";
import { workspaceIdFromPathname } from "@/lib/workspace-path";
import { StepCard } from "./step-card";

// Step 4: what to do next. The first link is the way out of Get started: the new monitor's page,
// or the monitors list when step 2 made none. A workspace that waits for its owner to verify their
// email doesn't run monitors yet; the verification banner above the page has the Resend button.
export function DoneStep({ monitorId }: { monitorId: string | null }) {
  const { t } = useLocalizedTranslation();
  const workspacePath = useWorkspacePath();
  const { instanceAdmin } = useCurrentAccess();
  const { pathname } = useLocation();
  const workspaces = useMe().data?.data?.workspaces;
  const waitingForVerification = isSuspendedUnverified(workspaces, workspaceIdFromPathname(pathname));
  const [first, ...others] = nextStepLinks({ monitorId, instanceAdmin });
  const href = (link: { path: string; outsideWorkspace?: true }) =>
    link.outsideWorkspace ? link.path : workspacePath(link.path);

  return (
    <StepCard step={4} title={t("get_started.done.title")} description={t("get_started.done.description")}>
      {waitingForVerification && <p className="text-sm">{t("get_started.done.unverified")}</p>}
      <div>
        <Button asChild>
          <Link to={href(first)}>{t(`get_started.done.links.${first.key}`)}</Link>
        </Button>
      </div>
      <ul className="flex flex-col gap-2 text-sm">
        {others.map((link) => (
          <li key={link.key}>
            <Link to={href(link)} className="underline">
              {t(`get_started.done.links.${link.key}`)}
            </Link>
          </li>
        ))}
      </ul>
    </StepCard>
  );
}
