#!/usr/bin/env bash
# Storage profiles and archived sandboxes, end to end, against a running dev
# cluster (the storage service is RustFS at 172.30.0.20):
#
#   ./dev.sh up && ./dev.sh cluster && ./archives.sh
#
# A storage profile on cluster "lab" with stop policy archive, a 1d local
# retention and named snapshots, node configure on every node (registry, env,
# cleanup timer), outdated nodes, then a customer's stopped sandbox archived
# to the bucket as archive-<name>-<id>, its local copy kept under 1d and
# removed under instant, restored on any node with its files, and the archive
# deleted from the bucket. e2e.sh runs it too. It exits 1 on the first failure.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
API="http://127.0.0.1:${LEWBOX_DEV_PORT:-8080}"
CLUSTER="${LEWBOX_DEV_CLUSTER:-lab}"
NODES=(${LEWBOX_DEV_NODES:-node-1 node-2})
RUNTIME="${LEWBOX_DEV_RUNTIME:-docker}"   # the cluster's: docker or firecracker
RUN="$(date +%s)"
S3=http://172.30.0.20:9000
S3_KEY=lewbox
S3_SECRET=lewbox-dev-secret
BUCKET=lewbox

STEP=0
pass() { printf '\033[32mPASS\033[0m %s\n' "$*"; }
fail() { printf '\033[31mFAIL\033[0m %s\n' "$*"; exit 1; }
step() { STEP=$((STEP + 1)); printf '\n\033[1m[archives %d] %s\033[0m\n' "$STEP" "$*"; }
expect() { if jq -e "$2" >/dev/null <<<"$3"; then pass "$1"; else echo "$3" | jq . >&2 || echo "$3" >&2; fail "$1"; fi; }
eventually() {
	local secs="$1" what="$2"; shift 2
	for _ in $(seq "$secs"); do "$@" >/dev/null 2>&1 && { pass "$what"; return; }; sleep 1; done
	"$@" || true
	fail "$what (after ${secs}s)"
}

dc() { docker compose -p lewbox-dev "$@"; }
on() { local n="$1"; shift; dc exec -T "$n" "$@"; }
lb() { local n="$1"; shift; dc exec -T -u lewbox "$n" lewbox "$@"; }
s3() { on node-1 curl -sS --aws-sigv4 "aws:amz:us-east-1:s3" --user "$S3_KEY:$S3_SECRET" "$@"; }
bucket_keys() { s3 "$S3/$BUCKET?list-type=2&max-keys=1000&prefix=$1" | grep -o '<Key>[^<]*</Key>' | sed 's/<[^>]*>//g' || true; }

tok=$(curl -fsS -X POST "$API/v0/auth/login" -H 'Content-Type: application/json' \
	-d "{\"email\":\"${LEWBOX_DEV_EMAIL:-admin@lewbox.dev}\",\"password\":\"${LEWBOX_DEV_PASSWORD:-dev-password-123}\"}" | jq -r .token)
admin() { local m="$1" p="$2"; shift 2; curl -fsS -X "$m" -H "Authorization: Bearer $tok" -H 'Content-Type: application/json' "$API$p" "$@"; }
JAR="$(mktemp -d)"
trap 'rm -rf "$JAR"' EXIT
cust() { local m="$1" p="$2"; shift 2; curl -fsS -X "$m" -b "$JAR/c" -c "$JAR/c" -H 'Content-Type: application/json' -H "Lewbox-Organization: $ORG" "$API$p" "$@"; }

step "a storage profile on the dev bucket"
s3 -X PUT "$S3/$BUCKET" -o /dev/null -w '%{http_code}' | grep -qE '^(200|409)$' && pass "bucket $BUCKET exists" || fail "create bucket"
p=$(admin POST /v0/storage-profiles -d "{\"name\":\"dev-s3-$RUN\",\"endpoint\":\"$S3\",\"bucket\":\"$BUCKET\",\"access_key_id\":\"$S3_KEY\",\"secret_access_key\":\"$S3_SECRET\"}")
expect "profile saved after a passing test, secret hidden" '.tested_at != null and .test_error == "" and .path_style == true and (tostring | contains("lewbox-dev-secret") | not)' "$p"
PROFILE="dev-s3-$RUN"
bad=$(curl -s -X POST -H "Authorization: Bearer $tok" -H 'Content-Type: application/json' "$API/v0/storage-profiles" \
	-d "{\"name\":\"bad-$RUN\",\"endpoint\":\"$S3\",\"bucket\":\"$BUCKET\",\"access_key_id\":\"$S3_KEY\",\"secret_access_key\":\"wrong\"}")
