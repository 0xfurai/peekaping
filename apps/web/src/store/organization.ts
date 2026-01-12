import { create } from "zustand";
import type {
    OrganizationOrganization as Organization,
    OrganizationOrganizationUser as OrganizationUser
} from "@/api/types.gen";

interface OrganizationState {
    currentOrganization: Organization | null;
    organizations: OrganizationUser[];
    isLoading: boolean;

    setOrganizations: (orgs: OrganizationUser[]) => void;
    setCurrentOrganization: (org: Organization | null) => void;
    setIsLoading: (loading: boolean) => void;
}

export const useOrganizationStore = create<OrganizationState>((set) => ({
    currentOrganization: null,
    organizations: [],
    isLoading: true,

    setOrganizations: (orgs) => set({ organizations: orgs }),
    setCurrentOrganization: (org) => set({ currentOrganization: org }),
    setIsLoading: (loading) => set({ isLoading: loading }),
}));
