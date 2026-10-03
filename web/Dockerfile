# Three stages so the runtime image carries only the standalone bundle
# (192 MB limit).

FROM node:22-alpine AS deps
WORKDIR /app
COPY package.json package-lock.json* ./
RUN npm ci

FROM node:22-alpine AS build
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .
ENV NEXT_TELEMETRY_DISABLED=1
RUN npm run build

FROM node:22-alpine AS runner
WORKDIR /app
ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1

# `output: 'standalone'` emits a server bundle plus only the node_modules it
# actually needs.
COPY --from=build /app/.next/standalone ./
COPY --from=build /app/.next/static ./.next/static
COPY --from=build /app/public ./public

# Run unprivileged. The container also runs with a read-only root filesystem
# (see docker-compose.yml), so anything needing writes gets a tmpfs mount.
USER node
EXPOSE 3000
CMD ["node", "server.js"]
