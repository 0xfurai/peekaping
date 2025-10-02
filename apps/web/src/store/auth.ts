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
        // Set first workspace as default if no workspace is selected
        set((state) => ({
          workspaces,
          selectedWorkspaceId: state.selectedWorkspaceId || (workspaces.length > 0 ? workspaces[0].id : null),
        }));
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