expect "a wrong secret is refused at the write step" '.error.details.step == "write"' "$bad"

step "the cluster archives stopped sandboxes; its nodes are outdated until configured"
c=$(admin PATCH "/v0/clusters/$CLUSTER" -d "{\"storage_profile\":\"$PROFILE\",\"stop_policy\":\"archive\",\"archive_local_retention\":\"1d\",\"archive_snapshot_name\":\"name\"}")
expect "cluster set" ".storage_profile == \"$PROFILE\" and .stop_policy == \"archive\" and .archive_local_retention == \"1d\" and .archive_snapshot_name == \"name\"" "$c"
c=$(admin POST "/v0/clusters/$CLUSTER/check")
expect "every node is outdated" '[.node_list[] | .settings.outdated] | all' "$c"
expect "the node settings check says so" '[.checks[] | select(.name == "node settings" and (.ok | not))] | length == 1' "$c"

step "node configure on every node"
for n in "${NODES[@]}"; do
	out=$(lb "$n" node configure --json 2>"$JAR/err") || { cat "$JAR/err" >&2; fail "$n: node configure"; }
	expect "$n: registry and timer installed, env changed" '.storage == "applied" and .registry_changed and .env_changed and .cleanup_timer' "$out"
	if grep -q "$S3_SECRET" "$JAR/err"; then fail "$n: the bucket secret was printed"; fi
	on "$n" systemctl is-active --quiet lewbox-registry && pass "$n: lewbox-registry is active" || fail "$n: lewbox-registry"
	on "$n" curl -fsS -o /dev/null http://127.0.0.1:5000/v2/ && pass "$n: the registry answers on loopback" || fail "$n: registry /v2/"
	on "$n" grep -q '^SB_SNAPSHOT_PUSH_ENABLED=true$' /etc/sandboxd/lewbox.env && pass "$n: AerolVM pushes snapshots" || fail "$n: push env"
	on "$n" systemctl is-active --quiet lewbox-archive-cleanup.timer && pass "$n: cleanup timer active" || fail "$n: cleanup timer"
	[[ $(on "$n" stat -c '%U %a' /usr/local/lib/lewbox/lewbox) == "root 755" ]] && pass "$n: the timer runs a root-owned copy" || fail "$n: cleanup binary owner"
	on "$n" grep -q -- '--older-than 24h0m0s$' /etc/systemd/system/lewbox-archive-cleanup.service && pass "$n: cleanup keeps copies for 1d" || fail "$n: cleanup --older-than"
	again=$(lb "$n" node configure --json 2>/dev/null)
	expect "$n: a second run changes nothing" '(.registry_changed | not) and (.env_changed | not) and (.cleanup_timer | not)' "$again"
done
c=$(admin POST "/v0/clusters/$CLUSTER/check")
expect "no node is outdated now" '[.node_list[] | .settings.outdated] | any | not' "$c"
expect "the node settings check passes" '[.checks[] | select(.name == "node settings" and .ok)] | length == 1' "$c"

