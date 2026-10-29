import { toast } from "sonner";
import { WorkspaceNameForm } from "@/components/workspace-name-form";
import { useCurrentWorkspace } from "@/hooks/useCurrentWorkspace";
import { useSaveWorkspaceName } from "@/hooks/useSaveWorkspaceName";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { workspaceErrorMessage } from "@/lib/workspace-errors";
import { StepCard } from "./step-card";

// Step 1: the workspace's name, as it is now ("My workspace" for a first signup, the name just
// typed for a new workspace). Continue renames it only when it was changed.
export function NameStep({ onDone }: { onDone: () => void }) {
  const { t } = useLocalizedTranslation();
  const workspace = useCurrentWorkspace();
  const { save, pending } = useSaveWorkspaceName();

  const submit = async (name: string) => {
    try {
      await save(name, workspace?.name ?? "");
      onDone();
    } catch (error) {
      console.error(error);
      toast.error(workspaceErrorMessage(error, t, t("workspaces.rename_error")));
    }
  };

  return (
    <StepCard step={1} title={t("get_started.name.title")} description={t("get_started.name.description")}>
      <WorkspaceNameForm
        name={workspace?.name ?? ""}
        submitLabel={pending ? t("common.saving") : t("common.continue")}
        pending={pending}
        onSubmit={submit}
      />
    </StepCard>
  );
}
