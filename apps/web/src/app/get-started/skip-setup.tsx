import { Link } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useWorkspacePath } from "@/hooks/useWorkspacePath";

// Leaves Get started for the monitors list.
export function SkipSetup() {
  const { t } = useLocalizedTranslation();
  const workspacePath = useWorkspacePath();
  return (
    <Button asChild variant="ghost">
      <Link to={workspacePath("/monitors")}>{t("get_started.skip")}</Link>
    </Button>
  );
}
