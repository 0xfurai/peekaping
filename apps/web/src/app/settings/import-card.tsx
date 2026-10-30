import { useId, useRef, useState, type ChangeEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";
import { postWorkspaceImportMutation } from "@/api/@tanstack/react-query.gen";
import type { WorkspaceportDocument, WorkspaceportImportResult, WorkspaceportProblem } from "@/api/types.gen";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useCurrentAccess } from "@/hooks/useCurrentAccess";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { dismissWhileIdle } from "@/lib/admin-users";
import {
  affectedByImport,
  changeMessage,
  createdLines,
  importProblems,
  problemLine,
  readImportFile,
} from "@/lib/workspace-import";
import { workspaceErrorMessage } from "@/lib/workspace-errors";

type Chosen = { name: string; document: WorkspaceportDocument };

// What the import screen shows after a try: the server's answer, or the problems of a refused file
type Outcome = { kind: "done"; result: WorkspaceportImportResult } | { kind: "refused"; problems: WorkspaceportProblem[] };

// The workspace import (GLOSSARY "Workspace export"), for an owner: the file is read and parsed in
// the browser, the owner confirms, and the server adds its records beside the ones there.
export function ImportCard() {
  const { t } = useLocalizedTranslation();
  const { role } = useCurrentAccess();
  const queryClient = useQueryClient();
  const inputId = useId();
  const input = useRef<HTMLInputElement>(null);
  const [chosen, setChosen] = useState<Chosen | null>(null);
  const [fileError, setFileError] = useState<string | null>(null);
  const [outcome, setOutcome] = useState<Outcome | null>(null);

  const send = useMutation({
    ...postWorkspaceImportMutation(),
    onSuccess: async (answer) => {
      setChosen(null);
      setOutcome({ kind: "done", result: answer });
      await queryClient.invalidateQueries({ predicate: (query) => affectedByImport(query.queryKey) });
    },
    onError: (error) => {
      const problems = importProblems(error);
      setChosen(null);
      if (problems) return setOutcome({ kind: "refused", problems });
      console.error(error);
      toast.error(workspaceErrorMessage(error, t, t("settings.workspace_import.failed")));
    },
  });

  if (role !== "owner") return null;

  const choose = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    // Empty, so that choosing the same file again (after fixing it) fires a change
    event.target.value = "";
    if (!file) return;
    const read = await readImportFile(file);
    setFileError(read.ok ? null : t(`settings.workspace_import.${read.reason}`));
    if (read.ok) setChosen({ name: file.name, document: read.document });
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("settings.workspace_import.title")}</CardTitle>
        <CardDescription>{t("settings.workspace_import.description")}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <div className="flex flex-col gap-2">
          <Label htmlFor={inputId}>{t("settings.workspace_import.file_label")}</Label>
          <Input
            id={inputId}
            ref={input}
            type="file"
            accept=".json,application/json"
            disabled={send.isPending}
            onChange={choose}
          />
        </div>
        {fileError && (
          <Alert variant="destructive">
            <AlertDescription>{fileError}</AlertDescription>
          </Alert>
        )}
      </CardContent>
      <ConfirmImportDialog
        chosen={chosen}
        pending={send.isPending}
        onCancel={() => setChosen(null)}
        onConfirm={() => chosen && send.mutate({ body: chosen.document })}
      />
      <ImportOutcomeDialog outcome={outcome} onClose={() => setOutcome(null)} />
    </Card>
  );
}

// Says what an import does before it does it. A pending request can't be dismissed: the dialog
// stays until the answer.
function ConfirmImportDialog({
  chosen,
  pending,
  onCancel,
  onConfirm,
}: {
  chosen: Chosen | null;
  pending: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const { t } = useLocalizedTranslation();
  const { onOpenChange, onEscapeKeyDown, onPointerDownOutside } = dismissWhileIdle(pending, onCancel);
  return (
    <Dialog open={chosen !== null} onOpenChange={onOpenChange}>
      <DialogContent
        showCloseButton={!pending}
        onEscapeKeyDown={onEscapeKeyDown}
        onPointerDownOutside={onPointerDownOutside}
      >
        <DialogHeader>
          <DialogTitle className="break-all">
            {t("settings.workspace_import.confirm_title", { file: chosen?.name })}
          </DialogTitle>
          <DialogDescription>{t("settings.workspace_import.confirm_description")}</DialogDescription>
        </DialogHeader>
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" disabled={pending} onClick={onCancel}>
            {t("common.cancel")}
          </Button>
          <Button type="button" disabled={pending} onClick={onConfirm}>
            {pending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            {pending ? t("settings.workspace_import.importing") : t("settings.workspace_import.confirm_button")}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function ImportOutcomeDialog({ outcome, onClose }: { outcome: Outcome | null; onClose: () => void }) {
  return (
    <Dialog open={outcome !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-2xl">
        {outcome?.kind === "done" && <ImportResultView result={outcome.result} onClose={onClose} />}
        {outcome?.kind === "refused" && <ImportProblemsView problems={outcome.problems} onClose={onClose} />}
      </DialogContent>
    </Dialog>
  );
}

function ImportResultView({ result, onClose }: { result: WorkspaceportImportResult; onClose: () => void }) {
  const { t } = useLocalizedTranslation();
  const created = createdLines(result.created, t);
  const changes = result.changes ?? [];
  return (
    <>
      <DialogHeader>
        <DialogTitle>{t("settings.workspace_import.result_title")}</DialogTitle>
        <DialogDescription>
          {created.length === 0 && t("settings.workspace_import.nothing_created")}
        </DialogDescription>
      </DialogHeader>
      {created.length > 0 && (
        <section className="flex flex-col gap-1" aria-label={t("settings.workspace_import.created_title")}>
          <h3 className="text-sm font-medium">{t("settings.workspace_import.created_title")}</h3>
          <ul className="text-sm" data-testid="import-created">
            {created.map((line) => (
              <li key={line.key}>
                {line.label}: {line.count}
              </li>
            ))}
          </ul>
        </section>
      )}
      <section className="flex flex-col gap-1" aria-label={t("settings.workspace_import.changes_title")}>
        <h3 className="text-sm font-medium">{t("settings.workspace_import.changes_title")}</h3>
        {changes.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("settings.workspace_import.no_changes")}</p>
        ) : (
          <ul className="list-disc pl-5 text-sm flex flex-col gap-1" data-testid="import-changes">
            {changes.map((change, index) => (
              <li key={`${change.kind}-${change.ref ?? ""}-${index}`}>{changeMessage(change, t)}</li>
            ))}
          </ul>
        )}
      </section>
      <div className="flex justify-end">
        <Button type="button" onClick={onClose}>
          {t("settings.workspace_import.close")}
        </Button>
      </div>
    </>
  );
}

function ImportProblemsView({ problems, onClose }: { problems: WorkspaceportProblem[]; onClose: () => void }) {
  const { t } = useLocalizedTranslation();
  return (
    <>
      <DialogHeader>
        <DialogTitle>{t("settings.workspace_import.problems_title")}</DialogTitle>
        <DialogDescription>{t("settings.workspace_import.problems_description")}</DialogDescription>
      </DialogHeader>
      <ul className="list-disc pl-5 text-sm flex flex-col gap-1 break-words" data-testid="import-problems">
        {problems.map((problem, index) => (
          <li key={`${problem.record}-${problem.ref ?? ""}-${index}`}>{problemLine(problem)}</li>
        ))}
      </ul>
      <div className="flex justify-end">
        <Button type="button" onClick={onClose}>
          {t("settings.workspace_import.close")}
        </Button>
      </div>
    </>
  );
}
