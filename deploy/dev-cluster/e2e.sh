#!/usr/bin/env bash
# End-to-end test of the Lewbox CLI against real AerolVM nodes in Docker.
#
#   ./e2e.sh           # fresh dev cluster, run every step, tear it down
#   ./e2e.sh --keep    # leave it running afterwards (for a live look)
#   ./e2e.sh --reuse   # skip down/up/install; run the checks on the running cluster
#
# Each step prints PASS or FAIL; the script exits 1 on the first failure.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
KEEP=false REUSE=false
for a in "$@"; do case "$a" in --keep) KEEP=true ;; --reuse) REUSE=true KEEP=true ;; *) echo "unknown flag $a" >&2; exit 2 ;; esac; done

dev() { "$HERE/dev.sh" "$@"; }
compose() { "$HERE/dev.sh" compose "$@"; }
lb() { local n="$1"; shift; compose exec -T -u lewbox "$n" lewbox "$@"; }
API="http://127.0.0.1:${LEWBOX_DEV_PORT:-8080}"
CLUSTER="${LEWBOX_DEV_CLUSTER:-lab}"
STEP=0
pass() { printf '\033[32mPASS\033[0m %s\n' "$*"; }
fail() { printf '\033[31mFAIL\033[0m %s\n' "$*"; exit 1; }
step() { STEP=$((STEP + 1)); printf '\n\033[1m[%d] %s\033[0m\n' "$STEP" "$*"; }
cleanup() { $KEEP || { echo; echo "tearing down (use --keep to leave it running)"; dev down >/dev/null 2>&1 || true; }; }
trap cleanup EXIT

# expect <description> <jq filter that must be true> <json>
expect() { if jq -e "$2" >/dev/null <<<"$3"; then pass "$1"; else echo "$3" | jq . >&2 || echo "$3" >&2; fail "$1"; fi; }

# --- bring it up -----------------------------------------------------------
if ! $REUSE; then
	step "fresh dev cluster"
	dev down >/dev/null 2>&1 || true
	dev up
	pass "orchestrator and nodes up"

	step "the CLI refuses to run as root"
	if compose exec -T node-1 lewbox install --dry-run >/dev/null 2>&1; then fail "root was allowed"; else pass "root refused"; fi

	step "install --dry-run changes nothing"
	out=$(lb node-1 install --public-host 172.30.0.11 --dry-run 2>&1) || { echo "$out"; fail "dry run failed"; }
	compose exec -T node-1 test ! -e /usr/local/bin/sandboxd && pass "no sandboxd after dry run" || fail "dry run installed something"

	step "log in, install AerolVM on both nodes, start a cluster on node-1, join node-2"
	dev cluster
	pass "cluster flow finished"
fi

# --- check what Lewbox sees ------------------------------------------------
step "cluster $CLUSTER is ready with two alive nodes"
c=$(lb node-1 cluster check "$CLUSTER" --json)
expect "status ready" '.status == "ready"' "$c"
expect "2 nodes" '.nodes == 2' "$c"
expect "both alive" '[.node_list[] | select(.alive)] | length == 2' "$c"
expect "every check passed" '[.checks[] | select(.ok | not)] | length == 0' "$c"
expect "runtime docker, AerolVM v0.7.0" '.runtime == "docker" and .version == "v0.7.0"' "$c"

step "node-2 sees the same cluster (it joined with --cluster node-1, a node name)"
r=$(lb node-2 node list --cluster "$CLUSTER" --json)
expect "node list from node-2" '[.data[].name] | sort == ["node-1","node-2"]' "$r"
expect "both in $CLUSTER" "[.data[] | select(.cluster == \"$CLUSTER\")] | length == 2" "$r"

step "the orchestrator API agrees (as the admin, from the host)"
tok=$(curl -fsS -X POST "$API/v0/auth/login" -H 'Content-Type: application/json' \
	-d "{\"email\":\"${LEWBOX_DEV_EMAIL:-admin@lewbox.dev}\",\"password\":\"${LEWBOX_DEV_PASSWORD:-dev-password-123}\"}" | jq -r .token)
