import { GalleryVerticalEnd } from "lucide-react";
import { Navigate } from "react-router-dom";
import { RegisterForm } from "./components/register-form";
import { useAuthConfig } from "@/hooks/useAuthConfig";
import { registerPageView } from "@/lib/signup";

const SHRegisterPage = () => {
  const { config, failed } = useAuthConfig();
  const view = registerPageView(config, failed);
  // Signup is closed: invite links still work through /invite/:token
  if (view === "login") return <Navigate to="/login" replace />;

  return (
    <div className="flex min-h-svh flex-col items-center justify-center gap-6 bg-muted p-6 md:p-10">
      <div className="flex w-full max-w-sm flex-col gap-6">
        <a href="#" className="flex items-center gap-2 self-center font-medium">
          <div className="flex h-6 w-6 items-center justify-center rounded-md bg-primary text-primary-foreground">
            <GalleryVerticalEnd className="size-4" />
          </div>
          Peekaping
        </a>

        {view === "form" && <RegisterForm turnstileSiteKey={config?.turnstileSiteKey} signupMode={config?.signupMode} />}
      </div>
    </div>
  );
};

export default SHRegisterPage;
