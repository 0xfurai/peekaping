import { useMutation } from "@tanstack/react-query";
import { Download, Loader2, TriangleAlert } from "lucide-react";
import { toast } from "sonner";
import { getWorkspaceExport, getWorkspaceExportHeartbeats } from "@/api/sdk.gen";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useCurrentAccess } from "@/hooks/useCurrentAccess";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { asDownload, downloadName, saveDownload } from "@/lib/save-blob";

type Download = {
  // The request, which comes back with the file as a blob
  fetch: () => Promise<{ data: Blob; headers: Record<string, unknown> }>;
  // The name to save under when the response doesn't give one
  fallbackName: () => string;
  failed: string;
};

// Fetches the file through the app's client, which adds the Authorization and X-Workspace-ID
// headers a plain link can't, and saves it
function useDownload({ fetch, fallbackName, failed }: Download) {
  return useMutation({
    mutationFn: async () => saveDownload(await fetch(), fallbackName()),
    onError: (error) => {
      console.error(error);
      toast.error(failed);
    },
  });
}

function DownloadButton({
  label,
  download,
  variant,
}: {
  label: string;
  download: ReturnType<typeof useDownload>;
  variant: "default" | "outline";
}) {
  const { t } = useLocalizedTranslation();
  const Icon = download.isPending ? Loader2 : Download;
  return (
    <Button type="button" variant={variant} disabled={download.isPending} onClick={() => download.mutate()}>
      <Icon className={download.isPending ? "mr-2 h-4 w-4 animate-spin" : "mr-2 h-4 w-4"} />
      {download.isPending ? t("settings.workspace_export.downloading") : label}
    </Button>
  );
}

// The workspace export (GLOSSARY "Workspace export"), for an owner: the setup as one file, and the
// heartbeat history as another. The file holds saved secrets in full, which the card says first.
export function ExportCard() {
  const { t } = useLocalizedTranslation();
  const { role } = useCurrentAccess();
  const workspaceFile = useDownload({
    // The spec types this route as JSON, so the client would parse the file: ask for it as a blob
    fetch: async () => asDownload(await getWorkspaceExport({ throwOnError: true, responseType: "blob" })),
    fallbackName: () => downloadName("peekaping-workspace", "json"),
    failed: t("settings.workspace_export.failed"),
  });
  const heartbeatFile = useDownload({
    fetch: async () => asDownload(await getWorkspaceExportHeartbeats({ throwOnError: true })),
    fallbackName: () => downloadName("peekaping-heartbeats", "ndjson"),
    failed: t("settings.workspace_export.heartbeats_failed"),
  });
  if (role !== "owner") return null;

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("settings.workspace_export.title")}</CardTitle>
        <CardDescription>{t("settings.workspace_export.description")}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <Alert variant="destructive">
          <TriangleAlert className="h-4 w-4" />
          <AlertTitle>{t("settings.workspace_export.warning_title")}</AlertTitle>
          <AlertDescription>{t("settings.workspace_export.warning")}</AlertDescription>
        </Alert>
        <div className="flex flex-col gap-2 items-start">
          <DownloadButton
            label={t("settings.workspace_export.download_workspace")}
            download={workspaceFile}
            variant="default"
          />
        </div>
        <div className="flex flex-col gap-2 items-start">
          <DownloadButton
            label={t("settings.workspace_export.download_heartbeats")}
            download={heartbeatFile}
            variant="outline"
          />
          <p className="text-sm text-muted-foreground">{t("settings.workspace_export.heartbeats_note")}</p>
        </div>
      </CardContent>
    </Card>
  );
}