[[ $tok == lbs_* ]] && pass "login over HTTP" || fail "login over HTTP"
cl=$(curl -fsS -H "Authorization: Bearer $tok" "$API/v0/clusters")
expect "GET /v0/clusters lists $CLUSTER" "[.data[] | select(.slug == \"$CLUSTER\" and .status == \"ready\")] | length == 1" "$cl"
code=$(curl -s -o /dev/null -w '%{http_code}' "$API/v0/clusters")
[[ $code == 401 ]] && pass "unauthenticated → 401" || fail "unauthenticated got $code"
expect "the admin token is never returned" '[.. | strings | select(startswith("avm_"))] | length == 0' "$cl"

step "Lewbox's settings are on every node, in Lewbox's own files"
for n in node-1 node-2; do
	compose exec -T "$n" grep -qx 'SB_IDENTITY_ENABLED=true' /etc/sandboxd/lewbox.env && pass "$n identity on" || fail "$n identity"
	compose exec -T "$n" test -f /etc/systemd/system/sandboxd.service.d/lewbox.conf && pass "$n drop-in" || fail "$n drop-in"
	compose exec -T "$n" sh -c '! grep -q SB_IDENTITY_ENABLED /etc/sandboxd/sandboxd.env' && pass "$n installer env untouched" || fail "$n sandboxd.env edited"
done

step "the PAT never reached the CLI's output"
for n in node-1 node-2; do
	pat=$(compose exec -T "$n" sh -c "grep ^SB_PAT_TOKEN= /etc/sandboxd/sandboxd.env | cut -d= -f2")
	if [[ -f "$HERE/.cache/install-$n.log" ]] && grep -qF "$pat" "$HERE/.cache/install-$n.log"; then fail "$n: PAT in install output"; fi
	pass "$n: PAT masked"
done

