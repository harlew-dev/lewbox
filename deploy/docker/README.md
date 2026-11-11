# Lewbox in Docker

The repo root has the production `Dockerfile` and `compose.yaml` for the
orchestrator (API, web console and docs). The user guide is
[Run with Docker](../../docs/setup/run-with-docker.md); this page is the
contributor's short version.

Everything goes through Docker Compose, which builds with the daemon's
BuildKit. You do **not** need the Docker `buildx` plugin.

```bash
# The runtime image, lewbox:local.
make docker

# Every check inside the image: gofmt, go vet, Go tests and the web tests.
make docker-test

# Run it locally on http://localhost:8080.
LEWBOX_ADMIN_PASSWORD='a-12-character-password' make up

# Stop it, keeping the data volume.
make down

# End-to-end smoke test against the real container (builds lewbox:local).
make e2e
```

`make e2e` runs `deploy/docker/e2e.sh`. Set `SKIP_BUILD=1` to reuse an image
built earlier. It removes its container and anonymous volume on exit.

`deploy/dev-cluster/` is a different thing: a multi-node AerolVM test bed.
See [Dev cluster](../dev-cluster/README.md).
