# syntax=docker/dockerfile:1.7
FROM golang:1.27.3-bookworm AS go-toolchain
FROM node:24.19.0-bookworm-slim AS build
COPY --from=go-toolchain /usr/local/go /usr/local/go
ENV PATH="/usr/local/go/bin:${PATH}"
WORKDIR /app
RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates && rm -rf /var/lib/apt/lists/*
COPY . .
# The optional CA is mounted only during networked builds in managed environments.
RUN --mount=type=secret,id=proxy_ca \
    if [ -f /run/secrets/proxy_ca ]; then export NODE_EXTRA_CA_CERTS=/run/secrets/proxy_ca GIT_SSL_CAINFO=/run/secrets/proxy_ca SSL_CERT_FILE=/run/secrets/proxy_ca; fi; \
    npm install -g pnpm@10.15.0 && bash scripts/setup-saju.sh && go build -o /tmp/nyangpuli ./cmd/nyangpuli

FROM node:24.19.0-bookworm-slim AS runtime
WORKDIR /app
ENV NODE_ENV=production
COPY --from=build /tmp/nyangpuli /usr/local/bin/nyangpuli
COPY --chown=node:node --from=build /app /app
RUN mkdir -p /data && chown node:node /data
USER node
EXPOSE 8080
CMD ["nyangpuli", "serve", "--addr", "0.0.0.0:8080", "--allow-container-bind", "--readings", "/data/readings.json"]
