import Layout from "@/layout";
import { useNavigate } from "react-router-dom";
import CreateProxy from "../components/create-proxy";
import { BackButton } from "@/components/back-button";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const NewProxy = () => {
  const navigate = useNavigate();
  const { t } = useLocalizedTranslation();

  return (
    <Layout pageName={t("proxies.new.page_name")}>
      <BackButton to="/proxies" />
      <CreateProxy onSuccess={() => navigate("/proxies")} />
    </Layout>
  );
};

export default NewProxy;
