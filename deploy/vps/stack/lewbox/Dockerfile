# The Lewbox orchestrator image, built on the server from a published release
# binary that lewbox-deploy has already checked against the release's
# checksums.txt. Same runtime as the repo-root Dockerfile's `runtime` stage.
FROM alpine:3.22
ARG VERSION=dev
RUN apk add --no-cache ca-certificates tzdata \
 && addgroup -g 10001 -S lewbox \
 && adduser -u 10001 -S -D -G lewbox -h /data lewbox \
 && mkdir -p /data \
 && chown 10001:10001 /data

COPY --chmod=0755 lewbox /usr/local/bin/lewbox

LABEL org.opencontainers.image.title="Lewbox" \
      org.opencontainers.image.source="https://github.com/harlew-dev/lewbox" \
      org.opencontainers.image.version="${VERSION}"

ENV XDG_DATA_HOME=/data \
    LEWBOX_LISTEN=:8080

EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/v0/health || exit 1

USER 10001:10001
ENTRYPOINT ["lewbox"]
CMD ["serve"]
