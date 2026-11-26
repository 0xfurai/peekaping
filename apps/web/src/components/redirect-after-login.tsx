import { Navigate, useSearchParams } from "react-router-dom";
import { safeNextPath } from "@/lib/next-path";
import { RedirectToWorkspace } from "@/components/redirect-to-workspace";

// A signed-in user on /login: back to the invite they came from, if the address names one,
// else the app's start.
export function RedirectAfterLogin() {
  const [params] = useSearchParams();
  const next = safeNextPath(params.get("next"));
  return next ? <Navigate to={next} replace /> : <RedirectToWorkspace path="/" />;
}
