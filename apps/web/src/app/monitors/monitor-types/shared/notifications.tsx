import { Button } from "@/components/ui/button";
import { FormControl, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/form";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { TypographyH4 } from "@/components/ui/typography";
import { useQuery } from "@tanstack/react-query";
import { useFormContext } from "react-hook-form";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import type { NotificationChannelModel } from "@/api/types.gen";
import { allNotificationChannelsOptions, availableChannels } from "./channel-support";

// What stands in the place of the channel select when the channels could not be loaded, so that
// the form doesn't say there are none
const ChannelsLoadError = ({ onRetry }: { onRetry: () => void }) => {
  const { t } = useLocalizedTranslation();
  return (
    <div role="alert" className="flex flex-1 items-center gap-2 text-sm text-destructive">
      <span>{t("monitors.form.shared.notifications.load_failed")}</span>
      <Button type="button" variant="outline" size="sm" onClick={onRetry}>
        {t("common.retry")}
      </Button>
    </div>
  );
};

// The channels ticked on the form, each with a button to take it off
const SelectedChannels = ({
  ids,
  channels,
}: {
  ids: unknown;
  channels: NotificationChannelModel[] | null | undefined;
}) => {
  const { t } = useLocalizedTranslation();
  const form = useFormContext();
  if (!Array.isArray(ids) || ids.length === 0) return null;
  return (
    <>
      <Label>{t("monitors.form.shared.notifications.selected_channels")}</Label>
      <div className="flex flex-col gap-1 mb-2">
        {ids.map((id: string) => {
          const notification = channels?.find((n) => n.id === id);
          if (!notification) {
            console.log("Notification not found to render in list", id);
            return null;
          }
          return (
            <div
              key={id}
              className="flex items-center justify-between bg-muted rounded px-3 py-1"
            >
              <span>{notification.name}</span>
              <Button
                type="button"
                size="icon"
                variant="ghost"
                onClick={() => {
                  const newIds = ids.filter((nid) => nid !== id);
                  form.setValue("notification_ids", newIds, {
                    shouldDirty: true,
                  });
                }}
                aria-label={`Remove ${notification.name}`}
              >
                ×
              </Button>
            </div>
          );
        })}
      </div>
    </>
  );
};

// Without onNewNotifier the "new channel" button is left out, for forms that have no
// place to create a channel.
const Notifications = ({ onNewNotifier }: { onNewNotifier?: () => void }) => {
  const { t } = useLocalizedTranslation();
  const form = useFormContext();
  const notification_ids = form.watch("notification_ids");

  // Every channel, so that a ticked one past the list's first page is shown and can be chosen
  const { data: notifications, isError, refetch } = useQuery({
    ...allNotificationChannelsOptions(),
  });
  // A refresh that failed after the channels were read still has them to show
  const loadFailed = isError && !notifications;
  const availableNotifiers = availableChannels(notifications?.data, notification_ids || []);
  const hasChannels = (notifications?.data?.length ?? 0) > 0;

  return (
    <div className="flex flex-col gap-2">
      <TypographyH4 className="mb-2">{t("monitors.form.shared.notifications.title")}</TypographyH4>

      <SelectedChannels ids={notification_ids} channels={notifications?.data} />

      <div className="flex gap-2 flex-col sm:flex-row sm:items-center">
        {loadFailed ? (
          <ChannelsLoadError onRetry={() => void refetch()} />
        ) : (
          <FormField
            control={form.control}
            name="notification_ids"
            render={({ field }) => (
              <FormItem className="flex-1">
                <FormLabel className="pb-1">
                  {hasChannels
                    ? t("monitors.form.shared.notifications.add_notifier")
                    : t("monitors.form.shared.notifications.no_channels")}
                </FormLabel>
                {/* FormControl goes on the trigger: the Select root renders no
                    element, so the label's id and aria attributes would be lost. */}
                <Select
                  value={"none"}
                  onValueChange={(val) => {
                    if (!val || val === "none") return;
                    const current = notification_ids || [];
                    if (!current.includes(val)) {
                      field.onChange([...current, val], {
                        shouldDirty: true,
                      });
                    }
                  }}
                >
                  <FormControl>
                    <SelectTrigger className="w-full">
                      <SelectValue placeholder={t("monitors.form.shared.notifications.select_notifier_placeholder")} />
                    </SelectTrigger>
                  </FormControl>

                  <SelectContent>
                    <SelectItem value="none" disabled>
                      {hasChannels
                        ? t("monitors.form.shared.notifications.select_channel")
                        : t("monitors.form.shared.notifications.no_channels_available")}
                    </SelectItem>
                    {availableNotifiers.map((n) => (
                      <SelectItem key={n.id} value={n.id || "none"}>
                        <span className="truncate max-w-[300px] sm:max-w-full">
                          {n.name}
                        </span>
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <FormMessage />
              </FormItem>
            )}
          />
        )}
        {onNewNotifier && (
          <Button
            type="button"
            onClick={onNewNotifier}
            variant="outline"
            className="self-start sm:self-end"
          >
            {t("monitors.form.shared.notifications.new_notifier")}
          </Button>
        )}
      </div>
    </div>
  );
};

export default Notifications;
