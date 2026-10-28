import Layout from "@/layout";
import { useNavigate } from "react-router-dom";
import CreateNotificationChannel from "../components/create-notification-channel";
import { BackButton } from "@/components/back-button";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useWorkspacePath } from "@/hooks/useWorkspacePath";

const NewNotificationChannel = () => {
  const navigate = useNavigate();
  const workspacePath = useWorkspacePath();
  const { t } = useLocalizedTranslation();

  return (
    <Layout pageName={t("notifications.new.title")}>
      <BackButton to="/notification-channels" />
      <CreateNotificationChannel onSuccess={() => navigate(workspacePath("/notification-channels"))} />
    </Layout>
  );
};

export default NewNotificationChannel;