step "a customer's stopped sandbox is archived to the bucket"
plan="arch-$RUN"
admin POST /v0/plans -d "{\"slug\":\"$plan\",\"name\":\"Archive\",\"max_sandboxes\":3}" >/dev/null
email="arch-$RUN@e2e.dev"
admin POST /v0/users -d "{\"email\":\"$email\",\"password\":\"customer-password-123\"}" >/dev/null
ORG="arch-$RUN"
admin POST /v0/organizations -d "{\"slug\":\"$ORG\",\"name\":\"archive workspace\",\"owner\":\"$email\"}" >/dev/null
admin PUT "/v0/subscriptions/$CLUSTER" -H "Lewbox-Organization: $ORG" -d "{\"plan\":\"$plan\"}" >/dev/null
cust POST /v0/auth/session -d "{\"email\":\"$email\",\"password\":\"customer-password-123\"}" >/dev/null
expect "the region says stopped sandboxes are archived" "[.data[] | select(.slug == \"$CLUSTER\" and .stop_policy == \"archive\")] | length == 1" "$(cust GET /v0/regions)"
k=$(cust POST /v0/sandbox-keys -d "{\"name\":\"arch\",\"region\":\"$CLUSTER\"}")
KEY=$(jq -r .token <<<"$k"); EP=$(jq -r .env.SB_API_URL <<<"$k")
avm() { local m="$1" p="$2"; shift 2; curl -sS -X "$m" -H "Authorization: Bearer $KEY" -H 'Content-Type: application/json' "$EP$p" "$@"; }
whoami_ok() { test "$(avm GET /v1/whoami -o /dev/null -w '%{http_code}')" = 200; }
eventually 30 "the key works on the cluster" whoami_ok
sb=$(avm POST /v1/sandboxes -d "{\"image\":\"alpine:3.20\",\"runtime\":\"$RUNTIME\",\"cpu\":1,\"memory_mb\":256,\"name\":\"keeper\",\"tags\":{\"app\":\"e2e\"}}" | jq -r .id)
[[ $sb == sb-* ]] && pass "sandbox $sb created" || fail "create sandbox"
out=$(avm POST "/v1/sandboxes/$sb/toolbox/process/execute" -d "$(jq -nc --arg c "sh -c 'echo archived-$RUN > /root/marker'" '{command: $c}')")
expect "a file written in it" '.exit_code == 0' "$out"
avm POST "/v1/sandboxes/$sb/stop" -o /dev/null
archived() { cust GET /v0/archives | jq -e "[.data[] | select(.sandbox_id == \"$sb\" and .status == \"archived\")] | length == 1"; }
eventually 180 "it is archived" archived
a=$(cust GET /v0/archives | jq -c ".data[] | select(.sandbox_id == \"$sb\")")
expect "the archive keeps its name, tags and registry ref" ".sandbox_name == \"keeper\" and .tags.app == \"e2e\" and (.registry_ref | startswith(\"127.0.0.1:5000/cluster/\"))" "$a"
expect "its snapshot is named archive-<name>-<id>" ".snapshot_name == \"archive-keeper-$sb\"" "$a"
ARCHIVE=$(jq -r .id <<<"$a"); REF=$(jq -r .registry_ref <<<"$a")
[[ $(avm GET "/v1/sandboxes/$sb" -o /dev/null -w '%{http_code}') == 404 ]] && pass "the sandbox is gone from the cluster" || fail "sandbox still on the cluster"
expect "it isn't listed as an ordinary snapshot" "[.data[] | select(.name == \"archive-keeper-$sb\")] | length == 0" "$(cust GET /v0/snapshots)"
CID=$(admin GET "/v0/clusters/$CLUSTER" | jq -r .id)
REPO="clusters/$CID/docker/registry/v2/repositories/${REF#127.0.0.1:5000/}"; REPO="${REPO%:*}/"
[[ -n $(bucket_keys "$REPO") ]] && pass "its repository is in the bucket" || fail "no repository under $REPO"

step "the cleanup timer keeps the local copy under 1d, and frees it under instant"
local_copies() { local total=0 n c; for n in "${NODES[@]}"; do c=$(on "$n" docker images --format '{{.Repository}}' | grep -c '^aerolvm-snapshot/' || true); total=$((total + c)); done; echo "$total"; }
for n in "${NODES[@]}"; do on "$n" systemctl start lewbox-archive-cleanup.service; done
[[ $(local_copies) -ge 1 ]] && pass "a fresh copy is kept under 1d" || fail "the 1d cleanup removed a fresh copy"
c=$(admin PATCH "/v0/clusters/$CLUSTER" -d '{"archive_local_retention":"instant"}')
c=$(admin POST "/v0/clusters/$CLUSTER/check")
expect "every node is outdated after the retention change" '[.node_list[] | .settings.outdated] | all' "$c"
for n in "${NODES[@]}"; do
	out=$(lb "$n" node configure --json 2>/dev/null) || fail "$n: node configure instant"
	expect "$n: only the cleanup unit changed" '(.registry_changed | not) and .cleanup_timer' "$out"
	on "$n" grep -q 'node cleanup --json$' /etc/systemd/system/lewbox-archive-cleanup.service && pass "$n: cleanup removes copies at once" || fail "$n: instant unit"
