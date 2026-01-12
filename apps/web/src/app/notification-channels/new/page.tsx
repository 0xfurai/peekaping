import Layout from "@/layout";
import { useNavigate } from "react-router-dom";
import CreateNotificationChannel from "../components/create-notification-channel";
import { BackButton } from "@/components/back-button";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useOrganizationStore } from "@/store/organization";


const NewNotificationChannel = () => {
    const navigate = useNavigate();
    const { t } = useLocalizedTranslation();
    const { currentOrganization } = useOrganizationStore();
    const slug = currentOrganization?.slug;
    const listPath = slug ? `/${slug}/notification-channels` : "/notification-channels";

    return (
        <Layout pageName={t("notifications.new.title")}>
            <BackButton to={listPath} />
            <CreateNotificationChannel onSuccess={() => navigate(listPath)} />
        </Layout>
    );
};

export default NewNotificationChannel;
