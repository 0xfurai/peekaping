FROM node:22-alpine AS builder

WORKDIR /app

# Install Bun, at the version pinned in package.json
COPY --from=oven/bun:1.4.4-alpine /usr/local/bin/bun /usr/local/bin/bun

# Copy workspace configuration and lock file from monorepo root
COPY package.json bun.lock ./

# Copy package.json files from all apps for dependency resolution
COPY apps/web/package.json ./apps/web/

# Install dependencies from monorepo root
RUN bun install --frozen-lockfile --filter web

# Copy web app source code
COPY apps/web ./apps/web/

# Build the web app
RUN bun run --filter web build

# Stage 2: Serve with Nginx, as the non-root user nginx (UID 101). The
# unprivileged image keeps nginx's pid file and temporary files in /tmp.
FROM nginxinc/nginx-unprivileged:1.30-alpine

# Copy built assets from builder. The entrypoint writes env.js into this
# folder at each start, so nginx owns the folder. COPY --chown doesn't change a
# folder that exists already, as this one does in the base image.
COPY --from=builder /app/apps/web/dist /usr/share/nginx/html
USER root
RUN chown nginx:nginx /usr/share/nginx/html
USER nginx

COPY --chmod=755 apps/web/docker-entrypoint.sh /docker-entrypoint.d/01-envsubst.sh

COPY apps/web/nginx.conf /etc/nginx/conf.d/default.conf

# nginx binds port 80 without root because a container's
# net.ipv4.ip_unprivileged_port_start is 0 on Docker. Kubernetes needs the
# pod sysctl (the Helm chart sets it).
EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