done
# A pass the timer started before node configure keeps the old flags, and
# systemctl start joins it instead of starting another: wait it out first.
cleanup_idle() { ! on "$1" systemctl is-active --quiet lewbox-archive-cleanup.service; }
for n in "${NODES[@]}"; do
	for _ in $(seq 60); do cleanup_idle "$n" && break; sleep 1; done
	on "$n" systemctl start lewbox-archive-cleanup.service
done
for n in "${NODES[@]}"; do
	left=$(on "$n" docker images --format '{{.Repository}}' | grep -c '^aerolvm-snapshot/' || true)
	if [[ $left != 0 ]]; then
		on "$n" docker images --format '{{.Repository}}:{{.Tag}} {{.CreatedAt}}' | grep '^aerolvm-snapshot/' >&2 || true
		on "$n" journalctl -u lewbox-archive-cleanup.service -n 30 --no-pager -o short-iso >&2 || true
		on "$n" cat /etc/systemd/system/lewbox-archive-cleanup.service >&2 || true
		# What the node's AerolVM still knows about, and what the archiver did:
		# an image no snapshot record names was orphaned, not missed.
		echo "$n snapshots:" >&2
		on "$n" sh -c '. /etc/sandboxd/sandboxd.env; curl -sS -H "Authorization: Bearer $SB_PAT_TOKEN" "http://127.0.0.1:21212/v1/snapshots?limit=500"' |
			jq -c '.snapshots[] | {name, image, push_state, image_registry_ref, created_at}' >&2 || true
		dc logs orchestrator 2>&1 | grep -E '"archive (state changed|step failed)"' | cut -c1-400 >&2 || true
		fail "$n: $left local snapshot image(s) left"
	fi
	pass "$n: no local snapshot image left"
done

step "restore it: a new sandbox, from the bucket, with its files"
r=$(cust POST "/v0/archives/$ARCHIVE/restore")
expect "restored as a new sandbox" ".sandbox.id != \"$sb\" and .sandbox.name == \"keeper\" and .archive.last_restored_sandbox_id == .sandbox.id" "$r"
nsb=$(jq -r .sandbox.id <<<"$r")
info=$(avm GET "/v1/sandboxes/$nsb")
expect "it carries the old tags and restored_from" ".tags.app == \"e2e\" and .tags[\"lewbox.restored_from\"] == \"$sb\"" "$info"
started() { test "$(avm GET "/v1/sandboxes/$nsb" | jq -r .status)" = started; }
eventually 60 "it is running" started
out=$(avm POST "/v1/sandboxes/$nsb/toolbox/process/execute" -d '{"command":"cat /root/marker"}')
expect "the file came back" ".stdout | contains(\"archived-$RUN\")" "$out"

step "delete the archive: gone from AerolVM and the bucket"
avm DELETE "/v1/sandboxes/$nsb" -o /dev/null
d=$(cust DELETE "/v0/archives/$ARCHIVE")
expect "deleted" '.status == "deleted"' "$d"
expect "no longer listed" "[.data[] | select(.id == \"$ARCHIVE\")] | length == 0" "$(cust GET /v0/archives)"
[[ -z $(bucket_keys "$REPO") ]] && pass "its repository is gone from the bucket" || fail "repository left under $REPO"

step "back to keeping stopped sandboxes"
c=$(admin PATCH "/v0/clusters/$CLUSTER" -d '{"stop_policy":"keep","storage_profile":"none"}')
expect "cluster set back" '.stop_policy == "keep" and .storage_profile == null' "$c"
for n in "${NODES[@]}"; do
	out=$(lb "$n" node configure --json 2>/dev/null) || fail "$n: node configure without storage"
	expect "$n: registry and timer removed" '.storage == "removed"' "$out"
	on "$n" test ! -e /etc/systemd/system/lewbox-registry.service && pass "$n: no registry unit" || fail "$n: registry unit left"
done
echo
pass "archives: all steps"
