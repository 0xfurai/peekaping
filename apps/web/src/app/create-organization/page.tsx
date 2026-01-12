import { OrganizationForm } from "@/components/organization-form";

export default function CreateOrganizationPage() {
    return (
        <div className="flex h-screen w-full items-center justify-center">
            <div className="w-full max-w-md space-y-6 rounded-lg border p-6 shadow-sm">
                <div className="space-y-2 text-center">
                    <h1 className="text-2xl font-bold">Create Organization</h1>
                    <p className="text-muted-foreground">
                        Start by creating a new organization for your monitors.
                    </p>
                </div>
                <OrganizationForm mode="create" />
            </div>
        </div>
    );
}
