#!/usr/bin/env bash
# End-to-end test of the Lewbox CLI against real AerolVM nodes in Docker.
#
#   ./e2e.sh           # fresh dev cluster, run every step, tear it down
#   ./e2e.sh --keep    # leave it running afterwards (for a live look)
#   ./e2e.sh --reuse   # skip down/up/install; run the checks on the running cluster
#   ./e2e.sh --runtime firecracker   # Firecracker nodes (needs /dev/kvm on the host)
#
# Each step prints PASS or FAIL; the script exits 1 on the first failure.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
KEEP=false REUSE=false
RUNTIME="${LEWBOX_DEV_RUNTIME:-docker}"
while (($#)); do
	case "$1" in
		--keep) KEEP=true ;;
		--reuse) REUSE=true KEEP=true ;;
		--runtime) RUNTIME="${2:-}"; shift ;;
		--runtime=*) RUNTIME="${1#*=}" ;;
		*) echo "unknown flag $1" >&2; exit 2 ;;
	esac
	shift
done
case "$RUNTIME" in docker | firecracker) ;; *) echo "--runtime must be docker or firecracker" >&2; exit 2 ;; esac
[[ $RUNTIME == firecracker && ! -c /dev/kvm ]] && { echo "--runtime firecracker needs /dev/kvm on this host" >&2; exit 2; }
# customers.sh and archives.sh create sandboxes with this runtime too.
export LEWBOX_DEV_RUNTIME="$RUNTIME"

dev() { "$HERE/dev.sh" "$@"; }
compose() { "$HERE/dev.sh" compose "$@"; }
lb() { local n="$1"; shift; compose exec -T -u lewbox "$n" lewbox "$@"; }
API="http://127.0.0.1:${LEWBOX_DEV_PORT:-8080}"
CLUSTER="${LEWBOX_DEV_CLUSTER:-lab}"
# The release the rolling-upgrade step moves to; it must be a published v0.7.x
# newer than the cluster's v0.7.0 (nodes download it from SB_RELEASE_URL).
UPGRADE_VERSION="${LEWBOX_DEV_UPGRADE_VERSION:-v0.7.1}"
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

	step "log in, install AerolVM ($RUNTIME) on both nodes, start a cluster on node-1, join node-2"
	dev cluster --runtime "$RUNTIME"
	pass "cluster flow finished"
fi

# --- check what Lewbox sees ------------------------------------------------
step "cluster $CLUSTER is ready with two alive nodes"
c=$(lb node-1 cluster check "$CLUSTER" --json)
expect "status ready" '.status == "ready"' "$c"
expect "2 nodes" '.nodes == 2' "$c"
expect "both alive" '[.node_list[] | select(.alive)] | length == 2' "$c"
expect "every check passed" '[.checks[] | select(.ok | not)] | length == 0' "$c"
expect "runtime $RUNTIME, AerolVM v0.7.0" ".runtime == \"$RUNTIME\" and .version == \"v0.7.0\"" "$c"

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
	compose exec -T "$n" grep -qx 'SB_CREATE_POLICY_FILE=/etc/sandboxd/lewbox-create-policy.yaml' /etc/sandboxd/lewbox.env && pass "$n policy pointed at" || fail "$n policy env"
	compose exec -T "$n" grep -qx "runtimes: \[$RUNTIME\]" /etc/sandboxd/lewbox-create-policy.yaml && pass "$n policy allows only $RUNTIME" || fail "$n policy file"
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
	http://127.0.0.1:21212/v1/tokens -d '{"name":"e2e-runner-'"$(date +%s)"'","role":"runner","team":"e2e","expires_in":"1h"}' | jq -r .token)
