import { GalleryVerticalEnd } from "lucide-react";
import { useParams } from "react-router-dom";
import { InviteLanding } from "./invite-landing";

const InvitePage = () => {
  const { token = "" } = useParams();
  return (
    <div className="flex min-h-svh flex-col items-center justify-center gap-6 bg-muted p-6 md:p-10">
      <div className="flex w-full max-w-sm flex-col gap-6">
        <div className="flex items-center gap-2 self-center font-medium">
          <div className="flex h-6 w-6 items-center justify-center rounded-md bg-primary text-primary-foreground">
            <GalleryVerticalEnd className="size-4" />
          </div>
          Peekaping
        </div>

        <InviteLanding token={token} />
      </div>
    </div>
  );
};

export default InvitePage;
