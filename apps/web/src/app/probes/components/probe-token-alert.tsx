import { Copy, Key } from "lucide-react";
import { toast } from "sonner";
import type { ProbeProbeWithTokenResponse } from "@/api/types.gen";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

// A probe with a token the API has just shown, after it was created or its token rotated.
export interface IssuedProbeToken {
  kind: "created" | "rotated";
  probe: ProbeProbeWithTokenResponse;
}

interface ProbeTokenAlertProps {
  issued: IssuedProbeToken;
  onDismiss: () => void;
}

const dockerRunCommand = (token: string) =>
  `docker run -d --name peekaping-probe -e PEEKAPING_URL=${window.location.origin} -e PROBE_TOKEN=${token} 0xfurai/peekaping-probe:latest`;

const ProbeTokenAlert = ({ issued, onDismiss }: ProbeTokenAlertProps) => {
  const { t } = useLocalizedTranslation();
  const { kind, probe } = issued;

  const copyToClipboard = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      toast.success(t("probes.messages.copy_success"));
    } catch {
      // navigator.clipboard is missing on a page served over plain http.
      toast.error(t("probes.messages.copy_failed"));
    }
  };

  const dockerCommand = dockerRunCommand(probe.token);

  return (
    <Alert variant="success">
      <Key className="h-4 w-4" />
      <AlertTitle>
        {kind === "created"
          ? t("probes.token_alert.title_created", { name: probe.name })
          : t("probes.token_alert.title_rotated", { name: probe.name })}
      </AlertTitle>
      <AlertDescription>
        <div className="mt-2 flex flex-col gap-4">
          <p className="text-sm text-muted-foreground">
            {t("probes.token_alert.note")}
          </p>

          <div className="flex flex-col gap-1">
            <Label>{t("probes.token_alert.token_label")}</Label>
            <div className="flex items-center gap-2">
              <code
                data-testid="probe-token"
                className="px-2 py-1 rounded text-sm font-mono bg-green-500/20 break-all"
              >
                {probe.token}
              </code>
              <Button
                size="sm"
                variant="outline"
                onClick={() => copyToClipboard(probe.token)}
                aria-label={t("probes.token_alert.copy_token")}
              >
                <Copy className="h-4 w-4" />
              </Button>
            </div>
          </div>

          <div className="flex flex-col gap-1">
            <Label>{t("probes.token_alert.docker_label")}</Label>
            <div className="flex items-start gap-2">
              <pre className="min-w-0 flex-1 whitespace-pre-wrap break-all rounded bg-green-500/20 px-2 py-1 text-sm font-mono">
                <code data-testid="probe-docker-command">{dockerCommand}</code>
              </pre>
              <Button
                size="sm"
                variant="outline"
                onClick={() => copyToClipboard(dockerCommand)}
                aria-label={t("probes.token_alert.copy_command")}
              >
                <Copy className="h-4 w-4" />
              </Button>
            </div>
          </div>

          <div>
            <Button size="sm" variant="ghost" onClick={onDismiss}>
              {t("common.dismiss")}
            </Button>
          </div>
        </div>
      </AlertDescription>
    </Alert>
  );
};

export default ProbeTokenAlert;
