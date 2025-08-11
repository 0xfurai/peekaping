import type { NotificationChannelModel } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Trash } from "lucide-react";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const NotificationChannelCard = ({
  notifier,
  onClick,
  onDelete,
}: {
  notifier: NotificationChannelModel;
  onClick: () => void;
  onDelete?: () => void;
}) => {
  const { t } = useLocalizedTranslation();
  const handleDeleteClick = (e: React.MouseEvent) => {
    e.stopPropagation(); // Prevent card click when clicking delete button
    onDelete?.();
  };

  return (
    <Card
      key={notifier.id}
      className="mb-2 p-2 hover:cursor-pointer light:hover:bg-gray-100 dark:hover:bg-zinc-800"
      onClick={onClick}
    >
      <CardContent className="px-2">
        <div className="flex justify-between items-center">
          <div className="flex items-center gap-4">
            <div className="flex flex-col min-w-[100px]">
              <h3 className="font-bold mb-1">{notifier.name}</h3>
              <Badge variant={"outline"}>{notifier.type}</Badge>
            </div>
          </div>

          {onDelete && (
            <Button
              variant="ghost"
              size="icon"
              onClick={handleDeleteClick}
              className="text-red-500 hover:text-red-700 hover:bg-red-50 dark:hover:bg-red-950"
              aria-label={t("notifications.aria.delete", { name: notifier.name })}
            >
              <Trash className="h-4 w-4" />
            </Button>
          )}
        </div>
      </CardContent>
    </Card>
  );
};

export default NotificationChannelCard;
