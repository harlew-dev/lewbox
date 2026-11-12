#!/usr/bin/env bash
# The customer journey, end to end, against a running dev cluster:
#
#   ./dev.sh up && ./dev.sh cluster && ./customers.sh
#
# Or against a real instance (see plans/RELEASING.md). It changes settings
# while it runs and puts them back on exit; it leaves three test customers and
# an e2e-<time> plan behind:
#
#   LEWBOX_URL=https://lewbox.example.com LEWBOX_REGION=eu-west \
#   LEWBOX_ADMIN_EMAIL=you@example.com LEWBOX_ADMIN_PASSWORD=… ./customers.sh
#
# Sign-up, a shared workspace plan, a sandbox key per member, sandboxes run
# with those keys straight on the cluster, Lewbox's mirror and isolation, each
# member's share enforced by AerolVM, stop and delete from Lewbox, usage,
# monitoring, suspension and revocation.
# e2e.sh runs it too. Each step prints PASS or FAIL; it exits 1 on a failure.
set -euo pipefail

API="${LEWBOX_URL:-http://127.0.0.1:${LEWBOX_DEV_PORT:-8080}}"
CLUSTER="${LEWBOX_REGION:-${LEWBOX_DEV_CLUSTER:-lab}}"
RUN="$(date +%s)"
JAR="$(mktemp -d)"

STEP=0
pass() { printf '\033[32mPASS\033[0m %s\n' "$*"; }
fail() { printf '\033[31mFAIL\033[0m %s\n' "$*"; exit 1; }
step() { STEP=$((STEP + 1)); printf '\n\033[1m[customers %d] %s\033[0m\n' "$STEP" "$*"; }
expect() { if jq -e "$2" >/dev/null <<<"$3"; then pass "$1"; else echo "$3" | jq . >&2 || echo "$3" >&2; fail "$1"; fi; }
# eventually <seconds> <description> <command…>: retry the command once a second.
eventually() {
	local secs="$1" what="$2"; shift 2
	for _ in $(seq "$secs"); do "$@" >/dev/null 2>&1 && { pass "$what"; return; }; sleep 1; done
	"$@" || true
	fail "$what (after ${secs}s)"
}

# The operator, with a bearer token.
tok=$(curl -fsS -X POST "$API/v0/auth/login" -H 'Content-Type: application/json' \
	-d "$(jq -n --arg e "${LEWBOX_ADMIN_EMAIL:-${LEWBOX_DEV_EMAIL:-admin@lewbox.dev}}" --arg p "${LEWBOX_ADMIN_PASSWORD:-${LEWBOX_DEV_PASSWORD:-dev-password-123}}" '{email: $e, password: $p}')" | jq -r .token)
admin() { local m="$1" p="$2"; shift 2; curl -fsS -X "$m" -H "Authorization: Bearer $tok" -H 'Content-Type: application/json' "$API$p" "$@"; }

# Put the operator's settings back however the run ends.
ORIGINAL=$(admin GET /v0/settings | jq -c '{signup, default_plan, default_regions}')
trap 'admin PATCH /v0/settings -d "$ORIGINAL" >/dev/null || echo "could not restore settings: $ORIGINAL" >&2; rm -rf "$JAR"' EXIT

# A customer: a browser session cookie, and their organization in the header.
declare -A ORG KEY
cust() { local who="$1" m="$2" p="$3"; shift 3; curl -fsS -X "$m" -b "$JAR/$who" -c "$JAR/$who" -H 'Content-Type: application/json' -H "Lewbox-Organization: ${ORG[$who]:-}" "$API$p" "$@"; }
# cust_in acts in another organization the account belongs to.
cust_in() { local who="$1" org="$2" m="$3" p="$4"; shift 4; curl -fsS -X "$m" -b "$JAR/$who" -c "$JAR/$who" -H 'Content-Type: application/json' -H "Lewbox-Organization: $org" "$API$p" "$@"; }
code() { curl -s -o /dev/null -w '%{http_code}' "$@"; }

