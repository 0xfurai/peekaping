import Layout from "@/layout";
import { useNavigate } from "react-router-dom";
import CreateProxy from "../components/create-proxy";
import { BackButton } from "@/components/back-button";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useWorkspacePath } from "@/hooks/useWorkspacePath";

const NewProxy = () => {
  const navigate = useNavigate();
  const workspacePath = useWorkspacePath();
  const { t } = useLocalizedTranslation();

  return (
    <Layout pageName={t("proxies.new.page_name")}>
      <BackButton to="/proxies" />
      <CreateProxy onSuccess={() => navigate(workspacePath("/proxies"))} />
    </Layout>
  );
};

export default NewProxy;
