import { Navigate, Route } from "react-router-dom";
import PublicStatusPage from "@/app/status/[slug]/page";
import InvitePage from "@/app/invite/page";

export const publicRoutes = [
  <Route path="/status/:slug" element={<PublicStatusPage />} />,
  // An invite link opens for a visitor and a signed-in user alike
  <Route path="/invite/:token" element={<InvitePage />} />
];

export const createCustomDomainRoute = (slug: string) => (
  <>
    <Route path="/" element={<PublicStatusPage incomingSlug={slug} />} />
    <Route path="*" element={<Navigate to="/" replace />} />
  </>
); 