import Layout from "@/layout";
import { useNavigate } from "react-router-dom";
import CreateProxy from "../components/create-proxy";
import { BackButton } from "@/components/back-button";

const NewProxy = () => {
  const navigate = useNavigate();

  return (
    <Layout pageName="New Proxy">
      <BackButton to="/proxies" />
      <CreateProxy onSuccess={() => navigate("/proxies")} />
    </Layout>
  );
};

export default NewProxy;
