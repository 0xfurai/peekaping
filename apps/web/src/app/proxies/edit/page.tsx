import { useParams, useNavigate } from "react-router-dom";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { BackButton } from "@/components/back-button";
import {
  getProxiesByIdOptions,
  putProxiesByIdMutation,
  getProxiesInfiniteQueryKey,
  getProxiesByIdQueryKey,
} from "@/api/@tanstack/react-query.gen";
import Layout from "@/layout";
import { toast } from "sonner";
import CreateEditProxy from "../components/create-edit-proxy";
import type { ProxyCreateUpdateDto } from "@/api/types.gen";
import { type ProxyForm } from "../components/create-edit-proxy";
import { commonMutationErrorHandler } from "@/lib/utils";

const EditProxy = () => {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const { data, isLoading, error } = useQuery({
    ...getProxiesByIdOptions({ path: { id: id! } }),
    enabled: !!id,
  });

  const mutation = useMutation({
    ...putProxiesByIdMutation(),
    onSuccess: () => {
      toast.success("Proxy updated successfully");

      queryClient.invalidateQueries({
        queryKey: getProxiesInfiniteQueryKey()
      });

      queryClient.removeQueries({
        queryKey: getProxiesByIdQueryKey({
          path: {
            id: id!
          }
        })
      });

      navigate("/proxies");
    },
    onError: commonMutationErrorHandler("Failed to update proxy"),
  });

  if (isLoading) return <Layout pageName="Edit Proxy">Loading...</Layout>;
  if (error || !data?.data)
    return <Layout pageName="Edit Proxy">Error loading proxy</Layout>;

  // Prepare initial values for the form
  const proxy = data.data;

  const initialValues = {
    protocol: proxy.protocol as "http" | "https" | "socks" | "socks5" | "socks5h" | "socks4",
    host: proxy.host || "",
    port: proxy.port || 1,
    auth: proxy.auth || false,
    username: proxy.username || "",
    password: proxy.password || "",
  };

  const handleSubmit = (formData: ProxyForm) => {
    const proxyData: ProxyCreateUpdateDto = {
      protocol: formData.protocol,
      host: formData.host,
      port: formData.port,
      auth: formData.auth,
      username: formData.auth ? formData.username : undefined,
      password: formData.auth ? formData.password : undefined,
    };

    mutation.mutate({
      path: { id: id! },
      body: proxyData,
    });
  };

  return (
    <Layout pageName={`Edit Proxy: ${proxy.host}:${proxy.port}`}>
      <BackButton to="/proxies" />
      <CreateEditProxy
        initialValues={initialValues}
        onSubmit={handleSubmit}
        mode="edit"
      />
    </Layout>
  );
};

export default EditProxy;
