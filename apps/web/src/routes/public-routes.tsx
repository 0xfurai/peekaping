import { Navigate, Route } from "react-router-dom";
import PublicStatusPage from "@/app/status/[slug]/page";

export const publicRoutes = [
  <Route path="/status/:slug" element={<PublicStatusPage />} />
];

export const createCustomDomainRoute = (slug: string) => (
  <>
    <Route path="/" element={<PublicStatusPage incomingSlug={slug} />} />
    <Route path="*" element={<Navigate to="/" replace />} />
  </>
); 