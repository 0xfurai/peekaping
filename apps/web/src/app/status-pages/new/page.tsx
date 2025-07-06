import Layout from "@/layout";
import CreateEditForm, { type StatusPageForm } from "../components/create-edit-form";
import { BackButton } from "@/components/back-button";
import {
  getStatusPagesInfiniteQueryKey,
  postStatusPagesMutation,
} from "@/api/@tanstack/react-query.gen";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { useNavigate } from "react-router-dom";
import { commonMutationErrorHandler } from "@/lib/utils";

const NewStatusPageContent = () => {
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  const createStatusPageMutation = useMutation({
    ...postStatusPagesMutation(),
    onSuccess: () => {
      toast.success("Status page created successfully");
      queryClient.invalidateQueries({
        queryKey: getStatusPagesInfiniteQueryKey(),
      });
      navigate("/status-pages");
    },
    onError: commonMutationErrorHandler("Failed to create status page"),
  });

  const handleSubmit = (data: StatusPageForm) => {
    const { monitors, ...rest } = data;
    createStatusPageMutation.mutate({
      body: {
        ...rest,
        monitor_ids: monitors?.map((monitor) => monitor.value),
      },
    });
  };

  return (
    <Layout pageName="New Status Page">
      <BackButton to="/status-pages" />
      <div className="flex flex-col gap-4">
        <p className="text-gray-500">
          Create a new status page to share your service status with users.
        </p>

        <CreateEditForm
          onSubmit={handleSubmit}
          isPending={createStatusPageMutation.isPending}
        />
      </div>
    </Layout>
  );
};

export default NewStatusPageContent;
