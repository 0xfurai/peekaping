import { useId, useReducer } from "react";
import { useMutation } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { deleteAuthMeMutation } from "@/api/@tanstack/react-query.gen";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { PasswordInput } from "@/components/ui/password-input";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { dismissWhileIdle } from "@/lib/admin-users";
import { accountDeletionError, deleteAccountReducer } from "@/lib/account-deletion";
import { useAuthStore } from "@/store/auth";

// The signed-in user deletes their own account after entering their password. The server refuses
// while they are the only owner of a workspace that isn't scheduled for deletion, and for the last
// instance admin; the refusal is shown on the card. After a deletion the browser's session ends
// the way a logout ends it.
export default function DeleteAccount() {
  const { t } = useLocalizedTranslation();
  const navigate = useNavigate();
  const clearTokens = useAuthStore((state) => state.clearTokens);
  const inputId = useId();
  const [{ password, confirming, error }, dispatch] = useReducer(deleteAccountReducer, {
    password: "",
    confirming: false,
    error: null,
  });

  const remove = useMutation({
    ...deleteAuthMeMutation(),
    onSuccess: () => {
      toast.success(t("security.delete_account.success"));
      clearTokens();
      navigate("/login");
    },
    onError: (failure) =>
      dispatch({ type: "refused", error: accountDeletionError(failure, t, t("security.delete_account.failed")) }),
  });
  // A pending request can't be dismissed: the dialog stays until its answer
  const { onOpenChange, onEscapeKeyDown, onPointerDownOutside } = dismissWhileIdle(remove.isPending, () =>
    dispatch({ type: "cancel" })
  );

  const ask = (event: React.FormEvent) => {
    event.preventDefault();
    dispatch({ type: "ask" });
  };

  return (
    <Card className="mb-6 mt-6 border-destructive/50">
      <CardHeader>
        <CardTitle>{t("security.delete_account.title")}</CardTitle>
        <CardDescription>{t("security.delete_account.description")}</CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={ask} className="flex flex-col gap-4 max-w-xs">
          <div className="flex flex-col gap-2">
            <Label htmlFor={inputId}>{t("security.delete_account.password_label")}</Label>
            <PasswordInput
              id={inputId}
              autoComplete="current-password"
              value={password}
              onChange={(event) => dispatch({ type: "typed", password: event.target.value })}
            />
          </div>
          {error && (
            <Alert variant="destructive">
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}
          <Button type="submit" variant="destructive" className="self-start" disabled={password === ""}>
            {t("security.delete_account.button")}
          </Button>
        </form>
      </CardContent>

      <Dialog open={confirming} onOpenChange={onOpenChange}>
        <DialogContent
          showCloseButton={!remove.isPending}
          onEscapeKeyDown={onEscapeKeyDown}
          onPointerDownOutside={onPointerDownOutside}
        >
          <DialogHeader>
            <DialogTitle>{t("security.delete_account.title")}</DialogTitle>
            <DialogDescription>{t("security.delete_account.dialog_description")}</DialogDescription>
          </DialogHeader>
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" disabled={remove.isPending} onClick={() => dispatch({ type: "cancel" })}>
              {t("common.cancel")}
            </Button>
            <Button
              type="button"
              variant="destructive"
              disabled={remove.isPending}
              onClick={() => remove.mutate({ body: { password } })}
            >
              {t("security.delete_account.button")}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </Card>
  );
}