# The cluster's own API with a customer's sandbox key. EP is the region's endpoint.
EP=""
avm() { local who="$1" m="$2" p="$3"; shift 3; curl -sS -X "$m" -H "Authorization: Bearer ${KEY[$who]}" -H 'Content-Type: application/json' "$EP$p" "$@"; }
SANDBOX='{"image":"alpine:3.20","cpu":1,"memory_mb":512}'
create_code() { avm "$1" POST /v1/sandboxes -d "$SANDBOX" -o "$JAR/created" -w '%{http_code}'; }
status_of() { avm "$1" GET "/v1/sandboxes/$2" | jq -r .status; }
# Customer GETs as plain functions, so `eventually` can call them.
sandboxes_of() { cust "$1" GET /v0/sandboxes; }
sandboxes_in() { cust_in "$1" "$2" GET /v0/sandboxes; }
limits_of() { cust "$1" GET /v0/limits; }
usage_of() { cust "$1" GET /v0/usage; }
org_of() { admin GET "/v0/organizations/$1"; }

has_sandbox() { sandboxes_of "$1" | jq -e "[.data[] | select(.id == \"$2\" and .region == \"$CLUSTER\")] | length == 1"; }
has_sandbox_in() { sandboxes_in "$1" "$2" | jq -e "[.data[] | select(.id == \"$3\" and .region == \"$CLUSTER\")] | length == 1"; }
no_sandbox_in() { sandboxes_in "$1" "$2" | jq -e "[.data[] | select(.id == \"$3\")] | length == 0"; }
member_share() { limits_of "$1" | jq -e --arg e "$2" --arg c "$CLUSTER" '[.regions[] | select(.slug == $c) | .members[] | select(.email == $e) | .share.sandboxes] == [1]'; }
has_usage() { usage_of "$1" | jq -e '.totals.sandbox_hours > 0 and .totals.vcpu_hours > 0'; }
org_is() { org_of "$1" | jq -e "$2"; }
is_status() { test "$(status_of "$1" "$2")" = "$3"; }
create_is() { test "$(create_code "$1")" = "$2"; }
create_ok() { create_code "$1" >/dev/null; jq -e '.status == "started"' "$JAR/created" >/dev/null; }
whoami_is() { test "$(avm "$1" GET /v1/whoami -o /dev/null -w '%{http_code}')" = "$2"; }

step "the operator opens sign-up on a plan of 2 sandboxes"
plan="e2e-$RUN"
p=$(admin POST /v0/plans -d "{\"slug\":\"$plan\",\"name\":\"E2E\",\"max_sandboxes\":2,\"max_vcpus\":4,\"max_memory_mb\":4096}")
expect "plan created" ".slug == \"$plan\" and .max_sandboxes == 2" "$p"
s=$(admin PATCH /v0/settings -d "{\"signup\":\"open\",\"default_plan\":\"$plan\",\"default_regions\":[\"$CLUSTER\"]}")
expect "sign-up open, plan and region are the defaults" ".signup == \"open\" and .default_plan == \"$plan\" and .default_regions == [\"$CLUSTER\"]" "$s"
expect "the public instance info says so" '.signup == "open"' "$(curl -fsS "$API/v0/instance")"

step "two customers sign up and get workspaces of their own on that plan"
for who in a b; do
	r=$(cust "$who" POST /v0/auth/signup -d "{\"email\":\"$who-$RUN@e2e.dev\",\"password\":\"customer-password-123\"}")
	expect "$who signed up" ".user.email == \"$who-$RUN@e2e.dev\" and (.user.instance_admin | not)" "$r"
	me=$(cust "$who" GET /v0/me)
	ORG[$who]=$(jq -r '.organizations[0].slug' <<<"$me")
	expect "$who owns an active workspace" '(.organizations | length == 1) and .organizations[0].role == "owner" and .organizations[0].status == "active"' "$me"
	o=$(cust "$who" GET "/v0/organizations/${ORG[$who]}")
	expect "$who is subscribed to $plan in $CLUSTER" "(.regions | length == 1) and .regions[0].slug == \"$CLUSTER\" and .regions[0].plan.slug == \"$plan\"" "$o"
done
[[ $(code -b "$JAR/a" "$API/v0/clusters") == 403 ]] && pass "a customer can't list clusters" || fail "clusters visible to a customer"
expect "a customer sees its subscribed region with the plan" "[.data[] | select(.slug == \"$CLUSTER\" and .plan.slug == \"$plan\")] | length == 1" "$(cust a GET /v0/regions)"

