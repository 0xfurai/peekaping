import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  getTagsByIdQueryKey,
  postTagsMutation,
  putTagsByIdMutation,
} from "@/api/@tanstack/react-query.gen";
import { toast } from "sonner";
import { useNavigate } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Card, CardContent } from "@/components/ui/card";
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { Loader2 } from "lucide-react";
import {
  commonMutationErrorHandler,
  invalidateByPartialQueryKey,
} from "@/lib/utils";
import type { TagModel } from "@/api";

const tagSchema = z.object({
  name: z
    .string()
    .min(1, "Name is required")
    .max(100, "Name must be less than 100 characters"),
  color: z.string().regex(/^#[0-9A-F]{6}$/i, "Color must be a valid hex color"),
  description: z.string().optional(),
});

type TagFormData = z.infer<typeof tagSchema>;

interface TagFormProps {
  mode: "create" | "edit";
  tag?: TagModel;
}

const TagForm = ({ mode, tag }: TagFormProps) => {
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const form = useForm<TagFormData>({
    resolver: zodResolver(tagSchema),
    defaultValues: {
      name: tag?.name || "",
      color: tag?.color || "#3B82F6",
      description: tag?.description || "",
    },
  });

  // Create mutation
  const createMutation = useMutation({
    ...postTagsMutation(),
    onSuccess: () => {
      toast.success("Tag created successfully");
      invalidateByPartialQueryKey(queryClient, { _id: "getTags" });
      navigate("/tags");
    },
    onError: commonMutationErrorHandler("Failed to create tag"),
  });

  // Edit mutation
  const editMutation = useMutation({
    ...putTagsByIdMutation({ path: { id: tag?.id || "" } }),
    onSuccess: () => {
      toast.success("Tag updated successfully");
      invalidateByPartialQueryKey(queryClient, { _id: "getTags" });
      queryClient.removeQueries({
        queryKey: getTagsByIdQueryKey({ path: { id: tag?.id || "" } }),
      });
      navigate("/tags");
    },
    onError: commonMutationErrorHandler("Failed to update tag"),
  });

  const onSubmit = (data: TagFormData) => {
    if (mode === "create") {
      createMutation.mutate({ body: data });
    } else {
      editMutation.mutate({
        body: data,
        path: { id: tag?.id || "" },
      });
    }
  };

  const isPending = createMutation.isPending || editMutation.isPending;

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit(onSubmit)}
        className="space-y-6 max-w-[600px]"
      >
        <Card>
          <CardContent className="space-y-4">
            <FormField
              control={form.control}
              name="name"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Name</FormLabel>
                  <FormControl>
                    <Input placeholder="Enter tag name" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="color"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Color</FormLabel>
                  <FormControl>
                    <div className="flex gap-2 items-center">
                      <Input
                        type="color"
                        className="w-12 h-10 p-1 border rounded"
                        {...field}
                      />
                      <Input
                        placeholder="#3B82F6"
                        {...field}
                        className="flex-1"
                      />
                    </div>
                  </FormControl>
                  <FormDescription>Choose a color for your tag</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="description"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Description</FormLabel>
                  <FormControl>
                    <Textarea
                      placeholder="Enter tag description (optional)"
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </CardContent>
        </Card>

        <div className="flex gap-2">
          <Button
            type="button"
            variant="outline"
            onClick={() => navigate("/tags")}
            disabled={isPending}
          >
            Cancel
          </Button>
          <Button type="submit" disabled={isPending}>
            {isPending && <Loader2 className="animate-spin mr-2 h-4 w-4" />}
            {mode === "create" ? "Create Tag" : "Update Tag"}
          </Button>
        </div>
      </form>
    </Form>
  );
};

export default TagForm;
