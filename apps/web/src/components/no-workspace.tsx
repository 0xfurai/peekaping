import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { NewWorkspaceDialog } from "@/components/new-workspace-dialog";
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

// What a signed-in user sees when they are a member of no workspace: they can make one, or sign out.
export function NoWorkspace() {
  const { t } = useLocalizedTranslation();
  const clearTokens = useAuthStore((state) => state.clearTokens);
  const navigate = useNavigate();
  const [creating, setCreating] = useState(false);

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
        <CardContent className="grid gap-2">
          <Button className="w-full" onClick={() => setCreating(true)}>
            {t("workspaces.create_first")}
          </Button>
          <Button variant="outline" className="w-full" onClick={handleLogout}>
            {t("common.logout")}
          </Button>
        </CardContent>
      </Card>
      <NewWorkspaceDialog open={creating} onOpenChange={setCreating} />
    </div>
  );
}