step "a adds b to its workspace, so the 2-sandbox plan splits one each"
m=$(cust a POST /v0/members -d "{\"email\":\"b-$RUN@e2e.dev\",\"role\":\"member\"}")
expect "b is a member of a's workspace" ".email == \"b-$RUN@e2e.dev\" and .role == \"member\"" "$m"
o=$(cust a GET /v0/limits)
expect "a and b each have a share of 1 sandbox" "[.regions[] | select(.slug == \"$CLUSTER\") | .members[] | select(.email == \"a-$RUN@e2e.dev\" or .email == \"b-$RUN@e2e.dev\") | .share.sandboxes] == [1, 1]" "$o"

step "each member creates a sandbox key in the workspace"
k=$(cust a POST /v0/sandbox-keys -d "{\"name\":\"a\",\"region\":\"$CLUSTER\"}")
expect "a's key is an avm_ token with SDK settings" '(.token | startswith("avm_")) and .env.SB_API_TOKEN == .token and (.e2b_env.E2B_API_URL | endswith("/e2b"))' "$k"
KEY[a]=$(jq -r .token <<<"$k")
EP=$(jq -r .env.SB_API_URL <<<"$k")
expect "a's key names a" '.member.email == "a-'"$RUN"'@e2e.dev"' "$k"
k=$(cust_in b "${ORG[a]}" POST /v0/sandbox-keys -d "{\"name\":\"b\",\"region\":\"$CLUSTER\"}")
expect "b's key belongs to b" '.member.email == "b-'"$RUN"'@e2e.dev"' "$k"
KEY[b]=$(jq -r .token <<<"$k")
expect "the key list never shows a token" '[.. | strings | select(startswith("avm_") and length > 20)] | length == 0' "$(cust a GET /v0/sandbox-keys)"
eventually 30 "a's key works on the cluster" whoami_is a 200
eventually 30 "b's key works on the cluster" whoami_is b 200

step "a customer gets no region until the operator assigns one"
admin PATCH /v0/settings -d '{"default_regions":[]}' >/dev/null
r=$(cust c POST /v0/auth/signup -d "{\"email\":\"c-$RUN@e2e.dev\",\"password\":\"customer-password-123\"}")
expect "c signed up" ".user.email == \"c-$RUN@e2e.dev\"" "$r"
ORG[c]=$(cust c GET /v0/me | jq -r '.organizations[0].slug')
expect "c sees no regions" '.data == []' "$(cust c GET /v0/regions)"
c_code=$(code -X POST -b "$JAR/c" -H "Lewbox-Organization: ${ORG[c]}" -H 'Content-Type: application/json' \
	"$API/v0/sandbox-keys" -d "{\"name\":\"e2e\",\"region\":\"$CLUSTER\"}")
[[ $c_code == 412 ]] && pass "c's key is refused with 412" || fail "c's key got $c_code, want 412"
sub=$(admin PUT "/v0/subscriptions/$CLUSTER" -H "Lewbox-Organization: ${ORG[c]}" -d "{\"plan\":\"$plan\"}")
expect "the operator subscribed c in $CLUSTER" ".region.slug == \"$CLUSTER\" and .plan.slug == \"$plan\" and .limits.sandboxes == 2" "$sub"
expect "c sees the region now" "[.data[] | select(.slug == \"$CLUSTER\" and .plan.slug == \"$plan\")] | length == 1" "$(cust c GET /v0/regions)"
k=$(cust c POST /v0/sandbox-keys -d "{\"name\":\"e2e\",\"region\":\"$CLUSTER\"}")
KEY[c]=$(jq -r .token <<<"$k")
eventually 30 "c's key works after the subscription" whoami_is c 200

step "a and b each run one sandbox with their own key, straight on the cluster"
create_ok a && pass "a's sandbox started" || { cat "$JAR/created" >&2; fail "a's create"; }
id1=$(jq -r .id "$JAR/created")
out=$(avm a POST "/v1/sandboxes/$id1/toolbox/process/execute" -d '{"command":"echo hello-from-sandbox"}')
grep -q 'hello-from-sandbox' <<<"$out" && pass "a command ran in it" || { echo "$out" >&2; fail "exec"; }
create_ok b && pass "b's sandbox started" || { cat "$JAR/created" >&2; fail "b's create"; }
id2=$(jq -r .id "$JAR/created")

