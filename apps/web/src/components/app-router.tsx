import { Routes } from "react-router-dom";
import { useAuthStore } from "@/store/auth";
import { useCheckCustomDomain } from "@/hooks/useCheckCustomDomain";
import { publicRoutes, createCustomDomainRoute } from "@/routes/public-routes";
import { authRoutes } from "@/routes/auth-routes";
import { protectedRoutes } from "@/routes/protected-routes";

export const AppRouter = () => {
  const accessToken = useAuthStore((state) => state.accessToken);
  const {
    customDomain,
    isCustomDomainLoading,
    isFetched,
  } = useCheckCustomDomain(window.location.hostname);

  const shouldRenderAuthRoutes = !isCustomDomainLoading && isFetched && !customDomain;

  return (
    <Routes>
      {/* Public routes */}
      {publicRoutes}

      {/* Custom domain route - render PublicStatusPage at root without login */}
      {customDomain && customDomain.data?.slug && 
        createCustomDomainRoute(customDomain.data.slug)
      }

      {/* Only render auth-dependent routes if not loading custom domain check */}
      {shouldRenderAuthRoutes && (
        !accessToken ? authRoutes : protectedRoutes
      )}
    </Routes>
  );
}; 