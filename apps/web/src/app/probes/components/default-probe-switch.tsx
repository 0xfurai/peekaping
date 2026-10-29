import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  getProbesByIdQueryKey,
  getProbesQueryKey,
  putProbesByIdMutation,
} from "@/api/@tanstack/react-query.gen";
import type { ProbeProbeResponse } from "@/api/types.gen";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { setProbeDefaultRequest } from "@/lib/probe-default";
import { commonMutationErrorHandler } from "@/lib/utils";

// The card of a probe's page that says whether new monitors run on it by default (the Default
// probe of GLOSSARY.md). Existing monitors keep their probes, which the text says.
export function DefaultProbeCard({
  checked,
  pending = false,
  onChange,
}: {
  checked: boolean;
  pending?: boolean;
  onChange: (checked: boolean) => void;
}) {
  const { t } = useLocalizedTranslation();
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("probes.default.title")}</CardTitle>
        <CardDescription>{t("probes.default.description")}</CardDescription>
      </CardHeader>
      <CardContent className="flex items-center gap-3">
        <Switch
          id="probe-is-default"
          data-testid="probe-default-switch"
          checked={checked}
          disabled={pending}
          onCheckedChange={onChange}
        />
        <Label htmlFor="probe-is-default">{t("probes.default.label")}</Label>
      </CardContent>
    </Card>
  );
}

// The default switch of a probe, which saves on every change (PUT /probes/{id} with is_default).
export default function DefaultProbeSwitch({ probe }: { probe: ProbeProbeResponse }) {
  const { t } = useLocalizedTranslation();
  const queryClient = useQueryClient();
  const update = useMutation(putProbesByIdMutation());

  const change = (isDefault: boolean) =>
    update.mutate(setProbeDefaultRequest(probe.id, isDefault), {
      onSuccess: () => {
        queryClient.invalidateQueries({ queryKey: getProbesQueryKey() });
        queryClient.invalidateQueries({ queryKey: getProbesByIdQueryKey({ path: { id: probe.id } }) });
        toast.success(t(isDefault ? "probes.default.on" : "probes.default.off", { name: probe.name }));
      },
      onError: commonMutationErrorHandler(t("probes.default.failed")),
    });

  return <DefaultProbeCard checked={probe.is_default} pending={update.isPending} onChange={change} />;
}
