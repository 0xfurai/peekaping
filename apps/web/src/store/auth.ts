import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import type { AuthModel, WorkspaceModel } from "@/api/types.gen";

interface AuthState {
  accessToken: string | null;
  refreshToken: string | null;
  user: AuthModel | null;
  workspaces: WorkspaceModel[];
  selectedWorkspaceId: string | null;
  setTokens: (accessToken: string, refreshToken: string) => void;
  setUser: (user: AuthModel | null) => void;
  setWorkspaces: (workspaces: WorkspaceModel[]) => void;
  setSelectedWorkspaceId: (workspaceId: string | null) => void;
  clearTokens: () => void;
  clearUser: () => void;
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set) => ({
      accessToken: null,
      refreshToken: null,
      user: null,
      workspaces: [],
      selectedWorkspaceId: null,
      setTokens: (accessToken: string, refreshToken: string) =>
        set({ accessToken, refreshToken }),
      setUser: (user: AuthModel | null) => set({ user }),
      setWorkspaces: (workspaces: WorkspaceModel[]) => {
        set((state) => {
          // Check if the currently selected workspace is still in the list
          const isSelectedWorkspaceValid = state.selectedWorkspaceId &&
            workspaces.some(w => w.id === state.selectedWorkspaceId);

          // Use existing selection if valid, otherwise use first workspace or null
          const selectedWorkspaceId = isSelectedWorkspaceValid
            ? state.selectedWorkspaceId
            : (workspaces.length > 0 ? workspaces[0].id : null);

          return {
            workspaces,
            selectedWorkspaceId,
          };
        });
      },
      setSelectedWorkspaceId: (workspaceId: string | null) =>
        set({ selectedWorkspaceId: workspaceId }),
      clearTokens: () => set({
        accessToken: null,
        refreshToken: null,
        user: null,
        workspaces: [],
        selectedWorkspaceId: null
      }),
      clearUser: () => set({ user: null }),
    }),
    {
      name: 'auth-storage',
    }
  )
);
