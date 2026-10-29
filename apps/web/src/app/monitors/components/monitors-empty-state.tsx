import { useNavigate } from "react-router-dom";
import EmptyList from "@/components/empty-list";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useWorkspacePath } from "@/hooks/useWorkspacePath";

// What the monitors list shows when the workspace has no monitor: create one, or set up where its
// alerts go first. That channel is made a default one, which a new monitor starts with ticked.
export function MonitorsEmptyState() {
  const navigate = useNavigate();
  const workspacePath = useWorkspacePath();
  const { t } = useLocalizedTranslation();
  return (
    <EmptyList
      title={t("monitors.empty_state.title")}
      text={t("monitors.empty_state.description")}
      actionText={t("monitors.empty_state.action")}
      onClick={() => navigate(workspacePath("/monitors/new"))}
      secondaryActionText={t("monitors.empty_state.add_channel")}
      secondaryTo={workspacePath("/notification-channels/new?default=true")}
    />
  );
}
