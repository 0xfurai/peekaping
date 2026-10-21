import { useLocalizedTranslation } from "@/hooks/useTranslation";

const TELEMETRY_DOCS_URL = "https://docs.peekaping.com/self-hosting/telemetry";

// A link to the docs page that says what the usage ping and the error reports send and how to
// turn them off. A section is the id of a heading on that page, such as "error-reporting".
export function TelemetryDocsLink({ section }: { section?: string }) {
  const { t } = useLocalizedTranslation();

  return (
    <a
      href={section ? `${TELEMETRY_DOCS_URL}#${section}` : TELEMETRY_DOCS_URL}
      target="_blank"
      rel="noopener noreferrer"
      className="underline text-blue-600"
    >
      {t("telemetry.docs_link")}
    </a>
  );
}
