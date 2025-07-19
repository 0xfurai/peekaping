import { getNotificationChannelsOptions } from "@/api/@tanstack/react-query.gen";
import { Button } from "@/components/ui/button";
import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { TypographyH4 } from "@/components/ui/typography";
import { useQuery } from "@tanstack/react-query";
import { useFormContext } from "react-hook-form";
import { z } from "zod";

export const notificationsSchema = z.object({
  notification_ids: z.array(z.string()),
});

export const notificationsDefaultValues = {
  notification_ids: [],
};

const Notifications = ({ onNewNotifier }: { onNewNotifier: () => void }) => {
  const form = useFormContext();
  const notification_ids = form.watch("notification_ids");

  const { data: notifications } = useQuery({
    ...getNotificationChannelsOptions(),
  });

  return (
    <div className="flex flex-col gap-2">
      <TypographyH4 className="mb-2">Notification channels</TypographyH4>

      {Array.isArray(notification_ids) && notification_ids.length > 0 && (
        <>
          <Label>Selected Notification channels</Label>
          <div className="flex flex-col gap-1 mb-2">
            {notification_ids.map((id: string) => {
              const notification = notifications?.data?.find(
                (n) => n.id === id
              );
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
                      const newIds = notification_ids.filter(
                        (nid) => nid !== id
                      );
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
      )}

      <div className="flex gap-2 flex-col sm:flex-row sm:items-center">
        <FormField
          control={form.control}
          name="notification_ids"
          render={({ field }) => {
            const availableNotifiers =
              notifications?.data?.filter(
                (n) => !(notification_ids || []).includes(n.id)
              ) || [];

            return (
              <FormItem className="flex-1">
                <FormLabel className="pb-1">
                  {notifications?.data?.length || 0
                    ? "Add Notifier"
                    : "No notification channels found, create one first"}
                </FormLabel>
                <FormControl>
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
                    <SelectTrigger className="w-full">
                      <SelectValue placeholder="Select Notifier" />
                    </SelectTrigger>

                    <SelectContent>
                      <SelectItem value="none" disabled>
                        {(notifications?.data?.length || 0) > 0
                          ? "Select channel"
                          : "No channels available"}
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
                </FormControl>
                <FormMessage />
              </FormItem>
            );
          }}
        />
        <Button
          type="button"
          onClick={onNewNotifier}
          variant="outline"
          className="self-start sm:self-end"
        >
          + New Notifier
        </Button>
      </div>
    </div>
  );
};

export default Notifications;
