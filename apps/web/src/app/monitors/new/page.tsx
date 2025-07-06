import Layout from "@/layout";
import { Sheet, SheetContent } from "@/components/ui/sheet";
import { BackButton } from "@/components/back-button";
import {
  MonitorFormProvider,
  useMonitorFormContext,
} from "../context/monitor-form-context";
import CreateEditForm from "../components/create-edit-form";
import CreateNotificationChannel from "@/app/notification-channels/components/create-notification-channel";
import CreateProxy from "@/app/proxies/components/create-proxy";
import { useLocation } from "react-router-dom";

import { cloneMonitor } from "../components/monitor-registry";

import type { MonitorNavigationState } from "../types";

const NewMonitorContent = () => {
  const {
    form,
    notifierSheetOpen,
    setNotifierSheetOpen,
    proxySheetOpen,
    setProxySheetOpen,
  } = useMonitorFormContext();

  return (
    <Layout pageName="New Monitor">
      <BackButton to="/monitors" />
      <div className="flex flex-col gap-4">
        <p className="text-gray-500">
          Create a new monitor to start tracking your website's performance.
        </p>

        <CreateEditForm />
      </div>

      <Sheet open={notifierSheetOpen} onOpenChange={setNotifierSheetOpen}>
        <SheetContent
          className="p-4 overflow-y-auto"
          onInteractOutside={(event) => event.preventDefault()}
        >
          <CreateNotificationChannel
            onSuccess={(newNotifier) => {
              setNotifierSheetOpen(false);
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
          onInteractOutside={(event) => event.preventDefault()}
        >
          <CreateProxy
            onSuccess={() => {
              setProxySheetOpen(false);
            }}
          />
        </SheetContent>
      </Sheet>
    </Layout>
  );
};

const NewMonitor = () => {
  const location = useLocation();

  // Type-safe access to navigation state
  const navigationState = location.state as MonitorNavigationState | undefined;
  const cloneData = navigationState?.cloneData;

  return (
    <MonitorFormProvider mode="create" initialValues={cloneMonitor(cloneData)}>
      <NewMonitorContent />
    </MonitorFormProvider>
  );
};

export default NewMonitor;
