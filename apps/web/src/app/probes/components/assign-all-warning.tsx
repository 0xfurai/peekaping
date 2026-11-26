import { useLocalizedTranslation } from "@/hooks/useTranslation";

const TOKEN_DOCS_URL =
  "https://docs.peekaping.com/self-hosting/remote-probes#what-the-token-can-read";

// What assigning a probe to every monitor costs: its token reads each of their configs. Shown
// where the choice is made, in the create form and in the confirm dialog of the Probes page.
const AssignAllWarning = () => {
  const { t } = useLocalizedTranslation();

  return (
    <>
      {t("probes.assign_all.warning")}{" "}
      <a
        href={TOKEN_DOCS_URL}
        target="_blank"
        rel="noopener noreferrer"
        className="underline text-blue-600"
      >
        {t("probes.assign_all.docs_link")}
      </a>
    </>
  );
};

export default AssignAllWarning;
