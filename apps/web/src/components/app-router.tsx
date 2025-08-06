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

  // If user is authenticated, always show the main app regardless of custom domain
  // If user is not authenticated and we have a custom domain, show status page
  // Otherwise, show auth routes or main app based on authentication state
  const shouldShowCustomDomainRoute = !isCustomDomainLoading && isFetched && customDomain && customDomain.data?.slug && !accessToken;
  const shouldRenderAuthRoutes = !isCustomDomainLoading && isFetched && (!customDomain || accessToken);

  return (
    <Routes>
      {/* Public routes */}
      {publicRoutes}

      {/* Custom domain route - render PublicStatusPage at root without login */}
      {shouldShowCustomDomainRoute && customDomain.data?.slug &&
        createCustomDomainRoute(customDomain.data.slug)
      }

      {/* Auth-dependent routes - prioritize authenticated users over custom domain */}
      {shouldRenderAuthRoutes && (
        !accessToken ? authRoutes : protectedRoutes
      )}
    </Routes>
  );
}; 