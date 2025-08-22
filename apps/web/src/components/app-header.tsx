import { Separator } from "@/components/ui/separator";
import { SidebarTrigger } from "@/components/ui/sidebar";
import { Button } from "./ui/button";
import { PlusIcon } from "lucide-react";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

export function SiteHeader({
  pageName,
  onCreate,
}: {
  pageName: string;
  onCreate?: () => void;
  }) {
  const { t } = useLocalizedTranslation();

  return (
    <header className="group-has-data-[collapsible=icon]/sidebar-wrapper:h-12 flex h-12 shrink-0 items-center gap-2 border-b transition-[width,height] ease-linear">
      <div className="flex w-full items-center gap-1 px-4 lg:gap-2 lg:px-6">
        <SidebarTrigger className="-ml-1" />
        <Separator
          orientation="vertical"
          className="mx-2 data-[orientation=vertical]:h-4"
        />
        <h1 className="text-base font-medium">{pageName}</h1>
      </div>
      {onCreate && (
        <div className="px-4">
          <Button size="sm" onClick={onCreate} data-testid="create-entity">
            <PlusIcon />
            {t("common.create")}
          </Button>
        </div>
      )}
    </header>
  );
}
