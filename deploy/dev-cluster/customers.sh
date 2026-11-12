#!/usr/bin/env bash
# The customer journey, end to end, against a running dev cluster:
#
#   ./dev.sh up && ./dev.sh cluster && ./customers.sh
#
# Sign-up, a sandbox key, a sandbox run with that key straight on the cluster,
# Lewbox's mirror and isolation, the plan's limit enforced by AerolVM, stop and
# delete from Lewbox, usage, monitoring, suspension, credits and revocation.
# e2e.sh runs it too. Each step prints PASS or FAIL; it exits 1 on a failure.
set -euo pipefail

API="http://127.0.0.1:${LEWBOX_DEV_PORT:-8080}"
CLUSTER="${LEWBOX_DEV_CLUSTER:-lab}"
RUN="$(date +%s)"
JAR="$(mktemp -d)"
trap 'rm -rf "$JAR"' EXIT

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
	-d "{\"email\":\"${LEWBOX_DEV_EMAIL:-admin@lewbox.dev}\",\"password\":\"${LEWBOX_DEV_PASSWORD:-dev-password-123}\"}" | jq -r .token)
admin() { local m="$1" p="$2"; shift 2; curl -fsS -X "$m" -H "Authorization: Bearer $tok" -H 'Content-Type: application/json' "$API$p" "$@"; }

# A customer: a browser session cookie, and their organization in the header.
declare -A ORG KEY
cust() { local who="$1" m="$2" p="$3"; shift 3; curl -fsS -X "$m" -b "$JAR/$who" -c "$JAR/$who" -H 'Content-Type: application/json' -H "Lewbox-Organization: ${ORG[$who]:-}" "$API$p" "$@"; }
code() { curl -s -o /dev/null -w '%{http_code}' "$@"; }

# The cluster's own API with a customer's sandbox key. EP is the region's endpoint.
EP=""
avm() { local who="$1" m="$2" p="$3"; shift 3; curl -sS -X "$m" -H "Authorization: Bearer ${KEY[$who]}" -H 'Content-Type: application/json' "$EP$p" "$@"; }
SANDBOX='{"image":"alpine:3.20","cpu":1,"memory_mb":512}'
create_code() { avm "$1" POST /v1/sandboxes -d "$SANDBOX" -o "$JAR/created" -w '%{http_code}'; }
status_of() { avm "$1" GET "/v1/sandboxes/$2" | jq -r .status; }
# Customer GETs as plain functions, so `eventually` can call them.
sandboxes_of() { cust "$1" GET /v0/sandboxes; }
limits_of() { cust "$1" GET /v0/limits; }
usage_of() { cust "$1" GET /v0/usage; }
org_of() { admin GET "/v0/organizations/$1"; }

has_sandbox() { sandboxes_of "$1" | jq -e "[.data[] | select(.id == \"$2\" and .region == \"$CLUSTER\")] | length == 1"; }
quota_pushed() { limits_of "$1" | jq -e "[.regions[] | select(.slug == \"$CLUSTER\") | .quota.max_sandboxes] == [$2]"; }
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
s=$(admin PATCH /v0/settings -d "{\"signup\":\"open\",\"default_plan\":\"$plan\"}")
expect "sign-up open, plan is the default" ".signup == \"open\" and .default_plan == \"$plan\"" "$s"
expect "the public instance info says so" '.signup == "open"' "$(curl -fsS "$API/v0/instance")"

step "two customers sign up and get organizations of their own on that plan"
for who in a b; do
	r=$(cust "$who" POST /v0/auth/signup -d "{\"email\":\"$who-$RUN@e2e.dev\",\"password\":\"customer-password-123\"}")
	expect "$who signed up" ".user.email == \"$who-$RUN@e2e.dev\" and (.user.instance_admin | not)" "$r"
	me=$(cust "$who" GET /v0/me)
	ORG[$who]=$(jq -r '.organizations[0].slug' <<<"$me")
	expect "$who owns an active organization" '(.organizations | length == 1) and .organizations[0].role == "owner" and .organizations[0].status == "active"' "$me"
	o=$(cust "$who" GET "/v0/organizations/${ORG[$who]}")
	expect "$who is on $plan, and can't see its team" ".plan.slug == \"$plan\" and .limits.sandboxes == 2 and (.team == null)" "$o"
done
[[ $(code -b "$JAR/a" "$API/v0/clusters") == 403 ]] && pass "a customer can't list clusters" || fail "clusters visible to a customer"
expect "a customer sees the region" "[.data[] | select(.slug == \"$CLUSTER\")] | length == 1" "$(cust a GET /v0/regions)"

step "each customer creates a sandbox key for the region"
for who in a b; do
	k=$(cust "$who" POST /v0/sandbox-keys -d "{\"name\":\"e2e\",\"region\":\"$CLUSTER\"}")
	expect "$who's key is an avm_ token with SDK settings" '(.token | startswith("avm_")) and .env.SB_API_TOKEN == .token and (.e2b_env.E2B_API_URL | endswith("/e2b"))' "$k"
	KEY[$who]=$(jq -r .token <<<"$k")
	EP=$(jq -r .env.SB_API_URL <<<"$k")
done
expect "the key list never shows a token" '[.. | strings | select(startswith("avm_") and length > 20)] | length == 0' "$(cust a GET /v0/sandbox-keys)"
eventually 30 "a's key works on the cluster" whoami_is a 200
eventually 30 "b's key works on the cluster" whoami_is b 200

