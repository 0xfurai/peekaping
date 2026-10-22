import { z } from "zod";
import { proxiesSchema, proxiesDefaultValues, monitorBaseSchema, monitorBaseDefaultValues } from "../shared/schema";

export const dockerSchema = z
  .object({
    type: z.literal("docker"),
    container_id: z.string().min(1, "Container Name/ID is required"),
    connection_type: z.enum(["socket", "tcp"], {
      required_error: "Connection type is required",
    }),
    docker_daemon: z.string().min(1, "Docker Daemon is required"),
    // TLS fields with proper defaults
    tls_enabled: z.boolean(),
    tls_cert: z.string(),
    tls_key: z.string(),
    tls_ca: z.string(),
    tls_verify: z.boolean(),
  })
  .merge(monitorBaseSchema)
  .merge(proxiesSchema);

export type DockerFormValues = z.infer<typeof dockerSchema>;

export const dockerDefaultValues: DockerFormValues = {
  type: "docker",
  container_id: "",
  connection_type: "socket",
  docker_daemon: "/var/run/docker.sock",
  tls_enabled: false,
  tls_cert: "",
  tls_key: "",
  tls_ca: "",
  tls_verify: true,
  ...monitorBaseDefaultValues,
  ...proxiesDefaultValues,
};
