import { type Env, handleRequest } from "./handler";

export default { fetch: handleRequest } satisfies ExportedHandler<Env>;
