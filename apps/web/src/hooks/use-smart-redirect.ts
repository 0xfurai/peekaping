import { useNavigate } from "react-router-dom";
import { getUserInvitations, getUserOrganizations } from "@/api/sdk.gen";
import { useOrganizationStore } from "@/store/organization";
import { toast } from "sonner";
import { useState } from "react";

export function useSmartRedirect() {
    const navigate = useNavigate();
    const { setOrganizations } = useOrganizationStore();
    const [isLoading, setIsLoading] = useState(false);

    const handleRedirect = async () => {
        setIsLoading(true);
        try {
            // 1. Check for pending invitations first
            try {
                const { data: invitationsData } = await getUserInvitations();
                if (invitationsData?.data && invitationsData.data.length > 0) {
                    navigate("/onboarding");
                    return;
                }
            } catch (invError) {
                console.error("Failed to fetch user invitations", invError);
            }

            // 2. Check for existing organizations
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

            // 3. If no orgs and no invitations, redirect to create organization
            navigate("/create-organization");

        } catch (error) {
            console.error("Failed to handle smart redirect", error);
            // Fallback
            navigate("/create-organization");
        } finally {
            setIsLoading(false);
        }
    };

    return { handleRedirect, isLoading };
}
