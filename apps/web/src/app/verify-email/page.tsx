import { useParams } from "react-router-dom";
import { AuthPageShell } from "@/components/auth-page-shell";
import { VerifyEmailLanding } from "./verify-email-landing";

export default function VerifyEmailPage() {
  const { token = "" } = useParams();
  return (
    <AuthPageShell>
      <VerifyEmailLanding token={token} />
    </AuthPageShell>
  );
}
