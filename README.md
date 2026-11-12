# Lewbox

Run your own sandbox cloud. Lewbox installs [AerolVM](https://github.com/aerol-ai/microvm) on your servers, groups them into regions, and lets other people sign up, create API keys and run isolated sandboxes on your compute with the AerolVM SDK or the E2B SDK. You set plans and limits; Lewbox keeps them in force across every region and records usage.

It's one Go binary: the server (`lewbox serve`: API, web app, background jobs, SQLite) and the CLI that sets up machines.

```bash
make                                   # builds the web app and bin/lewbox
LEWBOX_BOOTSTRAP_ADMIN_EMAIL=you@example.com \
LEWBOX_BOOTSTRAP_ADMIN_PASSWORD=change-me-please \
  bin/lewbox serve                     # http://localhost:8080
```

Then on each server: `lewbox install` and `lewbox node add`. The docs are built into the app at `/docs`, and in [docs/](docs/README.md).

- Operators: [Quickstart](docs/getting-started/quickstart.md), [Run in production](docs/setup/run-in-production.md), [Customers, plans and limits](docs/accounts/customers-and-plans.md)
- Contributors: [AGENTS.md](AGENTS.md), [web/README.md](web/README.md)
- How this was built and why: [plans/](plans/README.md)

MIT licensed.
