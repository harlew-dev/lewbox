# Images for the Lewbox dev cluster (see README.md). Build from the repo root:
#
#   docker build -f deploy/dev-cluster/Dockerfile --target node -t lewbox-node:dev .
#   docker build -f deploy/dev-cluster/Dockerfile --target orchestrator -t lewbox:dev .
#
# compose.yaml does both.

# The web console, which the binary embeds. .dockerignore leaves out a local
# web/dist, so it is built here, the same way the repo-root Dockerfile does.
FROM node:24-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
COPY web/vendor ./vendor
RUN npm ci
COPY web ./
RUN npm run build

FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/lewbox ./cmd/lewbox \
 && mkdir -p /out/data

# ---------------------------------------------------------------------------
# orchestrator: just the lewbox binary, running `lewbox serve`.
FROM gcr.io/distroless/static-debian12:nonroot AS orchestrator
COPY --from=build /out/lewbox /usr/local/bin/lewbox
COPY --from=build --chown=nonroot:nonroot /out/data /data
ENV LEWBOX_DB=/data/lewbox.db LEWBOX_KEYRING_FILE=/data/keyring.key
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/lewbox"]
CMD ["serve", "--port", "8080"]

# ---------------------------------------------------------------------------
# node: a "machine" for one AerolVM node. Ubuntu 24.04 with systemd as PID 1
# and its own Docker engine, so each node is isolated from the host and from
# the other nodes. It must run privileged (compose.yaml).
#
# Nothing AerolVM is baked in: `lewbox install` installs it, the same way it
# would on a real server.
FROM ubuntu:24.04 AS node
ENV container=docker DEBIAN_FRONTEND=noninteractive
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      systemd systemd-sysv dbus ca-certificates curl jq iproute2 iptables \
      sudo docker.io kmod procps less openssl e2fsprogs netcat-openbsd file ufw \
 && apt-get clean && rm -rf /var/lib/apt/lists/* \
 # The base image stops apt from starting services; a real machine doesn't,
 # and the AerolVM installer expects the services it installs to start.
 && rm -f /usr/sbin/policy-rc.d

# Units that need real hardware or a console and only fail in a container.
RUN systemctl mask \
      systemd-udevd.service systemd-udevd-kernel.socket systemd-udevd-control.socket \
      systemd-modules-load.service sys-kernel-config.mount sys-kernel-debug.mount \
      sys-kernel-tracing.mount systemd-remount-fs.service getty.target \
      console-getty.service systemd-firstboot.service systemd-logind.service \
 && systemctl enable docker.service

# The CLI refuses to run as root, like on a real machine. Work as `lewbox`,
# which has passwordless sudo for the steps that need it.
RUN useradd -m -s /bin/bash -G sudo,docker lewbox \
 && echo 'lewbox ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/lewbox \
 && chmod 0440 /etc/sudoers.d/lewbox

COPY --from=build /out/lewbox /usr/local/bin/lewbox

# Docker inside Docker can't keep its layers on overlayfs-on-overlayfs.
# Docker 29 keeps images in containerd's store, and sandboxd mounts sandbox
# disks under its own data dir, so all three need a real filesystem.
VOLUME /var/lib/docker /var/lib/containerd /var/lib/sandboxd
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
