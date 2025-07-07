FROM node:22-alpine AS builder

WORKDIR /app

# Install pnpm globally
RUN npm install -g pnpm

# Copy workspace configuration and lock file from monorepo root
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./

# Copy patches directory if it exists
COPY patches ./patches/

# Copy package.json files from all apps for dependency resolution
COPY apps/web/package.json ./apps/web/

# Install dependencies from monorepo root
RUN pnpm install --frozen-lockfile

# Copy web app source code
COPY apps/web ./apps/web/

# Build the web app
RUN pnpm --filter web run build

# Stage 2: Serve with Nginx
FROM nginx:alpine

# Copy built assets from builder
COPY --from=builder /app/apps/web/dist /usr/share/nginx/html

COPY apps/web/docker-entrypoint.sh /docker-entrypoint.d/01-envsubst.sh
RUN chmod +x /docker-entrypoint.d/01-envsubst.sh

COPY apps/web/nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
