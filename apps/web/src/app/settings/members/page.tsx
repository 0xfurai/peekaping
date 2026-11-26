import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { getWorkspaceMembersOptions } from "@/api/@tanstack/react-query.gen";
import Layout from "@/layout";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { NewWorkspaceDialog } from "@/components/new-workspace-dialog";
import { useCurrentAccess } from "@/hooks/useCurrentAccess";
import { useMe } from "@/hooks/useMe";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { InviteCard } from "./invite-card";
import { LeaveCard } from "./leave-card";
import { MembersCard } from "./members-card";
import { PendingInvitesCard } from "./pending-invites-card";

// The people of the workspace (/w/<id>/settings/members). Everyone sees the list and may leave;
// an admin or owner also invites and manages roles. The server enforces the same rules.
const MembersPage = () => {
  const { t } = useLocalizedTranslation();
  const { role, canManageWorkspace } = useCurrentAccess();
  const me = useMe().data?.data;
  const { data, isLoading, isError } = useQuery(getWorkspaceMembersOptions());
  const [creating, setCreating] = useState(false);
  const members = data?.data ?? [];
  const owners = members.filter((m) => m.role === "owner").length;

  return (
    <Layout pageName={t("members.page_name")} isLoading={isLoading}>
      <div className="max-w-3xl flex flex-col gap-6">
        {canManageWorkspace && role && <InviteCard actor={role} />}
        {canManageWorkspace && <PendingInvitesCard />}
        {isError ? (
          <p>{t("messages.unexpected_error")}</p>
        ) : (
          <MembersCard actor={role} currentUserId={me?.user.id} members={members} />
        )}
        <LeaveCard role={role} owners={owners} />
        {/* With two or more workspaces the switcher has this action */}
        {(me?.workspaces?.length ?? 0) < 2 && (
          <Card>
            <CardHeader>
              <CardTitle>{t("workspaces.new_title")}</CardTitle>
              <CardDescription>{t("workspaces.new_description")}</CardDescription>
            </CardHeader>
            <CardContent>
              <Button type="button" variant="outline" onClick={() => setCreating(true)}>
                {t("workspaces.new")}
              </Button>
            </CardContent>
          </Card>
        )}
      </div>
      <NewWorkspaceDialog open={creating} onOpenChange={setCreating} />
    </Layout>
  );
};

export default MembersPage;
