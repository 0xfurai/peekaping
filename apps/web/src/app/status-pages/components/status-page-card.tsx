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

interface StatusPageCardProps {
  statusPage: StatusPageModel;
  onClick?: () => void;
}

const StatusPageCard = ({ statusPage, onClick }: StatusPageCardProps) => {
  const queryClient = useQueryClient();
  const [isDeleteDialogOpen, setIsDeleteDialogOpen] = useState(false);

  const deleteStatusPageMutation = useMutation({
    ...deleteStatusPagesByIdMutation({
      path: { id: statusPage.id! },
    }),
    onSuccess: () => {
      toast.success("Status page deleted successfully");
      queryClient.invalidateQueries({
        queryKey: getStatusPagesInfiniteQueryKey(),
      });
      setIsDeleteDialogOpen(false);
    },
    onError: commonMutationErrorHandler("Failed to delete status page"),
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
                  {statusPage.published ? "Published" : "Draft"}
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
                    View Page
                  </DropdownMenuItem>

                  <DropdownMenuItem
                    onClick={(e) => {
                      e.stopPropagation();
                      setIsDeleteDialogOpen(true);
                    }}
                  >
                    <Trash className="mr-2 h-4 w-4" />
                    Delete
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
            <AlertDialogTitle>Delete Status Page</AlertDialogTitle>
            <AlertDialogDescription>
              Are you sure you want to delete "{statusPage.title}"? This action
              cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleDelete}
              className="bg-red-600 hover:bg-red-700"
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
};

export default StatusPageCard;
