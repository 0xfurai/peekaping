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

const SecurityPage = () => {
  const user = useAuthStore((s) => s.user);
  const setUser = useAuthStore((s) => s.setUser);
  const [showDisable, setShowDisable] = useState(false);
  const [password, setPassword] = useState("");
  const [loading, setLoading] = useState(false);

  const disable2FAMutation = useMutation({
    ...postAuth2FaDisableMutation(),
    onSuccess: () => {
      toast.success("2FA disabled successfully");
      setUser({
        ...user,
        email: user?.email || "",
        id: user?.id || "",
        twofa_status: false,
      });
      setShowDisable(false);
      setPassword("");
    },
    onError: commonMutationErrorHandler("Failed to disable 2FA"),
  });

  const handleDisable2FA = (e: React.FormEvent) => {
    e.preventDefault();
    if (!user?.email) return toast.error("User email not found");
    setLoading(true);
    disable2FAMutation.mutate({
      body: { email: user.email, password },
    });
    setLoading(false);
  };

  return (
    <Layout pageName="Security">
      <UpdatePassword />

      {user?.twofa_status ? (
        <Card className="mb-6 mt-6">
          <CardHeader>
            <CardTitle>Two-Factor Authentication Enabled</CardTitle>
            <CardDescription>Your account is protected with 2FA.</CardDescription>
          </CardHeader>
          <CardContent>
            <Alert variant="default" className="mb-4">
              <AlertCircle className="h-4 w-4" />
              <AlertTitle>2FA Active</AlertTitle>
              <AlertDescription>
                You have enabled two-factor authentication (2FA) for your account. This adds an extra layer of security.
              </AlertDescription>
            </Alert>

            {showDisable ? (
              <form onSubmit={handleDisable2FA} className="flex flex-col gap-2 max-w-xs">
                <PasswordInput
                  placeholder="Enter your password to disable 2FA"
                  value={password}
                  onChange={e => setPassword(e.target.value)}
                  required
                />


                <div className="flex gap-2">
                  <Button type="submit" disabled={loading} variant="destructive">
                    {loading ? "Disabling..." : "Disable 2FA"}
                  </Button>
                  <Button type="button" variant="outline" onClick={() => setShowDisable(false)}>
                    Cancel
                  </Button>
                </div>
              </form>
            ) : (
              <Button variant="destructive" onClick={() => setShowDisable(true)}>
                Disable 2FA
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
