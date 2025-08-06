import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { MoreHorizontal, ExternalLink, Trash } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { type StatusPageModel } from "@/api";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  deleteStatusPagesByIdMutation,
  getStatusPagesInfiniteQueryKey,
} from "@/api/@tanstack/react-query.gen";
import { toast } from "sonner";
import { useState } from "react";
import { commonMutationErrorHandler } from "@/lib/utils";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

interface StatusPageCardProps {
  statusPage: StatusPageModel;
  onClick?: () => void;
}

const StatusPageCard = ({ statusPage, onClick }: StatusPageCardProps) => {
  const queryClient = useQueryClient();
  const [isDeleteDialogOpen, setIsDeleteDialogOpen] = useState(false);
  const { t } = useLocalizedTranslation();

  const deleteStatusPageMutation = useMutation({
    ...deleteStatusPagesByIdMutation({
      path: { id: statusPage.id! },
    }),
    onSuccess: () => {
      toast.success(t("status_pages.messages.deleted_successfully"));
      queryClient.invalidateQueries({
        queryKey: getStatusPagesInfiniteQueryKey(),
      });
      setIsDeleteDialogOpen(false);
    },
    onError: commonMutationErrorHandler(t("status_pages.messages.delete_failed")),
  });

  const handleView = (e: React.MouseEvent<HTMLDivElement>) => {
    e.stopPropagation();

    if (statusPage.slug) {
      window.open(`/status/${statusPage.slug}`, "_blank");
    }
  };

  const handleDelete = () => {
    if (statusPage.id) {
      deleteStatusPageMutation.mutate({
        path: { id: statusPage.id },
      });
    }
  };

  return (
    <>
      <Card
        className="mb-2 p-2 hover:cursor-pointer light:hover:bg-gray-100 dark:hover:bg-zinc-800"
        onClick={onClick}
      >
        <CardContent className="px-2">
          <div className="flex justify-between">
            <div className="flex items-center">
              <div className="text-sm text-gray-500 mr-4 min-w-[60px]">
                <Badge variant={statusPage.published ? "default" : "secondary"}>
                  {statusPage.published ? t("status_pages.published_status") : t("status_pages.draft_status")}
                </Badge>
              </div>

              <div className="flex flex-col min-w-[100px]">
                <h3 className="font-bold mb-1">{statusPage.title}</h3>
                <Badge variant="outline">
                  {"/status/" + statusPage.slug || "No slug"}
                </Badge>
              </div>
            </div>

            <div className="flex items-center">
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="ghost" size="sm">
                    <MoreHorizontal className="h-4 w-4" />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem onClick={handleView}>
                    <ExternalLink className="mr-2 h-4 w-4" />
                    {t("status_pages.view_page")}
                  </DropdownMenuItem>

                  <DropdownMenuItem
                    onClick={(e) => {
                      e.stopPropagation();
                      setIsDeleteDialogOpen(true);
                    }}
                  >
                    <Trash className="mr-2 h-4 w-4" />
                    {t("common.delete")}
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          </div>
        </CardContent>
      </Card>

      <AlertDialog
        open={isDeleteDialogOpen}
        onOpenChange={setIsDeleteDialogOpen}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("status_pages.delete_dialog.title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("status_pages.delete_dialog.description", {
                title: statusPage.title,
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleDelete}
              className="bg-red-600 hover:bg-red-700"
            >
              {t("common.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
};

export default StatusPageCard;
