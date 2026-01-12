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
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const tagSchema = z.object({
    name: z
        .string()
        .min(1, "tags.validation.name_required")
        .max(100, "tags.validation.name_max_length"),
    color: z.string().regex(/^#[0-9A-F]{6}$/i, "tags.validation.color_invalid"),
    description: z.string().optional(),
});

type TagFormData = z.infer<typeof tagSchema>;

interface TagFormProps {
    mode: "create" | "edit";
    tag?: TagModel;
}

import { useOrganizationStore } from "@/store/organization";

const TagForm = ({ mode, tag }: TagFormProps) => {
    const { t } = useLocalizedTranslation();
    const navigate = useNavigate();
    const queryClient = useQueryClient();
    const { currentOrganization } = useOrganizationStore();
    const slug = currentOrganization?.slug;
    const listPath = slug ? `/${slug}/tags` : "/tags";

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
            toast.success(t("messages.tag_create_success"));
            invalidateByPartialQueryKey(queryClient, { _id: "getTags" });
            navigate(listPath);
        },
        onError: commonMutationErrorHandler(t("messages.tag_create_error")),
    });

    // Edit mutation
    const editMutation = useMutation({
        ...putTagsByIdMutation({ path: { id: tag?.id || "" } }),
        onSuccess: () => {
            toast.success(t("messages.tag_update_success"));
            invalidateByPartialQueryKey(queryClient, { _id: "getTags" });
            queryClient.removeQueries({
                queryKey: getTagsByIdQueryKey({ path: { id: tag?.id || "" } }),
            });
            navigate(listPath);
        },
        onError: commonMutationErrorHandler(t("messages.tag_update_error")),
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
                                    <FormLabel>{t("tags.form.name_label")}</FormLabel>
                                    <FormControl>
                                        <Input placeholder={t("tags.form.name_placeholder")} {...field} />
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
                                    <FormLabel>{t("tags.form.color_label")}</FormLabel>
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
                                    <FormDescription>{t("tags.form.color_description")}</FormDescription>
                                    <FormMessage />
                                </FormItem>
                            )}
                        />

                        <FormField
                            control={form.control}
                            name="description"
                            render={({ field }) => (
                                <FormItem>
                                    <FormLabel>{t("tags.form.description_label")}</FormLabel>
                                    <FormControl>
                                        <Textarea
                                            placeholder={t("tags.form.description_placeholder")}
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
                        onClick={() => navigate(listPath)}
                        disabled={isPending}
                    >
                        {t("common.cancel")}
                    </Button>
                    <Button type="submit" disabled={isPending}>
                        {isPending && <Loader2 className="animate-spin mr-2 h-4 w-4" />}
                        {mode === "create" ? t("tags.form.create_button") : t("tags.form.update_button")}
                    </Button>
                </div>
            </form>
        </Form>
    );
};

export default TagForm;
