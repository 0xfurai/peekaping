import { useLocation } from "react-router-dom";
import { Ban } from "lucide-react";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { useMe } from "@/hooks/useMe";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { contactLink, isSuspendedByAdmin } from "@/lib/admin-workspaces";
import { workspaceIdFromPathname } from "@/lib/workspace-path";

// The banner at the top of a workspace an instance admin suspended (PP-372), for every member: the
// workspace is suspended and how to get in touch, through the install's SUPPORT_CONTACT when /me
// carries one. The hold until the owner verifies their email is the verification banner's.
export function SuspendedWorkspaceBanner() {
  const me = useMe().data?.data;
  const { pathname } = useLocation();
  if (!me || !isSuspendedByAdmin(me.workspaces, workspaceIdFromPathname(pathname))) return null;
  return <SuspendedWorkspaceBannerView contact={me.supportContact} />;
}

export function SuspendedWorkspaceBannerView({ contact }: { contact: string | null | undefined }) {
  const { t } = useLocalizedTranslation();
  const link = contactLink(contact);
  return (
    <Alert variant="destructive">
      <Ban className="h-4 w-4" />
      <AlertDescription className="flex flex-col gap-1">
        <span>{t("workspaces.suspended_banner.message")}</span>
        {link ? (
          <span className="break-all">
            {t("workspaces.suspended_banner.contact")}{" "}
            {link.href ? (
              <a
                href={link.href}
                className="font-medium underline underline-offset-4"
                {...(link.href.startsWith("mailto:")
                  ? {}
                  : { target: "_blank", rel: "noopener noreferrer" })}
              >
                {link.text}
              </a>
            ) : (
              link.text
            )}
          </span>
        ) : (
          <span>{t("workspaces.suspended_banner.no_contact")}</span>
        )}
      </AlertDescription>
    </Alert>
  );
}
