import { Badge } from "@/components/ui/badge";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const ProbeStatusBadge = ({ status }: { status: string }) => {
  const { t } = useLocalizedTranslation();

  return status === "online" ? (
    <Badge className="border-transparent bg-green-600 text-white">
      {t("probes.table.status.online")}
    </Badge>
  ) : (
    <Badge variant="secondary">{t("probes.table.status.offline")}</Badge>
  );
};

export default ProbeStatusBadge;
