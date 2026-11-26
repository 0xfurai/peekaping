import { Navigate, Route } from "react-router-dom";
import PublicStatusPage from "@/app/status/[slug]/page";
import { StatusPageUnavailable } from "@/app/status/status-page-unavailable";
import InvitePage from "@/app/invite/page";
import ResetPasswordPage from "@/app/reset-password/page";
import VerifyEmailPage from "@/app/verify-email/page";

export const publicRoutes = [
  <Route path="/status/:slug" element={<PublicStatusPage />} />,
  // An invite link opens for a visitor and a signed-in user alike
  <Route path="/invite/:token" element={<InvitePage />} />,
  // So do the links in the account emails: a reset link opened in a browser that is signed in
  // (the server ends every session when the password changes), and a verification link
  <Route path="/reset-password/:token" element={<ResetPasswordPage />} />,
  <Route path="/verify-email/:token" element={<VerifyEmailPage />} />
];

export const createCustomDomainRoute = (slug: string) => (
  <>
    <Route path="/" element={<PublicStatusPage incomingSlug={slug} />} />
    <Route path="*" element={<Navigate to="/" replace />} />
  </>
); 

// A custom domain whose status page belongs to a suspended workspace: its lookup answers 503, and
// every address shows that the page is unavailable
export const createCustomDomainUnavailableRoute = () => (
  <>
    <Route path="/" element={<StatusPageUnavailable />} />
    <Route path="*" element={<Navigate to="/" replace />} />
  </>
);
