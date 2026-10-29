import Layout from "@/layout";
import { useNavigate, useSearchParams } from "react-router-dom";
import CreateNotificationChannel from "../components/create-notification-channel";
import { BackButton } from "@/components/back-button";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useWorkspacePath } from "@/hooks/useWorkspacePath";
import { createsDefaultChannel } from "../channel-payload";

const NewNotificationChannel = () => {
  const navigate = useNavigate();
  const workspacePath = useWorkspacePath();
  const { t } = useLocalizedTranslation();
  const [searchParams] = useSearchParams();
  const isDefault = createsDefaultChannel(searchParams);

  return (
    <Layout pageName={t("notifications.new.title")}>
      <BackButton to="/notification-channels" />
      <CreateNotificationChannel
        isDefault={isDefault}
        onSuccess={() => navigate(workspacePath("/notification-channels"))}
      />
      {isDefault && <p className="text-muted-foreground mt-4 text-sm">{t("notifications.new.default_hint")}</p>}
    </Layout>
  );
};

export default NewNotificationChannel;
