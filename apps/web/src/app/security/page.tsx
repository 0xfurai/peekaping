import Layout from "@/layout";
import UpdatePassword from "./components/update-password";
import Enable2FA from "./components/enable-2fa";
import { useAuthStore } from "@/store/auth";
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from "@/components/ui/card";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import { AlertCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { postAuth2FaDisableMutation } from "@/api/@tanstack/react-query.gen";
import { toast } from "sonner";
import { commonMutationErrorHandler } from "@/lib/utils";
import { PasswordInput } from "@/components/ui/password-input";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const SecurityPage = () => {
  const { t } = useLocalizedTranslation();
  const user = useAuthStore((s) => s.user);
  const setUser = useAuthStore((s) => s.setUser);
  const [showDisable, setShowDisable] = useState(false);
  const [password, setPassword] = useState("");
  const [loading, setLoading] = useState(false);

  const disable2FAMutation = useMutation({
    ...postAuth2FaDisableMutation(),
    onSuccess: () => {
      toast.success(t("security.enable_2fa.messages.2fa_disabled_successfully"));
      setUser({
        ...user,
        email: user?.email || "",
        id: user?.id || "",
        twofa_status: false,
      });
      setShowDisable(false);
      setPassword("");
    },
    onError: commonMutationErrorHandler(t("security.enable_2fa.messages.failed_to_disable_2fa")),
  });

  const handleDisable2FA = (e: React.FormEvent) => {
    e.preventDefault();
    if (!user?.email) return toast.error(t("security.enable_2fa.messages.user_email_not_found"));
    setLoading(true);
    disable2FAMutation.mutate({
      body: { email: user.email, password },
    });
    setLoading(false);
  };

  return (
    <Layout pageName={t("security.page_name")}>
      <UpdatePassword />

      {user?.twofa_status ? (
        <Card className="mb-6 mt-6">
          <CardHeader>
            <CardTitle>{t("security.enable_2fa.messages.two_factor_authentication_enabled")}</CardTitle>
            <CardDescription>{t("security.enable_2fa.messages.account_is_protected_with_2fa")}</CardDescription>
          </CardHeader>
          <CardContent>
            <Alert variant="default" className="mb-4">
              <AlertCircle className="h-4 w-4" />
              <AlertTitle>{t("security.enable_2fa.messages.2fa_active")}</AlertTitle>
              <AlertDescription>
                {t("security.enable_2fa.messages.have_enabled_2fa")}
              </AlertDescription>
            </Alert>

            {showDisable ? (
              <form onSubmit={handleDisable2FA} className="flex flex-col gap-2 max-w-xs">
                <PasswordInput
                  placeholder={t("security.enable_2fa.messages.enter_password_to_disable_2fa")}
                  value={password}
                  onChange={e => setPassword(e.target.value)}
                  required
                />


                <div className="flex gap-2">
                  <Button type="submit" disabled={loading} variant="destructive">
                    {loading ? t("common.disabling") : t("security.enable_2fa.messages.disable_2fa")}
                  </Button>
                  <Button type="button" variant="outline" onClick={() => setShowDisable(false)}>
                    {t("common.cancel")}
                  </Button>
                </div>
              </form>
            ) : (
              <Button variant="destructive" onClick={() => setShowDisable(true)}>
                {t("security.enable_2fa.messages.disable_2fa")}
              </Button>
            )}
          </CardContent>
        </Card>
      ) : (
        <Enable2FA />
      )}
    </Layout>
  );
};

export default SecurityPage;
