import Layout from "@/layout";
import { useQueryClient } from "@tanstack/react-query";
import { useParams } from "react-router-dom";
import { Sheet, SheetContent } from "@/components/ui/sheet";
import {
  MonitorFormProvider,
  useMonitorFormContext,
} from "../context/monitor-form-context";
import {
  getNotificationChannelsQueryKey,
  getProxiesQueryKey,
} from "@/api/@tanstack/react-query.gen";
import CreateEditForm from "../components/create-edit-form";
import type { NotificationChannelModel, ProxyModel } from "@/api";
import CreateProxy from "@/app/proxies/components/create-proxy";
import CreateNotificationChannel from "@/app/notification-channels/components/create-notification-channel";
import { BackButton } from "@/components/back-button";

const EditMonitorForm = () => {
  const {
    form,
    notifierSheetOpen,
    setNotifierSheetOpen,
    proxySheetOpen,
    setProxySheetOpen,
    monitor,
  } = useMonitorFormContext();
  const queryClient = useQueryClient();

  if (!monitor) return null;

  return (
    <Layout pageName={`Edit Monitor: ${monitor?.data?.name}`}>
      <BackButton to={`/monitors/${monitor?.data?.id}`} />
      <div className="flex flex-col gap-4">
        <p className="text-gray-500">
          Create a new monitor to start tracking your website's performance.
        </p>

        <CreateEditForm />
      </div>

      <Sheet open={notifierSheetOpen} onOpenChange={setNotifierSheetOpen}>
        <SheetContent
          className="p-4 overflow-y-auto"
          onInteractOutside={(e) => e.preventDefault()}
        >
          <CreateNotificationChannel
            onSuccess={(newNotifier: NotificationChannelModel) => {
              setNotifierSheetOpen(false);
              queryClient.invalidateQueries({
                queryKey: getNotificationChannelsQueryKey(),
              });
              form.setValue("notification_ids", [
                ...(form.getValues("notification_ids") || []),
                newNotifier.id!,
              ]);
            }}
          />
        </SheetContent>
      </Sheet>

      <Sheet open={proxySheetOpen} onOpenChange={setProxySheetOpen}>
        <SheetContent
          className="p-4 overflow-y-auto"
          onInteractOutside={(e) => e.preventDefault()}
        >
          <CreateProxy
            onSuccess={(newProxy: ProxyModel) => {
              setProxySheetOpen(false);
              queryClient.invalidateQueries({ queryKey: getProxiesQueryKey() });
              form.setValue("proxy_id", newProxy.id);
            }}
          />
        </SheetContent>
      </Sheet>
    </Layout>
  );
};

const EditMonitor = () => {
  const { id } = useParams();

  if (!id) return null;

  return (
    <MonitorFormProvider mode="edit" monitorId={id}>
      <EditMonitorForm />
    </MonitorFormProvider>
  );
};

export default EditMonitor;
