#!/usr/bin/env bash
# Drive the Lewbox dev cluster. Run ./dev.sh help for the commands.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
AEROLVM_VERSION="${AEROLVM_VERSION:-v0.7.0}"
CACHE="$HERE/.cache/aerolvm"
EMAIL="${LEWBOX_DEV_EMAIL:-admin@lewbox.dev}"
PASSWORD="${LEWBOX_DEV_PASSWORD:-dev-password-123}"
ORCH_URL="http://orchestrator:8080"   # as the nodes see it
CLUSTER="${LEWBOX_DEV_CLUSTER:-lab}"
# The cluster's runtime: docker (default) or firecracker. --runtime on up,
# cluster and e2e sets it; e2e.sh, customers.sh and archives.sh read it.
RUNTIME="${LEWBOX_DEV_RUNTIME:-docker}"

# LEWBOX_DEV_DNS="1.1.1.1 8.8.8.8" makes the nodes forward outside lookups to
# those servers instead of the host's resolver, for networks whose resolver
# fails on some names (the AerolVM installer needs archive.ubuntu.com).
# Container names still resolve through Docker's embedded DNS.
compose() {
	local files=(-f "$HERE/compose.yaml")
	if [[ -n "${LEWBOX_DEV_DNS:-}" ]]; then
		local override="$HERE/.cache/dns.override.yaml" s d
		mkdir -p "$HERE/.cache"
		{
			echo "services:"
			for s in node-1 node-2 node-3; do
				echo "  $s:"
				echo "    dns:"
				for d in $LEWBOX_DEV_DNS; do echo "      - $d"; done
			done
		} >"$override"
		files+=(-f "$override")
	fi
	docker compose "${files[@]}" "$@"
}
say() { printf '\033[1m==> %s\033[0m\n' "$*" >&2; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

# set_runtime <value> checks a --runtime value and exports it for the other
# scripts. Firecracker needs /dev/kvm on the host: the privileged nodes see
# the host's devices.
set_runtime() {
	case "$1" in
		docker | firecracker) RUNTIME="$1" ;;
		*) die "--runtime must be docker or firecracker, not \"$1\"" ;;
	esac
	export LEWBOX_DEV_RUNTIME="$RUNTIME"
	if [[ $RUNTIME == firecracker && ! -c /dev/kvm ]]; then
		die "--runtime firecracker needs /dev/kvm on this host (bare metal, or nested virtualization)"
	fi
}

# node_ip <node> prints the node's address on the cluster network.
node_ip() { case "$1" in node-1) echo 172.30.0.11 ;; node-2) echo 172.30.0.12 ;; node-3) echo 172.30.0.13 ;; *) die "unknown node $1" ;; esac; }

# lb <node> <args...> runs the lewbox CLI on a node as the `lewbox` user.
lb() { local n="$1"; shift; compose exec -T -u lewbox "$n" lewbox "$@"; }

# The AerolVM release files, so nodes install without each downloading ~200 MB.
cmd_fetch() {
	local v="${1:-$AEROLVM_VERSION}" arch dir
	arch="$(dpkg --print-architecture 2>/dev/null || echo amd64)"
	dir="$CACHE/$v"
	mkdir -p "$dir"
	local files=(install.sh checksums.txt "sandboxd_linux_$arch" "toolboxd_linux_$arch" "caddy_linux_$arch" "aerolvm_linux_$arch")
	for f in "${files[@]}"; do
		[[ -s "$dir/$f" ]] && continue
		say "downloading AerolVM $v $f"
		curl -fsSL -o "$dir/$f.part" "https://github.com/aerol-ai/microvm/releases/download/$v/$f"
		mv "$dir/$f.part" "$dir/$f"
	done
	# Every file must match the release's own checksums.
	(cd "$dir" && for f in "${files[@]}"; do [[ $f == checksums.txt ]] && continue; grep -E "  $f\$" checksums.txt; done | sha256sum -c --quiet -) \
		|| die "checksum mismatch in $dir; delete it and fetch again"
	say "AerolVM $v ready in $dir"

	# The node registry (CNCF Distribution) for clusters with a storage
	# profile. `node configure` reads it from $LEWBOX_AEROLVM_FROM/registry.
	local rv=3.1.2 ra rdir="$CACHE/registry"
	ra="registry_${rv}_linux_${arch}.tar.gz"
	mkdir -p "$rdir"
	if [[ ! -s "$rdir/$ra" ]]; then
		say "downloading the registry $rv"
		curl -fsSL -o "$rdir/$ra.part" "https://github.com/distribution/distribution/releases/download/v$rv/$ra"
		curl -fsSL -o "$rdir/$ra.sha256" "https://github.com/distribution/distribution/releases/download/v$rv/$ra.sha256"
		echo "$(tr -d ' \n' <"$rdir/$ra.sha256")  $ra.part" | (cd "$rdir" && sha256sum -c --quiet -) \
			|| die "registry checksum mismatch; delete $rdir and fetch again"
		mv "$rdir/$ra.part" "$rdir/$ra"
	fi
}

cmd_build() {
	say "building lewbox images"
	docker build -q -f "$HERE/Dockerfile" --target node -t lewbox-node:dev "$ROOT" >/dev/null
	docker build -q -f "$HERE/Dockerfile" --target orchestrator -t lewbox:dev "$ROOT" >/dev/null
}

