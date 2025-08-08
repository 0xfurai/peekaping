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

  // Routing rules:
  // - If the user has an accessToken: render the main app (ignore custom-domain logic).
  // - If the user has no accessToken and we resolved a custom domain to a status page: render the public status page at root.
  // - Otherwise: render auth or protected routes as appropriate.
  //
  // Why we check accessToken:
  // Visitors coming via a custom domain won’t have an access token. If someone points the custom domain at the app host,
  // this guard prevents the dashboard from taking over and ensures unauthenticated users see the public status page instead.
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