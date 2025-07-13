import { useMemo } from "react";
import type { MaintenanceModel } from "@/api/types.gen";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Trash, Clock, Calendar, Pause, Play } from "lucide-react";

const MaintenanceCard = ({
  maintenance,
  onClick,
  onDelete,
  onToggleActive,
  isPending,
}: {
  maintenance: MaintenanceModel;
  onClick: () => void;
  onDelete: () => void;
  onToggleActive: () => void;
  isPending: boolean;
}) => {
  const handleDeleteClick = (e: React.MouseEvent) => {
    e.stopPropagation(); // Prevent card click when clicking delete button
    onDelete();
  };

  const handleToggleActive = (e: React.MouseEvent) => {
    e.stopPropagation(); // Prevent card click when clicking toggle button
    onToggleActive();
  };

  const getStrategyLabel = (strategy: string) => {
    switch (strategy) {
      case "manual":
        return "Manual";
      case "single":
        return "Single Window";
      case "cron":
        return "Cron Schedule";
      case "recurring-interval":
        return "Recurring Interval";
      case "recurring-weekday":
        return "Recurring Weekday";
      case "recurring-day-of-month":
        return "Recurring Monthly";
      default:
        return strategy;
    }
  };

  const formatDate = (dateString?: string) => {
    if (!dateString) return null;
    return new Date(dateString).toLocaleString();
  };

  // Check if maintenance has ended based on end_date_time
  const isMaintenanceEnded = useMemo(() => {
    if (!maintenance.end_date_time) return false;
    const endDate = new Date(maintenance.end_date_time);
    const currentDate = new Date();
    return currentDate > endDate;
  }, [maintenance.end_date_time]);

  return (
    <Card
      key={maintenance.id}
      className="mb-2 p-2 hover:cursor-pointer light:hover:bg-gray-100 dark:hover:bg-zinc-800"
      onClick={onClick}
    >
      <CardContent className="px-2">
        <div className="flex justify-between items-center">
          <div className="flex items-center gap-4">
            <div className={`flex flex-col min-w-[200px] ${isMaintenanceEnded ? "text-gray-500" : ""}`}>
              <h3 className="font-bold mb-1">{maintenance.title}</h3>
              <div className="flex items-center gap-2">
                <Badge 
                  variant={isMaintenanceEnded ? "outline" : maintenance.active ? "default" : "secondary"}
                  className={isMaintenanceEnded ? "text-gray-500" : ""}
                >
                  {isMaintenanceEnded ? "Ended" : maintenance.active ? "Active" : "Inactive"}
                </Badge>
                <Badge 
                  variant="outline"
                  className={isMaintenanceEnded ? "text-gray-500" : ""}
                >
                  {getStrategyLabel(maintenance.strategy || "")}
                </Badge>
              </div>
              {maintenance.description && (
                <p className={`text-sm mb-2 line-clamp-2 ${isMaintenanceEnded ? "text-gray-500" : "text-muted-foreground"}`}>
                  {maintenance.description}
                </p>
              )}
              <div className="flex items-center gap-4 text-xs text-muted-foreground">
                {maintenance.start_date_time && (
                  <div className="flex items-center gap-1">
                    <Calendar className={`h-3 w-3 ${isMaintenanceEnded ? "text-gray-500" : ""}`} />
                    <span className={isMaintenanceEnded ? "text-gray-500" : ""}>
                      Start: {formatDate(maintenance.start_date_time)}
                    </span>
                  </div>
                )}
                {maintenance.end_date_time && (
                  <div className="flex items-center gap-1">
                    <Calendar className={`h-3 w-3 ${isMaintenanceEnded ? "text-gray-500" : ""}`} />
                    <span className={isMaintenanceEnded ? "text-gray-500" : ""}>End: {formatDate(maintenance.end_date_time)}</span>
                  </div>
                )}
                {maintenance.duration && (
                  <div className="flex items-center gap-1">
                    <Clock className={`h-3 w-3 ${isMaintenanceEnded ? "text-gray-500" : ""}`} />
                    <span className={isMaintenanceEnded ? "text-gray-500" : ""}>{maintenance.duration} min</span>
                  </div>
                )}
              </div>
            </div>
          </div>

          <div className="flex items-center gap-2">
            <Button
              variant="ghost"
              size="icon"
              onClick={handleToggleActive}
              className="text-blue-500 hover:text-blue-700 hover:bg-blue-50 dark:hover:bg-blue-950"
              aria-label={
                maintenance.active ? "Pause maintenance" : "Resume maintenance"
              }
              disabled={isPending}
            >
              {maintenance.active ? (
                <Pause className="h-4 w-4" />
              ) : (
                <Play className="h-4 w-4" />
              )}
            </Button>
            <Button
              variant="ghost"
              size="icon"
              onClick={handleDeleteClick}
              className="text-red-500 hover:text-red-700 hover:bg-red-50 dark:hover:bg-red-950"
              aria-label={`Delete ${maintenance.title}`}
            >
              <Trash className="h-4 w-4" />
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>
  );
};

export default MaintenanceCard;
