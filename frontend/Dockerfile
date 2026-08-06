# syntax=docker/dockerfile:1.7
# Frontend (Next.js) production image for Cloud Run.
# Build context is this directory (frontend/):
#   docker buildx build --platform linux/amd64 \
#     --build-arg NEXT_PUBLIC_KIWI_API_URL=https://api.runkiwi.dev \
#     --build-arg NEXT_PUBLIC_POSTHOG_KEY=phc_...   # optional, see below \
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
# Product analytics, opt-in. Unlike the API URL these are deliberately NOT
# required: with the key unset the app never imports posthog-js, which is what
# a self-hosted build should do. Only our own images pass them.
ARG NEXT_PUBLIC_POSTHOG_KEY
ARG NEXT_PUBLIC_POSTHOG_HOST
ENV NEXT_PUBLIC_POSTHOG_KEY=$NEXT_PUBLIC_POSTHOG_KEY
ENV NEXT_PUBLIC_POSTHOG_HOST=$NEXT_PUBLIC_POSTHOG_HOST
ENV NEXT_TELEMETRY_DISABLED=1

# Fail the build when the API URL is missing, rather than shipping a dashboard
# that silently points at localhost.
#
# NEXT_PUBLIC_* is inlined at build time, and the app falls back to
# http://localhost:8080 when the variable is unset. That build succeeds, passes
# a smoke test on the builder's own machine, and is broken for every real user —
# which is exactly how it reached production once. A build arg that is required
# should be enforced by the build, not by whoever remembers it.
RUN test -n "$NEXT_PUBLIC_KIWI_API_URL" || { \
      echo "ERROR: NEXT_PUBLIC_KIWI_API_URL build arg is required."; \
      echo "  docker buildx build --build-arg NEXT_PUBLIC_KIWI_API_URL=https://api.runkiwi.dev ..."; \
      exit 1; \
    }

RUN --mount=type=cache,target=/app/.next/cache \
    npm run build

# Prove the value actually landed in the bundle. The env var being set is not
# the same claim as the URL being inlined: a renamed variable, a changed
# fallback, or a stale Next cache would each satisfy the check above and still
# ship localhost. This asserts the artifact, not the intent.
RUN set -e; \
    if grep -rqF 'localhost:8080' .next/static; then \
      echo "ERROR: the built bundle still contains localhost:8080."; \
      echo "  NEXT_PUBLIC_KIWI_API_URL was set to '$NEXT_PUBLIC_KIWI_API_URL' but did not take effect."; \
      exit 1; \
    fi; \
    host=$(printf '%s' "$NEXT_PUBLIC_KIWI_API_URL" | sed -e 's|^[a-z]*://||' -e 's|/.*$||'); \
    grep -rqF "$host" .next/static || { \
      echo "ERROR: '$host' does not appear anywhere in the built bundle."; \
      exit 1; \
    }; \
    echo "verified: bundle points at $NEXT_PUBLIC_KIWI_API_URL"

FROM node:20-alpine AS run
WORKDIR /app
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1 PORT=8080 HOSTNAME=0.0.0.0
# Standalone bundle already contains the minimal node_modules it needs.
COPY --from=build /app/.next/standalone ./
COPY --from=build /app/.next/static ./.next/static
COPY --from=build /app/public ./public
EXPOSE 8080
CMD ["node", "server.js"]
