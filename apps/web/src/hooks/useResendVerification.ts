import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { getMeQueryKey, postAuthVerifyEmailResendMutation } from "@/api/@tanstack/react-query.gen";
import { accountEmailFailure } from "@/lib/account-email";

export type ResendOutcome = "idle" | "sent" | "rate_limited" | "error" | "already_verified";

// Sends the signed-in user a new verification link. A 409 means the address is verified already
// (from another tab, say): /me is fetched again so the app stops asking.
export const useResendVerification = () => {
  const queryClient = useQueryClient();
  const [outcome, setOutcome] = useState<ResendOutcome>("idle");

  const mutation = useMutation({
    ...postAuthVerifyEmailResendMutation(),
    onSuccess: () => setOutcome("sent"),
    onError: (error) => {
      const failure = accountEmailFailure(error);
      if (failure === "already_verified") {
        queryClient.invalidateQueries({ queryKey: getMeQueryKey() });
        setOutcome("already_verified");
      } else {
        setOutcome(failure === "rate_limited" ? "rate_limited" : "error");
      }
    },
  });

  return {
    outcome,
    pending: mutation.isPending,
    resend: () => {
      setOutcome("idle");
      mutation.mutate({});
    },
  };
};
