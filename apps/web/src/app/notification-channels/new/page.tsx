import Layout from "@/layout";
import { useNavigate } from "react-router-dom";
import CreateNotificationChannel from "../components/create-notification-channel";
import { BackButton } from "@/components/back-button";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const NewNotificationChannel = () => {
  const navigate = useNavigate();
  const { t } = useLocalizedTranslation();

  return (
    <Layout pageName={t("notifications.new.title")}>
      <BackButton to="/notification-channels" />
      <CreateNotificationChannel onSuccess={() => navigate("/notification-channels")} />
    </Layout>
  );
};

export default NewNotificationChannel;
