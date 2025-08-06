import Layout from "@/layout";
import { BackButton } from "@/components/back-button";
import TagForm from "../components/tag-form";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const NewTag = () => {
  const { t } = useLocalizedTranslation();

  return (
    <Layout pageName={t("tags.new_page_name")}>
      <BackButton to="/tags" />
      <div className="flex flex-col gap-4">
        <p className="text-gray-500">
          {t("tags.messages.create_description")}
        </p>

        <TagForm mode="create" />
      </div>
    </Layout>
  );
};

export default NewTag;