cmd_up() {
	local profile=()
	[[ "${1:-}" == "--three" ]] && profile=(--profile three)
	cmd_fetch
	cmd_build
	say "starting the orchestrator and nodes"
	compose "${profile[@]}" up -d --no-build
	for i in $(seq 60); do curl -fsS "http://127.0.0.1:${LEWBOX_DEV_PORT:-8080}/v0/health" >/dev/null 2>&1 && break; sleep 1; done
	curl -fsS "http://127.0.0.1:${LEWBOX_DEV_PORT:-8080}/v0/health" >/dev/null || die "orchestrator not healthy; ./dev.sh logs orchestrator"
	for n in $(compose "${profile[@]}" ps --services | grep '^node-'); do
		for i in $(seq 60); do compose exec -T "$n" systemctl is-system-running 2>/dev/null | grep -qE 'running|degraded' && break; sleep 1; done
	done
	say "up. Orchestrator: http://127.0.0.1:${LEWBOX_DEV_PORT:-8080}  admin: $EMAIL / $PASSWORD"
	say "next: ./dev.sh cluster   (or ./dev.sh shell node-1)"
}

# login <node> signs the node's lewbox CLI in to the orchestrator.
login() {
	printf '%s\n' "$PASSWORD" | lb "$1" login "$ORCH_URL" --email "$EMAIL" --password-stdin --context-name dev --json >/dev/null
}

# install <node> installs AerolVM on a node in IP mode.
install() { lb "$1" install --public-host "$(node_ip "$1")" --runtime "$RUNTIME" --yes; }

# The whole flow from docs/clusters/start-a-cluster.md: node-1 starts a cluster,
# the other nodes join it.
cmd_cluster() {
	while (($#)); do
		case "$1" in
			--runtime) set_runtime "${2:-}"; shift ;;
			--runtime=*) set_runtime "${1#*=}" ;;
			*) die "cluster: unknown flag $1" ;;
		esac
		shift
	done
	local nodes=(node-1 node-2)
	compose ps --services --status running | grep -qx node-3 && nodes+=(node-3)
	for n in "${nodes[@]}"; do say "$n: lewbox login"; login "$n"; done
	say "installing AerolVM ($RUNTIME) on ${nodes[*]} (in parallel)"
	local pids=()
	for n in "${nodes[@]}"; do install "$n" >"$HERE/.cache/install-$n.log" 2>&1 & pids+=($!); done
	for i in "${!pids[@]}"; do wait "${pids[$i]}" || { tail -30 "$HERE/.cache/install-${nodes[$i]}.log"; die "install failed on ${nodes[$i]}"; }; done
	say "node-1: lewbox node add --new-cluster $CLUSTER"
	lb node-1 node add node-1 --new-cluster "$CLUSTER"
	for n in "${nodes[@]:1}"; do
		say "$n: lewbox node add --cluster node-1"
		lb "$n" node add "$n" --cluster node-1
	done
	say "cluster $CLUSTER:"
	lb node-1 node list --cluster "$CLUSTER"
}

cmd_shell() { compose exec -u lewbox -w /home/lewbox "${1:-node-1}" bash -l; }
cmd_lewbox() { local n="${1:-node-1}"; shift || true; lb "$n" "$@"; }
cmd_logs() { local s="${1:-orchestrator}"; if [[ $s == orchestrator ]]; then compose logs -f orchestrator; else compose exec "$s" journalctl -u sandboxd -f; fi; }
cmd_down() { compose --profile three down -v --remove-orphans; }
cmd_ps() { compose --profile three ps; }
cmd_compose() { compose "$@"; }

cmd_help() {
	cat <<EOF
Usage: ./dev.sh <command>

  fetch [tag]          download AerolVM release files into .cache (default $AEROLVM_VERSION)
  build                build lewbox-node:dev and lewbox:dev
  up [--three]         fetch, build, start the orchestrator and node-1, node-2 (and node-3)
  cluster [--runtime docker|firecracker]
                       log in, install AerolVM on every node, start cluster "$CLUSTER" on node-1, join the rest
  shell [node]         a shell on a node as the lewbox user (default node-1)
  lewbox <node> args   run the lewbox CLI on a node, e.g. ./dev.sh lewbox node-2 node list
  logs [service]       follow the orchestrator's log, or sandboxd's on a node
  ps                   show containers
  down                 stop everything and delete the dev cluster's volumes
  compose <args>       docker compose for this project (with the DNS override, if set)

Environment: LEWBOX_DEV_DNS="1.1.1.1 8.8.8.8" sends the nodes' outside DNS
lookups to those servers, for networks whose resolver fails on some names.
LEWBOX_DEV_RUNTIME=firecracker is the same as --runtime firecracker.

  e2e [--keep] [--runtime docker|firecracker]
                       up, cluster, check it, run sandboxes on it, down (see e2e.sh)
EOF
}

case "${1:-help}" in
	fetch | build | up | cluster | shell | lewbox | logs | ps | down | compose | help) c="$1"; shift || true; "cmd_$c" "$@" ;;
	e2e) shift; exec "$HERE/e2e.sh" "$@" ;;
	*) cmd_help; exit 2 ;;
esac
