import Layout from "@/layout";
import CreateEditForm, { type StatusPageForm } from "../components/create-edit-form";
import { useNavigate, useParams } from "react-router-dom";
import { BackButton } from "@/components/back-button";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  getMonitorsBatchOptions,
  getStatusPagesByIdOptions,
  getStatusPagesByIdQueryKey,
  getStatusPagesInfiniteQueryKey,
  patchStatusPagesByIdMutation,
} from "@/api/@tanstack/react-query.gen";
import { toast } from "sonner";
import { commonMutationErrorHandler } from "@/lib/utils";

const EditStatusPageContent = () => {
  const { id: statusPageId } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const { data: statusPage, isLoading: statusPageIsLoading } = useQuery({
    ...getStatusPagesByIdOptions({ path: { id: statusPageId! } }),
    enabled: !!statusPageId,
  });

  const editStatusPageMutation = useMutation({
    ...patchStatusPagesByIdMutation({
      path: {
        id: statusPageId!,
      },
    }),
    onSuccess: () => {
      toast.success("Status page updated successfully");
      queryClient.invalidateQueries({
        queryKey: getStatusPagesInfiniteQueryKey(),
      });
      queryClient.removeQueries({
        queryKey: getStatusPagesByIdQueryKey({ path: { id: statusPageId! } }),
      });
      navigate("/status-pages");
    },
    onError: commonMutationErrorHandler("Failed to update status page"),
  });

  const handleSubmit = (data: StatusPageForm) => {
    const { monitors, ...rest } = data;
    editStatusPageMutation.mutate({
      body: {
        ...rest,
        monitor_ids: monitors?.map((monitor) => monitor.value),
      },
      path: { id: statusPageId! },
    });
  };

  const { data: monitorsData, isLoading: monitorsDataIsLoading } = useQuery({
    ...getMonitorsBatchOptions({
      query: {
        ids: statusPage?.data?.monitor_ids?.join(",") || "",
      },
    }),
    enabled: !!statusPage?.data?.monitor_ids?.length,
  });

  if (statusPageIsLoading || monitorsDataIsLoading) {
    return (
      <Layout pageName="Edit Status Page">
        <div>Loading...</div>
      </Layout>
    );
  }

  if (!statusPage?.data) {
    return (
      <Layout pageName="Edit Status Page">
        <div>Status page not found</div>
      </Layout>
    );
  }

  const statusPageData = statusPage?.data;

  return (
    <Layout pageName={`Edit Status Page: ${statusPageData.title}`}>
      <BackButton to="/status-pages" />
      <div className="flex flex-col gap-4">
        <p className="text-gray-500">
          Update your status page settings and configuration.
        </p>

        <CreateEditForm
          mode="edit"
          onSubmit={handleSubmit}
          initialValues={{
            title: statusPageData.title || "",
            slug: statusPageData.slug || "",
            description: statusPageData.description || "",
            icon: statusPageData.icon || "",
            footer_text: statusPageData.footer_text || "",
            auto_refresh_interval: statusPageData?.auto_refresh_interval || 0,
            published: statusPageData?.published || true,
            monitors: monitorsData?.data?.map((monitor) => ({
              label: monitor.name || "",
              value: monitor.id || "",
            })),
          }}
        />
      </div>
    </Layout>
  );
};

export default EditStatusPageContent;
