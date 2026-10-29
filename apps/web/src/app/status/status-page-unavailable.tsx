import { CircleSlash } from "lucide-react";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

// What a visitor sees of a public status page whose workspace is suspended (PP-372): a neutral
// "unavailable", which says neither that the page is missing nor why it is held back.
export function StatusPageUnavailable() {
  const { t } = useLocalizedTranslation();
  return (
    <div className="min-h-screen bg-background p-4">
      <div className="max-w-4xl mx-auto flex flex-col items-center gap-3 py-24 text-center">
        <CircleSlash className="h-10 w-10 text-muted-foreground" />
        <p className="text-lg font-medium">{t("status.messages.status_page_unavailable")}</p>
      </div>
    </div>
  );
}
