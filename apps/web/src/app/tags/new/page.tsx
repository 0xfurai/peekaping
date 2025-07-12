import Layout from "@/layout";
import { BackButton } from "@/components/back-button";
import TagForm from "../components/tag-form";

const NewTag = () => {
  return (
    <Layout pageName="New Tag">
      <BackButton to="/tags" />
      <div className="flex flex-col gap-4">
        <p className="text-gray-500">
          Create a new tag to organize and categorize your monitors.
        </p>

        <TagForm mode="create" />
      </div>
    </Layout>
  );
};

export default NewTag;
