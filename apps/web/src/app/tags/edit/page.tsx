import Layout from "@/layout";
import { BackButton } from "@/components/back-button";
import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { getTagsByIdOptions } from "@/api/@tanstack/react-query.gen";
import TagForm from "../components/tag-form";
import { Skeleton } from "@/components/ui/skeleton";
import { Card, CardContent } from "@/components/ui/card";

const EditTag = () => {
  const { id } = useParams();

  const { data: tag, isLoading, error } = useQuery({
    ...getTagsByIdOptions({ path: { id: id! } }),
    enabled: !!id,
  });

  if (!id) {
    return (
      <Layout pageName="Edit Tag">
        <BackButton to="/tags" />
        <div className="text-red-500">Tag ID is required</div>
      </Layout>
    );
  }

  if (isLoading) {
    return (
      <Layout pageName="Edit Tag">
        <BackButton to="/tags" />
        <div className="flex flex-col gap-4">
          <p className="text-gray-500">Loading tag...</p>
          <Card>
            <CardContent className="space-y-4 pt-6">
              <Skeleton className="h-4 w-1/4" />
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-4 w-1/4" />
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-4 w-1/4" />
              <Skeleton className="h-20 w-full" />
            </CardContent>
          </Card>
        </div>
      </Layout>
    );
  }

  if (error || !tag?.data) {
    return (
      <Layout pageName="Edit Tag">
        <BackButton to="/tags" />
        <div className="text-red-500">
          Failed to load tag. The tag may not exist or you may not have permission to view it.
        </div>
      </Layout>
    );
  }

  return (
    <Layout pageName={`Edit Tag: ${tag.data.name}`}>
      <BackButton to="/tags" />
      <div className="flex flex-col gap-4">
        <p className="text-gray-500">
          Update the tag details to better organize your monitors.
        </p>

        <TagForm mode="edit" tag={tag.data} />
      </div>
    </Layout>
  );
};

export default EditTag;
