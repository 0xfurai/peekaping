import { OrganizationForm } from "@/components/organization-form";
import { GalleryVerticalEnd } from "lucide-react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

export default function CreateOrganizationPage() {
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
                            <CardTitle className="text-xl">Create Organization</CardTitle>
                            <CardDescription>
                                Start by creating a new organization for your monitors.
                            </CardDescription>
                        </CardHeader>
                        <CardContent>
                            <OrganizationForm mode="create" />
                        </CardContent>
                    </Card>
                </div>
            </div>
        </div>
    );
}
