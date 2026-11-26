import { Card, CardContent } from "@/components/ui/card";
import { useMonitorFormContext } from "../../context/monitor-form-context";
import Tags from "../shared/tags";
import Probe from "../shared/probe";
import LocationRule from "../shared/location-rule";
import General from "../shared/general";
import Intervals from "../shared/intervals";
import Notifications from "../shared/notifications";
import SMTPSettings from "./settings";
import Authentication from "./authentication";
import OpenRelay from "./open-relay";

const SmtpForm = () => {
  const { setNotifierSheetOpen, isPending } = useMonitorFormContext();

  return (
    <>
      <fieldset disabled={isPending} className="space-y-6">
        <Card>
          <CardContent className="space-y-4">
            <General />
          </CardContent>
        </Card>

        <Card>
          <CardContent className="space-y-4">
            <SMTPSettings />
          </CardContent>
        </Card>

        <Card>
          <CardContent className="space-y-4">
            <Authentication />
          </CardContent>
        </Card>

        <Card>
          <CardContent className="space-y-4">
            <OpenRelay />
          </CardContent>
        </Card>

        <Card>
          <CardContent className="space-y-4">
            <Tags />
            <Probe />
            <LocationRule />
          </CardContent>
        </Card>

        <Card>
          <CardContent className="space-y-4">
            <Notifications onNewNotifier={() => setNotifierSheetOpen(true)} />
          </CardContent>
        </Card>

        <Card>
          <CardContent className="space-y-4">
            <Intervals />
          </CardContent>
        </Card>
      </fieldset>
    </>
  );
};

export default SmtpForm;
