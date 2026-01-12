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
import { useLocalizedTranslation } from "@/hooks/useTranslation";

import { cloneMonitor } from "../components/monitor-registry";

import type { MonitorNavigationState } from "../types";
import { useOrganizationStore } from "@/store/organization";

const NewMonitorContent = () => {
    const { t } = useLocalizedTranslation();
    const {
        form,
        notifierSheetOpen,
        setNotifierSheetOpen,
        proxySheetOpen,
        setProxySheetOpen,
    } = useMonitorFormContext();
    const { currentOrganization } = useOrganizationStore();
    const slug = currentOrganization?.slug;
    const listPath = slug ? `/${slug}/monitors` : "/monitors";

    return (
        <Layout pageName={t("monitors.page.new_title")}>
            <BackButton to={listPath} />
            <div className="flex flex-col gap-4">
                <p className="text-gray-500">
                    {t("monitors.page.description")}
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
