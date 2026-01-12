import { useEffect } from "react";
import { Outlet, useParams, useNavigate } from "react-router-dom";
import { useOrganizationStore } from "@/store/organization";
import { getOrganizationsSlugBySlug, getUserOrganizations } from "@/api/sdk.gen";
import { toast } from "sonner";

import { useQueryClient } from "@tanstack/react-query";

export const OrganizationLayout = ({ isGlobal = false }: { isGlobal?: boolean }) => {
    const { slug } = useParams<{ slug: string }>();
    const navigate = useNavigate();
    const queryClient = useQueryClient();
    const { currentOrganization, setCurrentOrganization, setIsLoading, isLoading, setOrganizations } = useOrganizationStore();

    useEffect(() => {
        const loadUserOrgs = async () => {
            // Fetch user organizations for switcher
            try {
                const { data: orgsData } = await getUserOrganizations();
                if (orgsData?.data) {
                    setOrganizations(orgsData.data);
                }
            } catch (e) {
                console.error("Failed to load user orgs", e);
            }
        };

        if (isGlobal) {
            loadUserOrgs();
            setIsLoading(false);
            return;
        }

        if (!slug) return;

        if (currentOrganization?.slug === slug) {
            setIsLoading(false);
            return;
        }

        const fetchOrg = async () => {
            setIsLoading(true);
            try {
                // Remove all queries when switching orgs to prevent stale data
                queryClient.removeQueries();

                const { data } = await getOrganizationsSlugBySlug({
                    path: { slug }
                });
                if (data?.data) {
                    setCurrentOrganization(data.data);
                } else {
                    throw new Error("Organization not found");
                }

                await loadUserOrgs();

            } catch (error) {
                console.error("Failed to fetch organization", error);
                toast.error("Organization not found");
                navigate("/");
            } finally {
                setIsLoading(false);
            }
        };

        fetchOrg();
    }, [slug, setCurrentOrganization, setIsLoading, navigate, currentOrganization?.slug, isGlobal]);

    if (isLoading) {
        // TODO: Replace with a proper Loading Spinner component
        return <div className="flex items-center justify-center h-screen text-muted-foreground">Loading...</div>;
    }

    // specific check: if NOT global, we need a current org
    if (!isGlobal && !currentOrganization) return null;

    return <Outlet />;
};
