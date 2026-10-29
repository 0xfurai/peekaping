import { Routes } from "react-router-dom";
import { useAuthStore } from "@/store/auth";
import { useCheckCustomDomain } from "@/hooks/useCheckCustomDomain";
import {
  publicRoutes,
  createCustomDomainRoute,
  createCustomDomainUnavailableRoute,
} from "@/routes/public-routes";
import { authRoutes } from "@/routes/auth-routes";
import { protectedRoutes } from "@/routes/protected-routes";

export const AppRouter = () => {
  const accessToken = useAuthStore((state) => state.accessToken);
  const { customDomain, isFetched, unavailable } = useCheckCustomDomain(
    window.location.hostname
  );

  // Routing rules:
  // - If the user has an accessToken: render the main app (ignore custom-domain logic).
  // - If the user has no accessToken and we resolved a custom domain to a status page: render the public status page at root.
  // - Otherwise: render auth or protected routes as appropriate.
  //
  // Why we check accessToken:
  // Visitors coming via a custom domain won’t have an access token. If someone points the custom domain at the app host,
  // this guard prevents the dashboard from taking over and ensures unauthenticated users see the public status page instead.
  //
  // Why we wait for the first lookup only:
  // The lookup settles once per page load: with a status page on a custom-domain host, with a 404 on any other host.
  // `isFetched` turns true when it settles and stays true, so a later refetch — a reconnect after a laptop wakes, say —
  // leaves the routes below mounted. Gating on the hook's loading flag instead unmounted them for the length of every
  // background refetch, which wiped whatever was being typed into an open form (see issues #161 and #228).
  const shouldShowCustomDomainRoute = isFetched && customDomain && customDomain.data?.slug && !accessToken;
  // A custom domain whose page's workspace is suspended answers 503 to the lookup: its visitors
  // see that the page is unavailable, not the sign-in page
  const shouldShowUnavailableRoute = isFetched && unavailable && !accessToken;
  const shouldRenderAuthRoutes =
    isFetched && !shouldShowUnavailableRoute && (!customDomain || accessToken);

  return (
    <Routes>
      {/* Public routes */}
      {publicRoutes}

      {/* Custom domain route - render PublicStatusPage at root without login */}
      {shouldShowCustomDomainRoute && customDomain.data?.slug &&
        createCustomDomainRoute(customDomain.data.slug)
      }

      {shouldShowUnavailableRoute && createCustomDomainUnavailableRoute()}

      {/* Auth-dependent routes - prioritize authenticated users over custom domain */}
      {shouldRenderAuthRoutes && (
        !accessToken ? authRoutes : protectedRoutes
      )}
    </Routes>
  );
}; 