import { useEffect } from "react";
import { useNavigate } from "react-router-dom";
import { getUserOrganizations, getUserInvitations } from "@/api/sdk.gen";
import { useOrganizationStore } from "@/store/organization";
import { toast } from "sonner";

export const RootRedirect = () => {
    const navigate = useNavigate();
    const { setOrganizations } = useOrganizationStore();

    useEffect(() => {
        const fetchOrgs = async () => {
            try {
                // 1. Check for existing organizations
                const { data: orgsData } = await getUserOrganizations();
                if (orgsData?.data && orgsData.data.length > 0) {
                    setOrganizations(orgsData.data);
                    // Redirect to the first organization's dashboard
                    const firstOrg = orgsData.data[0];
                    if (firstOrg.organization?.slug) {
                        navigate(`/${firstOrg.organization.slug}/monitors`);
                        return;
                    }
                }

                // 2. If no organizations, check for pending invitations
                try {
                    const { data: invitationsData } = await getUserInvitations();
                    if (invitationsData?.data && invitationsData.data.length > 0) {
                        // User has pending invitations, let them accept them first
                        navigate("/account/invitations");
                        return;
                    }
                } catch (invError) {
                    console.error("Failed to fetch user invitations", invError);
                    // Continue to onboarding if invitations check fails (fail open logic? or stay?)
                }

                // 3. If no orgs and no invitations, redirect to create organization
                navigate("/create-organization");

            } catch (error) {
                console.error("Failed to fetch user organizations", error);
                toast.error("Failed to load organizations");
            }
        };

        fetchOrgs();
    }, [navigate, setOrganizations]);

    return <div className="flex items-center justify-center h-screen">Loading...</div>;
};
