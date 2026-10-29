import { useParams } from "react-router-dom";
import { AuthPageShell } from "@/components/auth-page-shell";
import { ResetPasswordForm } from "./reset-password-form";

export default function ResetPasswordPage() {
  const { token = "" } = useParams();
  return (
    <AuthPageShell>
      <ResetPasswordForm token={token} />
    </AuthPageShell>
  );
}
