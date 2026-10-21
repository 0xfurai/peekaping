import { useLocalizedTranslation } from "@/hooks/useTranslation";

const TELEMETRY_DOCS_URL = "https://docs.peekaping.com/self-hosting/telemetry";

// A link to the docs page that says what the usage ping sends and how to turn it off
export function TelemetryDocsLink() {
  const { t } = useLocalizedTranslation();

  return (
    <a
      href={TELEMETRY_DOCS_URL}
      target="_blank"
      rel="noopener noreferrer"
      className="underline text-blue-600"
    >
      {t("telemetry.docs_link")}
    </a>
  );
}
