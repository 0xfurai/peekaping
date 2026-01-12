import { Navigate, Route } from "react-router-dom";
import PublicStatusPage from "@/app/status/[slug]/page";
import InvitationPage from "@/app/invite/[token]/page";

export const publicRoutes = [
    <Route path="/status/:slug" element={<PublicStatusPage />} />,
    <Route path="/invite/:token" element={<InvitationPage />} />
];

export const createCustomDomainRoute = (slug: string) => (
    <>
        <Route path="/" element={<PublicStatusPage incomingSlug={slug} />} />
        <Route path="*" element={<Navigate to="/" replace />} />
    </>
); 