step "a runs a sandbox with its key, straight on the cluster (no Lewbox in the path)"
create_ok a && pass "sandbox started" || { cat "$JAR/created" >&2; fail "create"; }
id1=$(jq -r .id "$JAR/created")
out=$(avm a POST "/v1/sandboxes/$id1/toolbox/process/execute" -d '{"command":"echo hello-from-sandbox"}')
grep -q 'hello-from-sandbox' <<<"$out" && pass "a command ran in it" || { echo "$out" >&2; fail "exec"; }

step "Lewbox mirrors it for a, and only for a"
eventually 60 "a's sandbox list has it" has_sandbox a "$id1"
expect "b's list doesn't" "[.data[] | select(.id == \"$id1\")] | length == 0" "$(sandboxes_of b)"
[[ $(avm b GET "/v1/sandboxes/$id1" -o /dev/null -w '%{http_code}') == 404 ]] && pass "b's key gets 404 for a's sandbox" || fail "b reached a's sandbox"
[[ $(code -X POST -b "$JAR/b" -H "Lewbox-Organization: ${ORG[b]}" "$API/v0/sandboxes/$id1/stop?region=$CLUSTER") == 404 ]] && pass "b can't stop it through Lewbox" || fail "b stopped a's sandbox"

step "the plan's limit (2 sandboxes) is enforced by AerolVM"
eventually 60 "the quota reached the cluster" quota_pushed a 2
create_ok a && pass "a second sandbox is fine" || { cat "$JAR/created" >&2; fail "second sandbox"; }
id2=$(jq -r .id "$JAR/created")
c3=$(create_code a)
[[ $c3 == 429 ]] && pass "a third is refused with 429" || { cat "$JAR/created" >&2; fail "third sandbox got $c3, want 429"; }

step "a stops and deletes a sandbox through Lewbox"
expect "stopped" '.status == "stopped"' "$(cust a POST "/v0/sandboxes/$id1/stop?region=$CLUSTER")"
cust a DELETE "/v0/sandboxes/$id1?region=$CLUSTER" >/dev/null && pass "deleted"
[[ $(avm a GET "/v1/sandboxes/$id1" -o /dev/null -w '%{http_code}') == 404 ]] && pass "gone on the cluster" || fail "still on the cluster"

step "usage is recorded and the operator sees the fleet"
eventually 60 "a has usage" has_usage a
expect "overview counts customers and running sandboxes" '.customers.total >= 2 and .sandboxes.running >= 1' "$(admin GET /v0/overview)"
expect "the cluster has health history" '(.cluster.days | length == 30) and ([.cluster.days[] | select(.uptime != null)] | length >= 1) and (.nodes | length >= 1)' "$(admin GET "/v0/clusters/$CLUSTER/health?days=30")"
expect "and node metrics" '[.nodes[].data[]] | length >= 1' "$(admin GET "/v0/clusters/$CLUSTER/metrics?range=24h")"
[[ $(code -b "$JAR/a" "$API/v0/overview") == 403 ]] && pass "customers can't see the overview" || fail "overview visible to a customer"

step "suspending a stops its sandboxes and blocks creates; restoring allows them again"
admin PATCH "/v0/organizations/${ORG[a]}" -d '{"status":"suspended"}' >/dev/null
eventually 60 "a's running sandbox was stopped" is_status a "$id2" stopped
eventually 30 "creates are refused while suspended" create_is a 429
admin PATCH "/v0/organizations/${ORG[a]}" -d '{"status":"active"}' >/dev/null
cust a DELETE "/v0/sandboxes/$id2?region=$CLUSTER" >/dev/null
eventually 60 "creates work again after restore" create_ok a
id3=$(jq -r .id "$JAR/created")

step "billing: with credits enforced, an empty balance suspends; a top-up restores"
admin PATCH /v0/settings -d '{"billing":"enforce"}' >/dev/null
eventually 60 "a is suspended for credits" org_is "${ORG[a]}" '.status == "suspended" and .status_reason == "credits"'
cr=$(admin POST /v0/credits -H "Lewbox-Organization: ${ORG[a]}" -d "{\"amount_micros\":5000000,\"note\":\"e2e\",\"reference\":\"e2e-$RUN\"}")
expect "credits added" '.amount_micros == 5000000' "$cr"
[[ $(code -X POST -H "Authorization: Bearer $tok" -H "Lewbox-Organization: ${ORG[a]}" -H 'Content-Type: application/json' "$API/v0/credits" -d "{\"amount_micros\":1,\"reference\":\"e2e-$RUN\"}") == 409 ]] && pass "the same reference can't credit twice" || fail "duplicate reference accepted"
eventually 60 "a is active again" org_is "${ORG[a]}" '.status == "active"'
expect "a sees its balance" '.balance_micros > 0' "$(cust a GET /v0/credits)"
admin PATCH /v0/settings -d '{"billing":"off"}' >/dev/null

step "revoking a key locks it out of the cluster"
cust a DELETE "/v0/sandboxes/$id3?region=$CLUSTER" >/dev/null || true
cust a DELETE /v0/sandbox-keys/e2e >/dev/null && pass "revoked in Lewbox"
eventually 30 "the cluster answers 401" whoami_is a 401
cust b DELETE /v0/sandbox-keys/e2e >/dev/null

admin PATCH /v0/settings -d '{"signup":"closed","default_plan":"free"}' >/dev/null
printf '\n\033[32mcustomer journey passed\033[0m\n'
