# syntax=docker/dockerfile:1
#
# The Lewbox orchestrator image: the API, the web console and the docs in one
# binary, running `lewbox serve` as an unprivileged user.
#
#   make docker                  # the runtime image, lewbox:local
#   make docker-test             # gofmt, go vet, the tests and the web tests
#
# Both go through Docker Compose. See docs/setup/run-with-docker.md and
# deploy/docker/README.md.

# --- web console -----------------------------------------------------------
FROM node:24-alpine AS web
WORKDIR /src/web
# The UI package is a local tarball under web/vendor, so it has to be here
# before npm ci runs.
COPY web/package.json web/package-lock.json ./
COPY web/vendor ./vendor
RUN npm ci
COPY web ./
RUN npm run build

# The console's own tests. Built only on request.
FROM web AS web-test
RUN npm test

# --- Go binary -------------------------------------------------------------
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
# The cache is owned by the unprivileged user the test stage runs as; root can
# still write to it here.
RUN --mount=type=cache,target=/go/pkg/mod,uid=10001,gid=10001 go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod,uid=10001,gid=10001 \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/lewbox ./cmd/lewbox

# --- checks ----------------------------------------------------------------
# Some CLI tests behave differently as root, so this stage drops to an
# unprivileged user with a writable HOME and Go caches.
FROM build AS test
RUN addgroup -g 10001 -S lewbox \
 && adduser -u 10001 -S -D -G lewbox -h /home/lewbox lewbox \
 && mkdir -p /home/lewbox/.cache/go-build /home/lewbox/.config /home/lewbox/.local/share \
 && chown -R lewbox:lewbox /home/lewbox
USER lewbox
ENV HOME=/home/lewbox \
    GOCACHE=/home/lewbox/.cache/go-build \
    XDG_CACHE_HOME=/home/lewbox/.cache \
    XDG_CONFIG_HOME=/home/lewbox/.config \
    XDG_DATA_HOME=/home/lewbox/.local/share
RUN --mount=type=cache,target=/go/pkg/mod,uid=10001,gid=10001 \
    --mount=type=cache,target=/home/lewbox/.cache/go-build,uid=10001,gid=10001 \
    test -z "$(gofmt -l .)" && go vet ./... && go test ./...

# --- runtime ---------------------------------------------------------------
FROM alpine:3.22 AS runtime
ARG VERSION=dev
RUN apk add --no-cache ca-certificates tzdata \
 && addgroup -g 10001 -S lewbox \
 && adduser -u 10001 -S -D -G lewbox -h /data lewbox \
 && mkdir -p /data \
 && chown 10001:10001 /data

COPY --from=build /out/lewbox /usr/local/bin/lewbox

LABEL org.opencontainers.image.title="Lewbox" \
      org.opencontainers.image.description="The Lewbox orchestrator: API, web console and docs for your AerolVM clusters" \
      org.opencontainers.image.source="https://github.com/harlew-dev/lewbox" \
      org.opencontainers.image.version="${VERSION}"

# The database and the keyring both land under /data, which is the volume.
ENV XDG_DATA_HOME=/data \
    LEWBOX_LISTEN=:8080

VOLUME /data
EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/v0/health || exit 1

USER lewbox:lewbox
ENTRYPOINT ["lewbox"]
CMD ["serve"]
