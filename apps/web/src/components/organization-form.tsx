import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
    Form,
    FormControl,
    FormField,
    FormItem,
    FormLabel,
    FormMessage,
} from "@/components/ui/form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { postOrganizations } from "@/api/sdk.gen";
import { client } from "@/api/client.gen";
import { toast } from "sonner";
import { useNavigate } from "react-router-dom";
import { useOrganizationStore } from "@/store/organization";

const organizationSchema = (t: (key: string, options?: any) => string) => z.object({
    name: z.string().min(3, t("organization.validation.name_min_length")),
    slug: z
        .string()
        .min(3, t("organization.validation.slug_min_length"))
        .regex(/^[a-z0-9-]+$/, t("organization.validation.slug_format"))
        .optional()
        .or(z.literal("")),
});

export type OrganizationFormValues = z.infer<ReturnType<typeof organizationSchema>>;

interface OrganizationFormProps {
    mode?: "create" | "edit";
    initialValues?: OrganizationFormValues;
    organizationId?: string;
    onSuccess?: (data: any) => void;
}

export function OrganizationForm({
    mode = "create",
    initialValues,
    organizationId,
    onSuccess,
}: OrganizationFormProps) {
    const { t } = useLocalizedTranslation();
    const navigate = useNavigate();
    const queryClient = useQueryClient();
    const { setOrganizations, organizations, setCurrentOrganization } = useOrganizationStore();

    const formSchema = organizationSchema(t);

    const form = useForm<OrganizationFormValues>({
        resolver: zodResolver(formSchema),
        defaultValues: initialValues || {
            name: "",
            slug: "",
        },
    });

    const createMutation = useMutation({
        mutationFn: async (values: OrganizationFormValues) => {
            const { data } = await postOrganizations({
                body: {
                    name: values.name,
                    slug: values.slug || undefined,
                },
            });
            return data;
        },
        onSuccess: (data) => {
            if (data?.data) {
                const newOrg = data.data;
                // Update store
                setOrganizations([...organizations, newOrg]);
                setCurrentOrganization(newOrg);

                toast.success(t("organization.creation_success"));

                if (onSuccess) {
                    onSuccess(newOrg);
                } else if (newOrg.slug) {
                    // Default redirect
                    navigate(`/${newOrg.slug}/monitors`);
                }
            }
        },
        onError: handleMutationError,
    });

    const updateMutation = useMutation({
        mutationFn: async (values: OrganizationFormValues) => {
            if (!organizationId) throw new Error("Organization ID required for update");
            const response = await client.instance.patch(`/organizations/${organizationId}`, {
                name: values.name,
                slug: values.slug || undefined,
            });
            return response.data;
        },
        onSuccess: (data) => {
            toast.success(t("organization.update_success"));
            queryClient.invalidateQueries({ queryKey: ["organizations", organizationId] });
            if (onSuccess) onSuccess(data);
        },
        onError: handleMutationError,
    });

    function handleMutationError(error: any) {
        // Handle SLUG_EXISTS
        const errorData = error.response?.data?.error;
        if (errorData?.code === "SLUG_EXISTS") {
            form.setError("slug", {
                message: t("organization.slug_already_used", { slug: errorData.slug })
            });
            // Scroll to top to see error
            window.scrollTo({ top: 0, behavior: "smooth" });
        } else {
            const defaultMsg = mode === "create" ? "Failed to create organization" : "Failed to update organization";
            const key = mode === "create" ? "organization.creation_error" : "organization.update_error";
            toast.error(t(key) || defaultMsg);
        }
    }

    const onSubmit = (values: OrganizationFormValues) => {
        if (mode === "create") {
            createMutation.mutate(values);
        } else {
            updateMutation.mutate(values);
        }
    };

    return (
        <Form {...form}>
            <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-4">
                <FormField
                    control={form.control}
                    name="name"
                    render={({ field }) => (
                        <FormItem>
                            <FormLabel>{t("organization.name_label")}</FormLabel>
                            <FormControl>
                                <Input placeholder={t("organization.placeholders.name")} {...field} />
                            </FormControl>
                            <FormMessage />
                        </FormItem>
                    )}
                />
                <FormField
                    control={form.control}
                    name="slug"
                    render={({ field }) => (
                        <FormItem>
                            <FormLabel>{t("organization.slug_label")}</FormLabel>
                            <FormControl>
                                <Input placeholder={t("organization.placeholders.slug")} {...field} />
                            </FormControl>
                            <FormMessage />
                        </FormItem>
                    )}
                />
                <Button type="submit" className="w-full" disabled={createMutation.isPending || updateMutation.isPending}>
                    {mode === "create" ? t("organization.create_button") : t("organization.update_button")}
                </Button>
            </form>
        </Form>
    );
}
