import Layout from "@/layout";
import { useNavigate } from "react-router-dom";
import CreateNotificationChannel from "../components/create-notification-channel";
import { BackButton } from "@/components/back-button";

const NewNotificationChannel = () => {
  const navigate = useNavigate();

  return (
    <Layout pageName="New Notification Channel">
      <BackButton to="/notification-channels" />
      <CreateNotificationChannel onSuccess={() => navigate("/notification-channels")} />
    </Layout>
  );
};

export default NewNotificationChannel;