step "Lewbox shows both to a, and only its own to each member"
eventually 60 "a's sandbox list has a's sandbox" has_sandbox a "$id1"
eventually 60 "a's workspace list has b's sandbox too" has_sandbox_in a "${ORG[a]}" "$id2"
eventually 60 "b's list has only b's sandbox" has_sandbox_in b "${ORG[a]}" "$id2"
no_sandbox_in b "${ORG[a]}" "$id1" && pass "b's list doesn't have a's sandbox" || fail "b saw a's sandbox"
[[ $(avm b GET "/v1/sandboxes/$id1" -o /dev/null -w '%{http_code}') == 404 ]] && pass "b's key gets 404 for a's sandbox" || fail "b reached a's sandbox"
[[ $(code -X POST -b "$JAR/b" -H "Lewbox-Organization: ${ORG[a]}" "$API/v0/sandboxes/$id1/stop?region=$CLUSTER") == 404 ]] && pass "b can't stop a's sandbox through Lewbox" || fail "b stopped a's sandbox"
step "each member's share (1 sandbox) is enforced by AerolVM"
c3=$(create_code a)
[[ $c3 == 429 ]] && pass "a's second sandbox is refused with 429" || { cat "$JAR/created" >&2; fail "a's second sandbox got $c3, want 429"; }
c4=$(create_code b)
[[ $c4 == 429 ]] && pass "b's second sandbox is refused with 429" || { cat "$JAR/created" >&2; fail "b's second sandbox got $c4, want 429"; }
eventually 30 "a sees both members' shares" member_share a "b-$RUN@e2e.dev"

step "a stops and deletes its sandbox through Lewbox"
expect "stopped" '.status == "stopped"' "$(cust a POST "/v0/sandboxes/$id1/stop?region=$CLUSTER")"
cust a DELETE "/v0/sandboxes/$id1?region=$CLUSTER" >/dev/null && pass "deleted"
[[ $(avm a GET "/v1/sandboxes/$id1" -o /dev/null -w '%{http_code}') == 404 ]] && pass "gone on the cluster" || fail "still on the cluster"

step "usage is recorded and the operator sees the fleet"
eventually 60 "a has usage" has_usage a
expect "overview counts customers and running sandboxes" '.customers.total >= 2 and .sandboxes.running >= 1' "$(admin GET /v0/overview)"
# The first health sample comes with the first check after the orchestrator starts.
has_health() { admin GET "/v0/clusters/$CLUSTER/health?days=30" | jq -e '(.cluster.days | length == 30) and ([.cluster.days[] | select(.uptime != null)] | length >= 1) and (.nodes | length >= 1)'; }
eventually 90 "the cluster has health history" has_health
expect "and node metrics" '[.nodes[].data[]] | length >= 1' "$(admin GET "/v0/clusters/$CLUSTER/metrics?range=24h")"
[[ $(code -b "$JAR/a" "$API/v0/overview") == 403 ]] && pass "customers can't see the overview" || fail "overview visible to a customer"

step "suspending a stops its sandboxes and blocks creates; restoring allows them again"
admin PATCH "/v0/organizations/${ORG[a]}" -d '{"status":"suspended"}' >/dev/null
eventually 60 "b's running sandbox was stopped too" is_status b "$id2" stopped
eventually 30 "creates are refused while suspended" create_is a 429
admin PATCH "/v0/organizations/${ORG[a]}" -d '{"status":"active"}' >/dev/null
cust a DELETE "/v0/sandboxes/$id2?region=$CLUSTER" >/dev/null
eventually 60 "creates work again after restore" create_ok a
id3=$(jq -r .id "$JAR/created")

step "revoking a key locks it out of the cluster"
cust a DELETE "/v0/sandboxes/$id3?region=$CLUSTER" >/dev/null || true
cust a DELETE /v0/sandbox-keys/a >/dev/null && pass "revoked in Lewbox"
eventually 30 "the cluster answers 401" whoami_is a 401
cust_in b "${ORG[a]}" DELETE /v0/sandbox-keys/b >/dev/null
cust c DELETE /v0/sandbox-keys/e2e >/dev/null

printf '\n\033[32mcustomer journey passed\033[0m\n'
