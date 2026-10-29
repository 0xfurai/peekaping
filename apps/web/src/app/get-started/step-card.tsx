import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

export const STEP_COUNT = 4;

// One step of Get started: its number, title, description and body.
export function StepCard({
  step,
  title,
  description,
  children,
}: {
  step: number;
  title: string;
  description: string;
  children: React.ReactNode;
}) {
  const { t } = useLocalizedTranslation();
  return (
    <Card>
      <CardHeader>
        <p className="text-sm text-muted-foreground">
          {t("get_started.step_of", { current: step, total: STEP_COUNT })}
        </p>
        <CardTitle>{title}</CardTitle>
        <CardDescription>{description}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">{children}</CardContent>
    </Card>
  );
}
