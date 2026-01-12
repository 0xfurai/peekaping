import { OrganizationForm } from "@/components/organization-form";
import { GalleryVerticalEnd } from "lucide-react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { LanguageSelector } from "@/components/LanguageSelector";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

export default function CreateOrganizationPage() {
    const { t } = useLocalizedTranslation();

    return (
        <div className="flex min-h-svh flex-col items-center justify-center gap-6 bg-muted p-6 md:p-10">
            <div className="flex w-full max-w-sm flex-col gap-6">
                <a href="#" className="flex items-center gap-2 self-center font-medium">
                    <div className="flex h-6 w-6 items-center justify-center rounded-md bg-primary text-primary-foreground">
                        <GalleryVerticalEnd className="size-4" />
                    </div>
                    Vigi
                </a>

                <div className="flex flex-col gap-6">
                    <Card>
                        <CardHeader className="text-center">
                            <CardTitle className="text-xl">{t("organization.create_title")}</CardTitle>
                            <CardDescription>
                                {t("organization.create_description")}
                            </CardDescription>
                        </CardHeader>
                        <CardContent>
                            <OrganizationForm mode="create" />
                        </CardContent>
                    </Card>

                    <div className="w-[180px] self-center">
                        <LanguageSelector />
                    </div>
                </div>
            </div>
        </div>
    );
}
