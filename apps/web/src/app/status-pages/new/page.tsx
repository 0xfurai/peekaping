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
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const NewStatusPageContent = () => {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const { t } = useLocalizedTranslation();

  const createStatusPageMutation = useMutation({
    ...postStatusPagesMutation(),
    onSuccess: () => {
      toast.success(t("status_pages.messages.created_successfully"));
      queryClient.invalidateQueries({
        queryKey: getStatusPagesInfiniteQueryKey(),
      });
      navigate("/status-pages");
    },
    onError: commonMutationErrorHandler(t("status_pages.messages.create_failed")),
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
    <Layout pageName={t("status_pages.new_page_name")}>
      <BackButton to="/status-pages" />
      <div className="flex flex-col gap-4">
        <p className="text-gray-500">
          {t("status_pages.messages.create_description")}
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
