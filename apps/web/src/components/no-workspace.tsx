import { useNavigate } from "react-router-dom";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { useAuthStore } from "@/store/auth";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

// What a signed-in user sees when they are a member of no workspace.
export function NoWorkspace() {
  const { t } = useLocalizedTranslation();
  const clearTokens = useAuthStore((state) => state.clearTokens);
  const navigate = useNavigate();

  const handleLogout = () => {
    clearTokens();
    navigate("/login");
  };

  return (
    <div className="flex min-h-svh flex-col items-center justify-center bg-muted p-6 md:p-10">
      <Card className="w-full max-w-sm">
        <CardHeader className="text-center">
          <CardTitle className="text-xl">{t("workspaces.none_title")}</CardTitle>
          <CardDescription>{t("workspaces.none_description")}</CardDescription>
        </CardHeader>
        <CardContent>
          <Button variant="outline" className="w-full" onClick={handleLogout}>
            {t("common.logout")}
          </Button>
        </CardContent>
      </Card>
    </div>
  );
}
