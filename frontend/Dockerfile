# syntax=docker/dockerfile:1.7
# Frontend (Next.js) production image for Cloud Run.
# Build context is this directory (frontend/):
#   docker buildx build --platform linux/amd64 \
#     --build-arg NEXT_PUBLIC_KIWI_API_URL=https://api.runkiwi.dev \
#     -t <img> --push ./frontend
#
# NEXT_PUBLIC_* is baked at BUILD time (build-arg), not runtime. The API URL is
# public, so it's fine to keep this file in-repo — there are no secrets here.
#
# Speed: deps + build run on the native $BUILDPLATFORM (no QEMU emulation of
# npm/next), with npm and Next build caches persisted via BuildKit cache
# mounts. The run stage ships Next's standalone output only, so the ~630MB
# node_modules is never copied between stages.
#
# The app has no native *runtime* deps, so building deps on the build arch and
# running on the target arch is safe. If a runtime native dep is ever added,
# drop the two `--platform=$BUILDPLATFORM` lines to build under the target arch.

FROM --platform=$BUILDPLATFORM node:20-alpine AS deps
WORKDIR /app
COPY package.json package-lock.json* ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci

FROM --platform=$BUILDPLATFORM node:20-alpine AS build
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .
# Ensure public/ exists even when the repo has no static assets.
RUN mkdir -p public
ARG NEXT_PUBLIC_KIWI_API_URL
ENV NEXT_PUBLIC_KIWI_API_URL=$NEXT_PUBLIC_KIWI_API_URL
ENV NEXT_TELEMETRY_DISABLED=1
RUN --mount=type=cache,target=/app/.next/cache \
    npm run build

FROM node:20-alpine AS run
WORKDIR /app
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1 PORT=8080 HOSTNAME=0.0.0.0
# Standalone bundle already contains the minimal node_modules it needs.
COPY --from=build /app/.next/standalone ./
COPY --from=build /app/.next/static ./.next/static
COPY --from=build /app/public ./public
EXPOSE 8080
CMD ["node", "server.js"]
