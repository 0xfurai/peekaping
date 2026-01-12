import Layout from "@/layout";
import { useNavigate } from "react-router-dom";
import CreateProxy from "../components/create-proxy";
import { BackButton } from "@/components/back-button";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useOrganizationStore } from "@/store/organization";


const NewProxy = () => {
    const navigate = useNavigate();
    const { t } = useLocalizedTranslation();
    const { currentOrganization } = useOrganizationStore();
    const slug = currentOrganization?.slug;
    const listPath = slug ? `/${slug}/proxies` : "/proxies";

    return (
        <Layout pageName={t("proxies.new.page_name")}>
            <BackButton to={listPath} />
            <CreateProxy onSuccess={() => navigate(listPath)} />
        </Layout>
    );
};

export default NewProxy;
