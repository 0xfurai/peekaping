import { TypographyH4 } from "@/components/ui/typography";
import { Button } from "@/components/ui/button";
import { useEffect, useState } from "react";
import { Card, CardContent } from "@/components/ui/card";
import Intervals from "../shared/intervals";
import General from "../shared/general";
import Notifications from "../shared/notifications";
import Tags from "../shared/tags";
import Probe from "../shared/probe";
import { useMonitorFormContext } from "../../context/monitor-form-context";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

// Generate a random 24-character alphanumeric token
const generateToken = () =>
  Array.from({ length: 24 }, () => Math.random().toString(36)[2] || "x").join(
    ""
  );

const PushForm = () => {
  const { form, mode, setNotifierSheetOpen } = useMonitorFormContext();
  const pushToken = form.watch("pushToken");
  const { t } = useLocalizedTranslation();

  // A new monitor gets its own token, also when it is a clone: the token identifies the monitor
  // the push endpoint delivers to, so two monitors can't share one. An edited monitor keeps its.
  useEffect(() => {
    if (mode === "create" || !pushToken) {
      form.setValue("pushToken", generateToken(), { shouldDirty: true });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const [copied, setCopied] = useState(false);
  const pushUrl = `${window.location.origin}/api/v1/push/${pushToken}?status=up&msg=OK&ping=`;

  const handleCopy = () => {
    navigator.clipboard.writeText(pushUrl);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };

  const handleRegenerate = () => {
    const newToken = generateToken();
    form.setValue("pushToken", newToken, { shouldDirty: true });
    setCopied(false);
  };

  return (
    <>
      <Card>
        <CardContent className="space-y-4">
          <General />
        </CardContent>
      </Card>

      <Card>
        <CardContent className="space-y-4">
          <TypographyH4>{t("monitors.form.push.url_title")}</TypographyH4>
          <div className="text-muted-foreground mb-2">
            {t("monitors.form.push.url_description")}
          </div>
          <div className="flex items-center gap-2 bg-muted rounded px-3 py-2">
            <span className="break-all font-mono text-sm">{pushUrl}</span>
          </div>
          <div className="flex gap-2 mt-2">
            <Button
              size="sm"
              variant="outline"
              type="button"
              onClick={handleCopy}
            >
              {copied ? t("monitors.form.buttons.copied") : t("monitors.form.buttons.copy")}
            </Button>
            <Button
              size="sm"
              variant="secondary"
              type="button"
              onClick={handleRegenerate}
            >
              {t("monitors.form.buttons.regenerate")}
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardContent className="space-y-4">
          <Tags />
          <Probe />
        </CardContent>
      </Card>

      <Card>
        <CardContent className="space-y-4">
          <Notifications onNewNotifier={() => setNotifierSheetOpen(true)} />
        </CardContent>
      </Card>

      <Card>
        <CardContent className="space-y-4">
          <Intervals />
        </CardContent>
      </Card>
    </>
  );
};

export default PushForm;
