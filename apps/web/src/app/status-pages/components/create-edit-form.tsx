import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Switch } from "@/components/ui/switch";
import {
    Form,
    FormControl,
    FormField,
    FormItem,
    FormLabel,
    FormMessage,
} from "@/components/ui/form";
import SearchableMonitorSelector from "@/components/searchable-monitor-selector";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useForm } from "react-hook-form";
import { useState } from "react";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { AlertCircleIcon } from "lucide-react";
import DomainsManager from "./domains-manager";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
    getStatusPagesInfiniteQueryKey,
    getStatusPagesByIdQueryKey,
    postStatusPagesMutation,
    patchStatusPagesByIdMutation,
} from "@/api/@tanstack/react-query.gen";
import { toast } from "sonner";
import { useNavigate } from "react-router-dom";
import { commonMutationErrorHandler } from "@/lib/utils";
import type { AxiosError } from "axios";
import { useOrganizationStore } from "@/store/organization";

const statusPageSchema = z.object({
    title: z.string().min(1, "Title is required"),
    slug: z
        .string()
        .min(1, "Slug is required")
        .regex(
            /^[a-z0-9-]+$/,
            "Slug must contain only lowercase letters, numbers, and hyphens"
        ),
    description: z.string().optional(),
    icon: z.string().optional(),
    footer_text: z.string().optional(),
    auto_refresh_interval: z.number().min(0).optional(),
    published: z.boolean(),
    monitors: z
        .array(
            z.object({
                label: z.string(),
                value: z.string(),
            })
        )
        .optional(),
    domains: z.array(z.string()).optional(),
});

export type StatusPageForm = z.infer<typeof statusPageSchema>;

type DomainAlreadyUsedError = {
    code: "DOMAIN_EXISTS";
    domain: string;
};

type SlugAlreadyUsedError = {
    code: "SLUG_EXISTS";
    slug: string;
};

const formDefaultValues: StatusPageForm = {
    title: "",
    slug: "",
    description: "",
    icon: "",
    footer_text: "",
    auto_refresh_interval: 300,
    published: true,
    monitors: [],
    domains: [],
};

