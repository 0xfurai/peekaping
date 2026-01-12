import { useEffect } from "react";
import { useNavigate } from "react-router-dom";
import { getUserOrganizations } from "@/api/sdk.gen";
import { useOrganizationStore } from "@/store/organization";
import { toast } from "sonner";

export const RootRedirect = () => {
    const navigate = useNavigate();
    const { setOrganizations } = useOrganizationStore();

    useEffect(() => {
        const fetchOrgs = async () => {
            try {
                const { data } = await getUserOrganizations();
                if (data?.data && data.data.length > 0) {
                    setOrganizations(data.data);
                    // Redirect to the first organization's dashboard
                    const firstOrg = data.data[0];
                    if (firstOrg.organization?.slug) {
                        navigate(`/${firstOrg.organization.slug}/monitors`);
                        return;
                    }
                }

                // If no orgs, maybe redirect to onboarding or create org page?
                // For now, let's redirect to 'welcome' or similar, but since we don't have it, 
                // we might need a create org page first.
                // Assuming /create-organization route exists or we create it.
                // navigate("/create-organization");

                // If we stay here it's blank. Let's assume we have to create one.
                navigate("/create-organization"); // We need to implement this route

            } catch (error) {
                console.error("Failed to fetch user organizations", error);
                toast.error("Failed to load organizations");
            }
        };

        fetchOrgs();
    }, [navigate, setOrganizations]);

    return <div className="flex items-center justify-center h-screen">Loading...</div>;
};
