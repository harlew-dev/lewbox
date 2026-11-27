# Contributing

Thanks for helping with Lewbox.

Read [AGENTS.md](AGENTS.md) first. It covers the code layout, the rules every change follows, and the checks that must pass. The web console has its own guide in [web/README.md](web/README.md).

## Build and test

```bash
make           # build the web console, then bin/lewbox
make run       # build, then serve on :8080
make test      # static checks, Go tests with the race detector, and web tests
make check     # static checks only
```

## A test cluster

Launch an orchestrator and two real AerolVM nodes in Docker, then seed them with a plan, a workspace, a sandbox key, sandboxes and a snapshot. It needs Docker with privileged containers and takes about 3 minutes.

```bash
cd deploy/dev-cluster
./dev.sh up        # build the images, start the orchestrator and two nodes
./dev.sh cluster   # install AerolVM on both nodes as cluster "lab"
./dev.sh seed      # fill the console with test data
./dev.sh down      # when you're done: stop everything and delete its volumes
```

Open http://127.0.0.1:8080 and sign in as `admin@example.com` (the operator console at `/admin`) or `customer@example.com` (the customer portal), both with password `password1234`. If installing AerolVM fails with `Could not resolve 'archive.ubuntu.com'`, set `LEWBOX_DEV_DNS="1.1.1.1 8.8.8.8"` and start again. The full guide is in [deploy/dev-cluster](deploy/dev-cluster/README.md).

Changes that touch a machine also need the end-to-end test against real AerolVM in Docker: [deploy/dev-cluster](deploy/dev-cluster/README.md).

## Releasing

Releases follow [RELEASING.md](RELEASING.md). Never publish by hand.