const CreateEditForm = ({
    initialValues,
    mode = "create",
    id,
}: {
    initialValues?: StatusPageForm;
    mode?: "create" | "edit";
    id?: string;
}) => {
    const { t } = useLocalizedTranslation();
    const navigate = useNavigate();
    const queryClient = useQueryClient();
    const [domainToHighlight, setDomainToHighlight] = useState<string | undefined>(undefined);
    const { currentOrganization } = useOrganizationStore();
    const slug = currentOrganization?.slug;
    const listPath = slug ? `/${slug}/status-pages` : "/status-pages";

    const form = useForm<StatusPageForm>({
        defaultValues: initialValues || formDefaultValues,
        resolver: zodResolver(statusPageSchema),
    });

    const handleMutationError = (error: AxiosError<{ error: DomainAlreadyUsedError | SlugAlreadyUsedError }>) => {
        // Fallback toast
        const fallbackMessage = mode === "create" ? "Failed to create status page" : "Failed to update status page";
        commonMutationErrorHandler(fallbackMessage)(error);

        // Handle structured error response
        const errorData = error.response?.data?.error;

        if (errorData?.code === "DOMAIN_EXISTS") {
            const domainErrorMessage = t("status_pages.domain_already_used", { domain: errorData.domain });

            form.setError("domains", { message: domainErrorMessage });

            if (errorData.domain) {
                setDomainToHighlight(errorData.domain);
            }
        }

        if (errorData?.code === "SLUG_EXISTS") {
            form.setError("slug", { message: t("status_pages.slug_already_used", { slug: errorData.slug }) });
            window.scrollTo({ top: 0, behavior: "smooth" });
        }
    };

    const createStatusPageMutation = useMutation({
        mutationFn: postStatusPagesMutation().mutationFn,
        onSuccess: () => {
            toast.success(t("status_pages.messages.created_successfully"));
            queryClient.invalidateQueries({
                queryKey: getStatusPagesInfiniteQueryKey(),
            });
            navigate(listPath);
        },
        onError: handleMutationError,
    });

    const editStatusPageMutation = useMutation({
        mutationFn: patchStatusPagesByIdMutation({
            path: { id: id! },
        }).mutationFn,
        onSuccess: () => {
            toast.success(t("status_pages.messages.updated_successfully"));
            queryClient.invalidateQueries({
                queryKey: getStatusPagesInfiniteQueryKey(),
            });
            queryClient.removeQueries({
                queryKey: getStatusPagesByIdQueryKey({ path: { id: id! } }),
            });
            navigate(listPath);
        },
        onError: handleMutationError,
    });

    const handleSubmit = (data: StatusPageForm) => {
        // Clear previous errors
        form.clearErrors("domains");
        setDomainToHighlight(undefined);

        const { monitors, ...rest } = data;
        const payload = {
            ...rest,
            monitor_ids: monitors?.map((monitor) => monitor.value),
        };

        if (mode === "create") {
            createStatusPageMutation.mutate({ body: payload });
        } else {
            editStatusPageMutation.mutate({
                body: payload,
                path: { id: id! },
            });
        }
    };

    const isPending = createStatusPageMutation.isPending || editStatusPageMutation.isPending;

    return (
        <Form {...form}>
            <form
                onSubmit={form.handleSubmit(handleSubmit)}
                className="space-y-6 max-w-[600px]"
            >
                <Card>
                    <CardHeader>
                        <CardTitle>{t("status_pages.basic_information")}</CardTitle>
                    </CardHeader>
                    <CardContent className="space-y-4">
                        <div className="grid grid-cols-1 md:grid-cols-2 gap-4 items-start">
                            <FormField
                                control={form.control}
                                name="title"
                                render={({ field }) => (
                                    <FormItem>
                                        <FormLabel>{t("forms.labels.title")}</FormLabel>
                                        <FormControl>
                                            <Input placeholder={t("forms.placeholders.page_title")} {...field} />
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
                                        <FormLabel>{t("forms.labels.slug")}</FormLabel>
                                        <FormControl>
                                            <Input placeholder={t("forms.placeholders.slug")} {...field} />
                                        </FormControl>
                                        <FormMessage />
                                    </FormItem>
                                )}
                            />
                        </div>

                        <FormField
                            control={form.control}
                            name="description"
                            render={({ field }) => (
                                <FormItem>
                                    <FormLabel>{t("status_pages.description")}</FormLabel>
                                    <FormControl>
                                        <Textarea
                                            placeholder={t("forms.placeholders.page_description")}
                                            {...field}
                                        />
                                    </FormControl>
                                    <FormMessage />
                                </FormItem>
                            )}
                        />

                        <FormField
                            control={form.control}
                            name="icon"
                            render={({ field }) => (
                                <FormItem>
                                    <FormLabel>{t("forms.labels.icon_url")}</FormLabel>
                                    <FormControl>
                                        <Input
                                            placeholder={t("forms.placeholders.icon_url")}
                                            {...field}
                                        />
                                    </FormControl>
                                    <FormMessage />
                                </FormItem>
                            )}
                        />

                        <div className="space-y-4">
                            <h2 className="text-lg font-semibold">{t("status_pages.affected_monitors")}</h2>
                            <div className="space-y-2">
                                <p className="text-sm text-muted-foreground">
                                    {t("status_pages.affected_monitors_info")}
                                </p>

                                <SearchableMonitorSelector
                                    value={form.watch("monitors") || []}
                                    onSelect={(value) => {
                                        form.setValue("monitors", value);
                                    }}
                                />
                            </div>
                        </div>
                    </CardContent>
                </Card>

                <Card>
                    <CardHeader>
                        <CardTitle>{t("status_pages.customization")}</CardTitle>
                    </CardHeader>
                    <CardContent className="space-y-4">
                        <FormField
                            control={form.control}
                            name="footer_text"
                            render={({ field }) => (
                                <FormItem>
                                    <FormLabel>{t("forms.labels.footer_text")}</FormLabel>
                                    <FormControl>
                                        <Input placeholder={t("forms.placeholders.footer_text")} {...field} />
                                    </FormControl>
                                    <FormMessage />
                                </FormItem>
                            )}
                        />

                        <FormField
                            control={form.control}
                            name="auto_refresh_interval"
                            render={({ field }) => (
                                <FormItem>
                                    <FormLabel>{t("status_pages.auto_refresh_interval")}</FormLabel>
                                    <FormControl>
                                        <Input
                                            type="number"
                                            min="0"
                                            placeholder={t("forms.placeholders.auto_refresh_help")}
                                            {...field}
                                            onChange={(e) => field.onChange(e.target.valueAsNumber)}
                                        />
                                    </FormControl>
                                    <FormMessage />
                                </FormItem>
                            )}
                        />
                    </CardContent>
                </Card>

                <Card>
                    <CardHeader>
                        <CardTitle>{t("common.settings")}</CardTitle>
                    </CardHeader>
                    <CardContent className="space-y-4">
                        <FormField
                            control={form.control}
                            name="published"
                            render={({ field }) => (
                                <FormItem>
                                    <div className="flex items-center justify-between">
                                        <div className="space-y-0.5">
                                            <FormLabel>{t("status_pages.published")}</FormLabel>
                                            <p className="text-sm text-muted-foreground">
                                                {t("status_pages.published_info")}
                                            </p>
                                        </div>
                                        <FormControl>
                                            <Switch
                                                checked={field.value}
                                                onCheckedChange={field.onChange}
                                            />
                                        </FormControl>
                                    </div>
                                    <FormMessage />
                                </FormItem>
                            )}
                        />
                        <FormField
                            control={form.control}
                            name="domains"
                            render={({ field }) => (
                                <FormItem>
                                    <FormLabel>{t("status_pages.domains")}</FormLabel>
                                    <FormControl>
                                        <DomainsManager
                                            value={field.value || []}
                                            onChange={field.onChange}
                                            error={{
                                                message: form.formState.errors.domains?.message,
                                                domain: domainToHighlight,
                                            }}
                                        />
                                    </FormControl>
                                    <Alert className="mt-1">
                                        <AlertCircleIcon className="w-4 h-4" />
                                        <AlertDescription>
                                            {t("status_pages.domains_info_warning")}
                                        </AlertDescription>
                                    </Alert>
                                </FormItem>
                            )}
                        />
                    </CardContent>
                </Card>

                <div className="flex justify-end space-x-2">
                    <Button
                        type="button"
                        variant="outline"
                        onClick={() => navigate(listPath)}
                    >
                        {t("actions.cancel")}
                    </Button>
                    <Button type="submit" disabled={isPending}>
                        {isPending
                            ? t("actions.saving")
                            : mode === "create"
                                ? t("status_pages.create_status_page")
                                : t("status_pages.update_status_page")}
                    </Button>
                </div>
            </form>
        </Form>
    );
};

export default CreateEditForm;