for i in $(seq 30); do [[ $(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $runner" http://127.0.0.1:21202/v1/whoami) == 200 ]] && break; sleep 1; done
ids=()
for i in 1 2 3; do
	s=$(curl -fsS -X POST -H "Authorization: Bearer $runner" -H 'Content-Type: application/json' http://127.0.0.1:21202/v1/sandboxes -d "{\"name\":\"e2e-$i\",\"image\":\"alpine:3.20\",\"runtime\":\"$RUNTIME\"}")
	expect "sandbox e2e-$i started" '.status == "started"' "$s"
	ids+=("$(jq -r .id <<<"$s")")
done
# running_on <node> counts the sandboxes running on a node: containers, or
# Firecracker VM processes.
running_on() {
	if [[ $RUNTIME == firecracker ]]; then compose exec -T "$1" sh -c 'pgrep -xc firecracker || true'
	else compose exec -T "$1" docker ps -q | wc -l; fi
}
on1=$(running_on node-1); on2=$(running_on node-2)
echo "placement: node-1 runs $on1, node-2 runs $on2"
(( on1 + on2 == 3 )) && pass "3 sandboxes ($RUNTIME) across the cluster" || fail "expected 3 sandboxes, got $on1 + $on2"
for id in "${ids[@]}"; do curl -fsS -o /dev/null -X DELETE -H "Authorization: Bearer $runner" "http://127.0.0.1:21201/v1/sandboxes/$id" || true; done

step "the create policy allows only $RUNTIME"
if [[ $RUNTIME == firecracker ]]; then
	denied="$HERE/.cache/policy-denied.json"
	code=$(curl -sS -o "$denied" -w '%{http_code}' -X POST -H "Authorization: Bearer $runner" -H 'Content-Type: application/json' \
		http://127.0.0.1:21202/v1/sandboxes -d '{"name":"e2e-policy-docker","image":"alpine:3.20","runtime":"docker"}')
	[[ $code == 403 ]] && grep -q create_policy_denied "$denied" && pass "a docker create is refused with 403 create_policy_denied" \
		|| { cat "$denied" >&2; echo; fail "docker create: HTTP $code"; }
	# A create that names the allowed runtime still works under the policy.
	s=$(curl -fsS -X POST -H "Authorization: Bearer $runner" -H 'Content-Type: application/json' \
		http://127.0.0.1:21202/v1/sandboxes -d '{"name":"e2e-policy-fc","image":"alpine:3.20","runtime":"firecracker"}')
	expect "a firecracker create is allowed" '.status == "started"' "$s"
	id=$(jq -r .id <<<"$s"); curl -fsS -o /dev/null -X DELETE -H "Authorization: Bearer $runner" "http://127.0.0.1:21201/v1/sandboxes/$id" || true
else
	pass "a docker cluster: the policy allows docker, as the creates above show"
fi

step "firewall enable: ufw with Lewbox's ports, the cluster ports from the private network only"
compose exec -T node-1 ufw allow 5555/tcp >/dev/null # a rule Lewbox doesn't set
if lb node-1 firewall enable >/dev/null 2>&1; then fail "other rules were handled without asking"; else pass "other rules need --reset outside a terminal"; fi
out=$(lb node-1 firewall enable --reset=false --json) || fail "firewall enable --reset=false on node-1"
expect "node-1 kept 5555/tcp" '.enabled and (.reset | not) and .kept == ["5555/tcp"]' "$out"
expect "cluster ports open to the dev network only" '[.rules[] | select(.purpose == "cluster") | .from] | unique == ["172.30.0.0/24"]' "$out"
expect "AerolVM's sandbox rules copied" '[.rules[] | select(.purpose == "sandbox-to-host")] | length > 0' "$out"
out=$(lb node-1 firewall enable --yes --json) || fail "firewall enable --yes on node-1"
expect "node-1 reset to Lewbox's rules" '.reset and .kept == []' "$out"
compose exec -T node-1 sh -c '! ufw status | grep -q 5555' && pass "5555/tcp removed by the reset" || fail "5555/tcp still open"
lb node-2 firewall enable --yes --json >/dev/null || fail "firewall enable on node-2"
for n in node-1 node-2; do compose exec -T "$n" ufw status | grep -q '^Status: active' && pass "$n: ufw active" || fail "$n: ufw not active"; done
# Enabling ufw briefly drops cluster traffic. A member AerolVM drops and
# re-adds comes back as a new node row with no reported settings, so apply
# them again before judging the cluster.
for n in node-1 node-2; do lb "$n" node configure --json >/dev/null || fail "$n: node configure after firewall"; done
c=$(lb node-1 cluster check "$CLUSTER" --json)
expect "cluster still ready behind the firewall" '.status == "ready"' "$c"
ids=()
for i in 1 2; do
	s=$(curl -fsS -X POST -H "Authorization: Bearer $runner" -H 'Content-Type: application/json' http://127.0.0.1:21202/v1/sandboxes -d "{\"name\":\"e2e-fw-$i\",\"image\":\"alpine:3.20\",\"runtime\":\"$RUNTIME\"}")
	expect "sandbox e2e-fw-$i started through node-2" '.status == "started"' "$s"
	ids+=("$(jq -r .id <<<"$s")")
done
# A sandbox with an egress allowlist goes through AerolVM's egress proxy and
# DNS on the host, which ufw's default-deny would drop without Lewbox's rules.
s=$(curl -fsS -X POST -H "Authorization: Bearer $runner" -H 'Content-Type: application/json' http://127.0.0.1:21202/v1/sandboxes \
	-d "{\"name\":\"e2e-fw-egress\",\"image\":\"alpine:3.20\",\"runtime\":\"$RUNTIME\",\"network_allow_out\":[\"example.com\"]}")
expect "egress-restricted sandbox started" '.status == "started"' "$s"
id=$(jq -r .id <<<"$s"); ids+=("$id")
# Through AerolVM's toolbox, so it works for containers and VMs alike.
fetch_in() {
	curl -fsS -X POST -H "Authorization: Bearer $runner" -H 'Content-Type: application/json' \
		"http://127.0.0.1:21202/v1/sandboxes/$id/toolbox/process/execute" \
		-d "{\"command\":\"timeout 20 wget -q -O /dev/null $1\"}" | jq -r .exit_code
}
[[ $(fetch_in http://example.com) == 0 ]] && pass "allowed host reachable behind the firewall" || fail "egress to an allowed host blocked"
code=$(fetch_in http://google.com)
[[ -n $code && $code != 0 && $code != null ]] && pass "other hosts still blocked" || fail "egress to a host not allowed got through (exit $code)"
for id in "${ids[@]}"; do curl -fsS -o /dev/null -X DELETE -H "Authorization: Bearer $runner" "http://127.0.0.1:21201/v1/sandboxes/$id" || true; done

step "the customer journey (customers.sh)"
"$HERE/customers.sh" || fail "customer journey"

step "storage profiles and archived sandboxes (archives.sh)"
"$HERE/archives.sh" || fail "archives"

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

step "a rolling AerolVM upgrade keeps a running sandbox, Lewbox's files and identity, then rolls back"
# One sandbox stays running across both node restarts.
s=$(curl -fsS -X POST -H "Authorization: Bearer $runner" -H 'Content-Type: application/json' \
	http://127.0.0.1:21202/v1/sandboxes -d "{\"name\":\"e2e-upgrade\",\"image\":\"alpine:3.20\",\"runtime\":\"$RUNTIME\"}")
expect "sandbox e2e-upgrade started before the upgrade" '.status == "started"' "$s"
up_id=$(jq -r .id <<<"$s")

# --check shows the plan and starts nothing.
out=$(lb node-1 cluster upgrade "$CLUSTER" --version "$UPGRADE_VERSION" --check --json) || fail "cluster upgrade --check"
expect "the plan is allowed and untested" '.allowed and (.tested | not) and .target_version == "'"$UPGRADE_VERSION"'"' "$out"
expect "the plan has both nodes" '.nodes | length == 2' "$out"
if lb node-1 cluster upgrade status "$CLUSTER" --json >/dev/null 2>&1; then fail "--check created a rollout"; else pass "--check changed nothing"; fi

# Two mixed nodes are two voters, so the API is down while each restarts.
out=$(lb node-1 cluster upgrade "$CLUSTER" --version "$UPGRADE_VERSION" --allow-downtime --hold --yes --json) \
	|| fail "cluster upgrade"
expect "the rollout started" '(.phase == "staging" or .phase == "running") and .target_version == "'"$UPGRADE_VERSION"'"' "$out"
phase() { lb node-1 cluster upgrade status "$CLUSTER" --json 2>/dev/null | jq -r '.phase' || true; }
wait_phase() {
	local want="$1"
	for i in $(seq 300); do [[ $(phase) == "$want" ]] && return 0; sleep 2; done
	echo "last status: $(lb node-1 cluster upgrade status "$CLUSTER" --json 2>&1 || true)" >&2
	return 1
}
wait_phase done || fail "the rollout did not reach done"
pass "both nodes moved to $UPGRADE_VERSION (held at done)"

c=$(lb node-1 cluster check "$CLUSTER" --json)
expect "cluster check passes on the new version" ".status == \"ready\" and .version == \"$UPGRADE_VERSION\"" "$c"
expect "both nodes report $UPGRADE_VERSION" "[.node_list[] | select(.version == \"$UPGRADE_VERSION\")] | length == 2" "$c"
curl -fsS -H "Authorization: Bearer $runner" "http://127.0.0.1:21202/v1/sandboxes/$up_id" | jq -e '.status == "started"' >/dev/null \
	&& pass "sandbox survived the upgrade" || fail "sandbox did not survive the upgrade"
for n in node-1 node-2; do
	compose exec -T "$n" grep -qx SB_IDENTITY_ENABLED=true /etc/sandboxd/lewbox.env && pass "$n identity still on" || fail "$n identity after upgrade"
	compose exec -T "$n" grep -Fqx "runtimes: [$RUNTIME]" /etc/sandboxd/lewbox-create-policy.yaml && pass "$n create policy still in place" || fail "$n create policy after upgrade"
	compose exec -T "$n" test -f /etc/systemd/system/sandboxd.service.d/lewbox.conf && pass "$n drop-in still in place" || fail "$n drop-in after upgrade"
done
# A token write needs the leader. Try each node's own PAT until one answers.
created=""
for n in node-1 node-2; do
	pat=$(compose exec -T "$n" sh -c "grep ^SB_PAT_TOKEN= /etc/sandboxd/sandboxd.env | cut -d= -f2")
	for i in $(seq 30); do
		code=$(compose exec -T "$n" curl -s -o /dev/null -w '%{http_code}' -X POST \
			-H "Authorization: Bearer $pat" -H 'Content-Type: application/json' \
			http://127.0.0.1:21212/v1/tokens \
			-d '{"name":"e2e-after-upgrade","role":"admin","team":"*","expires_in":"1d"}')
		[[ $code == 201 ]] && { created="$n"; break; }
		sleep 2
	done
	[[ -n $created ]] && break
done
[[ -n $created ]] && pass "a token write succeeds after the upgrade ($created is leader)" || fail "no token write succeeded after the upgrade"
# AerolVM v0.7.1 re-renders node.json with ingress_proxy_routing "auto", and
# its host apply then passes "auto" to install.sh as a value, which v0.7.1
# refuses. A later upgrade or rollback on an IP-mode node would fail there.
# Pin the (already off) setting to false for the rest of the run.
for n in node-1 node-2; do
	compose exec -T "$n" sed -i 's/"ingress_proxy_routing": "auto"/"ingress_proxy_routing": false/' /etc/sandboxd/node.json
done

out=$(lb node-1 cluster upgrade rollback "$CLUSTER" --yes --json) || fail "rollback after a held upgrade"
expect "rollback was accepted" '.direction == "rollback"' "$out"
wait_phase rolled_back || fail "the rollout did not reach rolled_back"
c=$(lb node-1 cluster check "$CLUSTER" --json)
expect "the cluster is back on v0.7.0" '.status == "ready" and .version == "v0.7.0"' "$c"
curl -fsS -H "Authorization: Bearer $runner" "http://127.0.0.1:21202/v1/sandboxes/$up_id" | jq -e '.status == "started"' >/dev/null \
	&& pass "sandbox survived the rollback" || fail "sandbox did not survive the rollback"
# The rollback's install.sh derived "auto" again; pin it for the next upgrade.
for n in node-1 node-2; do
	compose exec -T "$n" sed -i 's/"ingress_proxy_routing": "auto"/"ingress_proxy_routing": false/' /etc/sandboxd/node.json
done

# Start once more and stop it: the nodes in progress finish, no more start.
out=$(lb node-1 cluster upgrade "$CLUSTER" --version "$UPGRADE_VERSION" --allow-downtime --yes --json) || fail "second cluster upgrade"
expect "the second rollout started" '(.phase == "staging" or .phase == "running")' "$out"
lb node-1 cluster upgrade stop "$CLUSTER" --json >/dev/null || fail "cluster upgrade stop"
wait_phase stopped || fail "the rollout did not reach stopped"
pass "stop left the rollout stopped"
# Restore v0.7.0 for the rest of the run (a stop may have restarted one node).
lb node-1 cluster upgrade rollback "$CLUSTER" --yes --json >/dev/null || fail "rollback after stop"
wait_phase rolled_back || fail "the stopped rollout did not roll back"
c=$(lb node-1 cluster check "$CLUSTER" --json)
expect "cluster is ready on v0.7.0 after the stop test" '.status == "ready" and .version == "v0.7.0"' "$c"
curl -fsS -o /dev/null -X DELETE -H "Authorization: Bearer $runner" "http://127.0.0.1:21201/v1/sandboxes/$up_id" || true

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