step "sandboxes run on the cluster (runner key made on node-1, used through node-2)"
pat=$(compose exec -T node-1 sh -c "grep ^SB_PAT_TOKEN= /etc/sandboxd/sandboxd.env | cut -d= -f2")
runner=$(compose exec -T node-1 curl -fsS -X POST -H "Authorization: Bearer $pat" -H 'Content-Type: application/json' \
	http://127.0.0.1:21212/v1/tokens -d '{"name":"e2e-runner","role":"runner","team":"e2e","expires_in":"1h"}' | jq -r .token)
for i in $(seq 30); do [[ $(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $runner" http://127.0.0.1:21202/v1/whoami) == 200 ]] && break; sleep 1; done
ids=()
for i in 1 2 3; do
	s=$(curl -fsS -X POST -H "Authorization: Bearer $runner" -H 'Content-Type: application/json' http://127.0.0.1:21202/v1/sandboxes -d "{\"name\":\"e2e-$i\",\"image\":\"alpine:3.20\"}")
	expect "sandbox e2e-$i started" '.status == "started"' "$s"
	ids+=("$(jq -r .id <<<"$s")")
done
on1=$(compose exec -T node-1 docker ps -q | wc -l); on2=$(compose exec -T node-2 docker ps -q | wc -l)
echo "placement: node-1 runs $on1, node-2 runs $on2"
(( on1 + on2 == 3 )) && pass "3 sandbox containers across the cluster" || fail "expected 3 containers, got $on1 + $on2"
for id in "${ids[@]}"; do curl -fsS -o /dev/null -X DELETE -H "Authorization: Bearer $runner" "http://127.0.0.1:21201/v1/sandboxes/$id" || true; done

step "node add is safe to run again"
out=$(lb node-2 node add node-2 --cluster node-1 --json) || fail "re-run of node add failed"
expect "re-run reports node-2" '.name == "node-2"' "$out"
c=$(lb node-1 cluster get "$CLUSTER" --json)
expect "still 2 nodes" '.nodes == 2' "$c"

step "a failed check marks the cluster, and it recovers"
compose exec -T node-1 systemctl stop sandboxd
compose exec -T node-2 systemctl stop sandboxd
c=$(lb node-1 cluster check "$CLUSTER" --json || true)
expect "unreachable while sandboxd is stopped" '.status == "unreachable"' "$c"
compose exec -T node-1 systemctl start sandboxd
compose exec -T node-2 systemctl start sandboxd
for i in $(seq 60); do c=$(lb node-1 cluster check "$CLUSTER" --json 2>/dev/null || true); jq -e '.status == "ready"' >/dev/null <<<"$c" 2>/dev/null && break; sleep 2; done
expect "ready again" '.status == "ready"' "$c"

step "destructive commands want --yes"
if lb node-1 node remove node-2 >/dev/null 2>&1; then fail "remove without --yes was allowed"; else pass "remove without --yes refused"; fi

step "remove node-2 from the cluster"
out=$(lb node-1 node remove node-2 --yes --json --force)
expect "removed node-2" '.name == "node-2"' "$out"
c=$(lb node-1 cluster check "$CLUSTER" --json)
expect "one node left" '.nodes == 1' "$c"
m=$(compose exec -T node-1 sh -c 'curl -s -H "Authorization: Bearer $(grep ^SB_PAT_TOKEN= /etc/sandboxd/sandboxd.env | cut -d= -f2)" http://127.0.0.1:21212/v1/cluster/members')
expect "AerolVM no longer lists node-2" '[.members[].node_id] == ["node-1"]' "$m"
if lb node-1 node remove node-1 --yes >/dev/null 2>&1; then fail "removed the last node"; else pass "the last node can't be removed"; fi

step "one machine (node-3): local-mode AerolVM v0.6.13 and an orchestrator, then update --aerolvm to v0.7.0"
# Started now, not with the others, so `dev.sh cluster` didn't join it to $CLUSTER.
compose --profile three up -d --no-build node-3 >/dev/null 2>&1
for i in $(seq 60); do compose exec -T node-3 systemctl is-system-running 2>/dev/null | grep -qE 'running|degraded' && break; sleep 1; done
n3() { compose exec -T -u lewbox -w /home/lewbox node-3 bash -lc "$*"; }
# Where the CLI's own installer puts it, so `update` can replace it without sudo.
n3 'mkdir -p ~/.local/bin && cp /usr/local/bin/lewbox ~/.local/bin/lewbox'
L='~/.local/bin/lewbox'
n3 "$L install --mode local --aerolvm-version v0.6.13 --yes --json" >"$HERE/.cache/install-node-3.log" 2>&1 \
	|| { tail -20 "$HERE/.cache/install-node-3.log"; fail "local install of v0.6.13"; }
v=$(compose exec -T node-3 curl -fsS http://127.0.0.1:21212/health | jq -r .version)
[[ $v == v0.6.13 ]] && pass "AerolVM v0.6.13 on 127.0.0.1" || fail "health says $v"
n3 "nohup $L serve --port 8080 >/tmp/serve.log 2>&1 &"
for i in $(seq 20); do n3 'curl -fsS http://127.0.0.1:8080/v0/health' >/dev/null 2>&1 && break; sleep 1; done
n3 'curl -fsS http://127.0.0.1:8080/v0/health' >/dev/null && pass "orchestrator on the same machine" || fail "lewbox serve on node-3"
c=$(n3 "$L node add --json")
expect "one-node cluster registered" '.slug == "node-3" and .nodes == 1 and .version == "v0.6.13"' "$c"
# A Lewbox "release" whose release.json moves AerolVM to v0.7.0.
n3 'rm -rf /tmp/rel && mkdir /tmp/rel && cp /usr/local/bin/lewbox /tmp/rel/lewbox_linux_amd64 &&
    (cd /tmp/rel && sha256sum lewbox_linux_amd64 > checksums.txt) &&
    echo "{\"version\":\"v0.0.1-e2e\",\"aerolvm_version\":\"v0.7.0\"}" > /tmp/rel/release.json'
out=$(n3 "$L update --aerolvm --release-from /tmp/rel --yes --json" 2>"$HERE/.cache/update-node-3.log") \
	|| { tail -20 "$HERE/.cache/update-node-3.log"; fail "update --aerolvm"; }
v=$(compose exec -T node-3 curl -fsS http://127.0.0.1:21212/health | jq -r .version)
[[ $v == v0.7.0 ]] && pass "AerolVM is now v0.7.0" || fail "health says $v after update"
c=$(n3 "$L cluster check node-3 --json")
expect "Lewbox sees v0.7.0 and the cluster is ready" '.version == "v0.7.0" and .status == "ready"' "$c"
compose exec -T node-3 test -f /etc/sandboxd/lewbox.env && pass "Lewbox settings survived the upgrade" || fail "lewbox.env gone after upgrade"

echo
printf '\033[32mAll %d steps passed.\033[0m\n' "$STEP"
