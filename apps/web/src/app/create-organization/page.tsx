import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { postOrganizations } from "@/api/sdk.gen";


export default function CreateOrganizationPage() {
    const [name, setName] = useState("");
    const [slug, setSlug] = useState("");
    const navigate = useNavigate();
    // const { setOrganizations } = useOrganizationStore();

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        try {
            const { data } = await postOrganizations({
                body: {
                    name,
                    slug: slug || undefined
                }
            });

            if (data?.data) {
                toast.success("Organization created successfully");
                // Refetch orgs or update store manually?
                // Ideally refetch. Since we don't have global SWR for list yet, we might need to trigger reload
                // or update store manually.
                // For now, reload window or navigate to new org
                if (data.data.slug) {
                    navigate(`/${data.data.slug}/monitors`);
                }
            }
        } catch (error) {
            console.error(error);
            toast.error("Failed to create organization");
        }
    };

    return (
        <div className="flex h-screen w-full items-center justify-center">
            <div className="w-full max-w-md space-y-6 rounded-lg border p-6 shadow-sm">
                <div className="space-y-2 text-center">
                    <h1 className="text-2xl font-bold">Create Organization</h1>
                    <p className="text-muted-foreground">
                        Start by creating a new organization for your monitors.
                    </p>
                </div>
                <form onSubmit={handleSubmit} className="space-y-4">
                    <div className="space-y-2">
                        <Label htmlFor="name">Organization Name</Label>
                        <Input
                            id="name"
                            placeholder="Acme Corp"
                            value={name}
                            onChange={(e) => setName(e.target.value)}
                            required
                        />
                    </div>
                    <div className="space-y-2">
                        <Label htmlFor="slug">Slug (Optional)</Label>
                        <Input
                            id="slug"
                            placeholder="acme-corp"
                            value={slug}
                            onChange={(e) => setSlug(e.target.value)}
                        />
                    </div>
                    <Button type="submit" className="w-full">
                        Create Organization
                    </Button>
                </form>
            </div>
        </div>
    );
}
