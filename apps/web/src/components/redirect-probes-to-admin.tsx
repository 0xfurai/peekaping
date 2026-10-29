import { Navigate, useParams } from "react-router-dom";

// The probe pages used to live in each workspace (/w/<id>/probes). They belong to the install, so
// they are in the admin area now (/admin/probes), and an old address, a bookmark say, goes there.
// The caller puts it behind RequireInstanceAdmin: anyone else goes to the workspace's monitors.
export function RedirectProbesToAdmin() {
  const { id } = useParams();
  return <Navigate to={id ? `/admin/probes/${id}` : "/admin/probes"} replace />;
}
