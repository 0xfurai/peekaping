FROM node:22-alpine AS builder

WORKDIR /app

# Install Bun, at the version pinned in package.json
COPY --from=oven/bun:1.4.3-alpine /usr/local/bin/bun /usr/local/bin/bun

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

# Stage 2: Serve with Nginx
FROM nginx:alpine

# Copy built assets from builder
COPY --from=builder /app/apps/web/dist /usr/share/nginx/html

COPY apps/web/docker-entrypoint.sh /docker-entrypoint.d/01-envsubst.sh
RUN chmod +x /docker-entrypoint.d/01-envsubst.sh

COPY apps/web/nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
