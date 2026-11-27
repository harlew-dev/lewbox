#!/usr/bin/env bash
# Fill a running dev cluster with a small instance the console can show:
#
#   ./dev.sh up && ./dev.sh cluster && ./dev.sh seed
#
# Creates an instance admin and a customer, a plan, a workspace on that plan in
# cluster "lab", a sandbox key, and real sandboxes (three running, one stopped)
# plus a snapshot. Idempotent: a re-run reuses whatever already exists and only
# creates what is missing. It never deletes or duplicates anything.
#
# The sandbox key's token is shown only once, so it is saved in .cache/seed.env
# (0600) and printed from there on later runs.
set -euo pipefail
umask 077

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
API="${LEWBOX_URL:-http://127.0.0.1:${LEWBOX_DEV_PORT:-8080}}"
API="${API%/}"
CLUSTER="${LEWBOX_REGION:-${LEWBOX_DEV_CLUSTER:-lab}}"
RUNTIME="${LEWBOX_DEV_RUNTIME:-docker}"
BOOTSTRAP_EMAIL="${LEWBOX_DEV_EMAIL:-admin@lewbox.dev}"
BOOTSTRAP_PASSWORD="${LEWBOX_DEV_PASSWORD:-dev-password-123}"

ADMIN_EMAIL="admin@example.com"
CUSTOMER_EMAIL="customer@example.com"
SEED_PASSWORD="password1234"
PLAN_SLUG="dev"
PLAN_NAME="Dev"
ORG_SLUG="acme-dev"
ORG_NAME="Acme Dev"
KEY_NAME="seed"
SNAPSHOT_NAME="seed-snapshot"
IMAGE="${LEWBOX_DEV_SEED_IMAGE:-alpine:3.20}"

CACHE="$HERE/.cache"
SEED_ENV="$CACHE/seed.env"

for tool in curl jq; do
	command -v "$tool" >/dev/null || { echo "error: $tool is required" >&2; exit 2; }
done

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

say() { printf '\033[1m==> %s\033[0m\n' "$*" >&2; }
note() { printf '    %s\n' "$*" >&2; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

# error_text reads the API's error message from BODY.
error_text() {
	local text
	text=$(jq -r '.error.message // .error // .message // empty' <<<"$BODY" 2>/dev/null) || true
	[[ -n $text ]] || text=${BODY:-no response}
	printf '%s' "$text"
}

# login EMAIL PASSWORD prints the login response, or fails.
login() {
	local email=$1 password=$2
	jq -n --arg e "$email" --arg p "$password" '{email: $e, password: $p}' >"$TMP/login.json"
	curl -fsS -X POST "$API/v0/auth/login" -H 'Content-Type: application/json' --data-binary "@$TMP/login.json"
}

# write_auth FILE TOKEN stores a bearer header in FILE, away from the process list.
write_auth() { printf 'Authorization: Bearer %s\n' "$2" >"$1"; }

# call AUTH ORG METHOD PATH [JSON] prints the body and dies on a non-2xx answer.
call() {
	local auth=$1 org=$2 method=$3 path=$4 data=${5-} code
	local args=(-sS -X "$method" -H "@$auth" -H 'Content-Type: application/json')
	[[ -n $org ]] && args+=(-H "Lewbox-Organization: $org")
	if [[ -n $data ]]; then
		printf '%s' "$data" >"$TMP/request.json"
		args+=(--data-binary "@$TMP/request.json")
	fi
	code=$(curl "${args[@]}" -o "$TMP/response.json" -w '%{http_code}' "$API$path") || code=000
	BODY=$(<"$TMP/response.json")
	CODE=$code
	if [[ $code != 2?? ]]; then
		die "$method $path failed (HTTP $code): $(error_text)"
	fi
	printf '%s' "$BODY"
}

admin() { call "$TMP/admin.auth" "" "$@"; }
admin_org() { call "$TMP/admin.auth" "$ORG_SLUG" "$@"; }
cust() { call "$TMP/cust.auth" "$ORG_SLUG" "$@"; }

# avm_at BASE METHOD PATH [JSON] calls a cluster node with the seed key; sets
# CODE and BODY.
avm_at() {
	local base=$1 method=$2 path=$3 data=${4-}
	local args=(-sS -X "$method" -H "@$TMP/sb.auth" -H 'Content-Type: application/json')
	if [[ -n $data ]]; then
		printf '%s' "$data" >"$TMP/avm-request.json"
		args+=(--data-binary "@$TMP/avm-request.json")
	fi
	CODE=$(curl "${args[@]}" -o "$TMP/avm-response.json" -w '%{http_code}' "$base$path") || CODE=000
	BODY=$(<"$TMP/avm-response.json")
}
avm() { avm_at "$SB_API_URL" "$@"; }
avm_die() { die "cluster request $1 failed (HTTP $CODE): $(error_text)"; }

# ---------------------------------------------------------------- preflight

curl -fsS "$API/v0/health" >/dev/null 2>&1 \
	|| die "no orchestrator at $API; run ./dev.sh up && ./dev.sh cluster first"

if ! bootstrap=$(login "$BOOTSTRAP_EMAIL" "$BOOTSTRAP_PASSWORD"); then
	die "could not sign in to $API as the bootstrap admin $BOOTSTRAP_EMAIL; run ./dev.sh up first"
fi
admin_tok=$(jq -r '.token // empty' <<<"$bootstrap")
[[ -n $admin_tok ]] || die "the orchestrator returned no token for $BOOTSTRAP_EMAIL"
write_auth "$TMP/admin.auth" "$admin_tok"

cluster_code=$(curl -sS -o "$TMP/cluster.json" -w '%{http_code}' -H "@$TMP/admin.auth" "$API/v0/clusters/$CLUSTER") || cluster_code=000
case $cluster_code in
404) die "cluster $CLUSTER is not registered; run ./dev.sh up && ./dev.sh cluster first" ;;
2??) ;;
*) die "could not read cluster $CLUSTER (HTTP $cluster_code); run ./dev.sh up && ./dev.sh cluster first" ;;
esac
cluster_status=$(jq -r '.status // "unknown"' <"$TMP/cluster.json")
case $cluster_status in
ready | degraded) ;;
*) die "cluster $CLUSTER is $cluster_status, not ready; run ./dev.sh cluster first" ;;
esac

say "orchestrator $API, cluster $CLUSTER ($cluster_status)"

# ---------------------------------------------------------------- accounts

ensure_user() {
	local email=$1 is_admin=$2 users user payload
	users=$(admin GET /v0/users)
	user=$(jq -c --arg e "$email" '[.data[] | select(.email == $e)] | first // empty' <<<"$users")
	if [[ -z $user ]]; then
		payload=$(jq -n --arg e "$email" --arg p "$SEED_PASSWORD" --argjson a "$is_admin" \
			'{email: $e, password: $p, instance_admin: $a}')
		admin POST /v0/users "$payload" >/dev/null
		say "created account $email"
	else
		say "reusing account $email"
		if jq -e '.disabled == true' >/dev/null <<<"$user"; then
			die "account $email is disabled; enable it in the console and run ./dev.sh seed again"
		fi
	fi
	if [[ $is_admin == true ]]; then
		users=$(admin GET /v0/users)
		user=$(jq -c --arg e "$email" '[.data[] | select(.email == $e)] | first' <<<"$users")
		jq -e '.instance_admin == true' >/dev/null <<<"$user" \
			|| die "$email exists but is not an instance admin; start from a fresh dev cluster or fix the account"
	fi
}

ensure_user "$ADMIN_EMAIL" true
ensure_user "$CUSTOMER_EMAIL" false

# ---------------------------------------------------------------- plan

ensure_plan() {
	local plans plan payload
	plans=$(admin GET /v0/plans)
	plan=$(jq -c --arg s "$PLAN_SLUG" '[.data[] | select(.slug == $s)] | first // empty' <<<"$plans")
	if [[ -z $plan ]]; then
		payload=$(jq -n --arg s "$PLAN_SLUG" --arg n "$PLAN_NAME" \
			'{slug: $s, name: $n, max_sandboxes: 8, max_vcpus: 8, max_memory_mb: 8192}')
		admin POST /v0/plans "$payload" >/dev/null
		say "created plan $PLAN_SLUG (8 sandboxes, 8 vCPUs, 8 GB)"
	else
		say "reusing plan $PLAN_SLUG"
	fi
}

ensure_plan

# ---------------------------------------------------------------- workspace

ensure_workspace() {
	local orgs org status payload members member mid
	orgs=$(admin GET /v0/organizations)
	org=$(jq -c --arg s "$ORG_SLUG" '[.data[] | select(.slug == $s)] | first // empty' <<<"$orgs")
	if [[ -z $org ]]; then
		payload=$(jq -n --arg s "$ORG_SLUG" --arg n "$ORG_NAME" --arg o "$CUSTOMER_EMAIL" \
			'{slug: $s, name: $n, owner: $o}')
		admin POST /v0/organizations "$payload" >/dev/null
		say "created workspace $ORG_NAME ($ORG_SLUG)"
	else
		say "reusing workspace $ORG_NAME ($ORG_SLUG)"
		status=$(jq -r '.status // "active"' <<<"$org")
		[[ $status == active ]] || note "workspace is $status"
	fi

	# The customer owns the workspace, whether it was just created or not.
	members=$(admin_org GET /v0/members)
	member=$(jq -c --arg e "$CUSTOMER_EMAIL" '[.data[] | select(.email == $e)] | first // empty' <<<"$members")
	if [[ -z $member ]]; then
		admin_org POST /v0/members "$(jq -n --arg e "$CUSTOMER_EMAIL" '{email: $e, role: "owner"}')" >/dev/null
		say "made $CUSTOMER_EMAIL an owner of $ORG_SLUG"
	elif [[ $(jq -r '.role' <<<"$member") != owner ]]; then
		mid=$(jq -r '.id' <<<"$member")
		admin_org PATCH "/v0/members/$mid" '{"role":"owner"}' >/dev/null
		say "made $CUSTOMER_EMAIL an owner of $ORG_SLUG"
	fi
}

ensure_workspace

# ---------------------------------------------------------------- subscription

ensure_subscription() {
	local subs sub plan payload
	subs=$(admin_org GET /v0/subscriptions)
	sub=$(jq -c --arg c "$CLUSTER" '[.data[] | select(.region.slug == $c)] | first // empty' <<<"$subs")
	if [[ -z $sub ]]; then
		payload=$(jq -n --arg p "$PLAN_SLUG" '{plan: $p}')
		admin_org PATCH "/v0/subscriptions/$CLUSTER" "$payload" >/dev/null
		say "subscribed $ORG_SLUG to $CLUSTER on plan $PLAN_SLUG"
	else
		plan=$(jq -r '.plan.slug // "?"' <<<"$sub")
		say "reusing subscription to $CLUSTER (plan $plan)"
		[[ $plan == "$PLAN_SLUG" ]] || note "warning: $ORG_SLUG is on plan $plan, not $PLAN_SLUG; leaving it"
	fi
}

ensure_subscription

# ---------------------------------------------------------------- sandbox key

key=$(admin_org GET /v0/sandbox-keys | jq -c --arg n "$KEY_NAME" \
	'[.data[] | select(.name == $n and .revoked_at == null)] | first // empty')

if [[ -n $key ]]; then
	if jq -e '.expired == true' >/dev/null <<<"$key"; then
		die "sandbox key \"$KEY_NAME\" exists but has expired. Revoke it, then run ./dev.sh seed again:
  Portal: Workspace → Keys → $KEY_NAME → Revoke
  API:    curl -X DELETE -H \"Authorization: Bearer <your token>\" -H \"Lewbox-Organization: $ORG_SLUG\" $API/v0/sandbox-keys/$KEY_NAME"
	fi
	if [[ ! -f $SEED_ENV ]]; then
		die "sandbox key \"$KEY_NAME\" already exists, but its token was not saved in $SEED_ENV (it is only shown once). Revoke the key, then run ./dev.sh seed again:
  Portal: Workspace → Keys → $KEY_NAME → Revoke
  API:    curl -X DELETE -H \"Authorization: Bearer <your token>\" -H \"Lewbox-Organization: $ORG_SLUG\" $API/v0/sandbox-keys/$KEY_NAME"
	fi
	# shellcheck disable=SC1090
	source "$SEED_ENV"
	[[ -n ${SB_API_URL:-} && -n ${SB_API_TOKEN:-} ]] \
		|| die "$SEED_ENV does not set SB_API_URL and SB_API_TOKEN. Revoke key \"$KEY_NAME\", delete $SEED_ENV, and run ./dev.sh seed again"
	say "reusing sandbox key $KEY_NAME"
else
	if ! cust_login=$(login "$CUSTOMER_EMAIL" "$SEED_PASSWORD"); then
		die "could not sign in as $CUSTOMER_EMAIL with the seed password (has it been changed?). Reset it in the console, then run ./dev.sh seed again"
	fi
	cust_tok=$(jq -r '.token // empty' <<<"$cust_login")
	[[ -n $cust_tok ]] || die "the orchestrator returned no token for $CUSTOMER_EMAIL"
	write_auth "$TMP/cust.auth" "$cust_tok"

	key=$(cust POST /v0/sandbox-keys "$(jq -n --arg n "$KEY_NAME" --arg r "$CLUSTER" \
		'{name: $n, region: $r, expires_in: "never"}')")
	SB_API_URL=$(jq -r '.env.SB_API_URL // empty' <<<"$key")
	SB_API_TOKEN=$(jq -r '.token // empty' <<<"$key")
	[[ -n $SB_API_URL && -n $SB_API_TOKEN ]] || die "the orchestrator returned no sandbox key token"
	mkdir -p "$CACHE"
	{
		printf '# The seed workspace sandbox key. Generated by ./dev.sh seed; keep it private.\n'
		printf 'SB_API_URL=%q\n' "$SB_API_URL"
		printf 'SB_API_TOKEN=%q\n' "$SB_API_TOKEN"
	} >"$SEED_ENV"
	chmod 600 "$SEED_ENV"
	say "created sandbox key $KEY_NAME (saved in $SEED_ENV)"
fi
write_auth "$TMP/sb.auth" "$SB_API_TOKEN"

# Snapshots are node-local, so every member is checked, not just the endpoint.
CLUSTER_NODES=$(jq -r '[(.node_list // [])[].url] + [.admin_url] | map(select(. != null and . != "")) | unique[]' <"$TMP/cluster.json")
[[ -n $CLUSTER_NODES ]] || CLUSTER_NODES=$SB_API_URL

# The key is new for a moment: the cluster's other nodes may not know it yet.
for _ in $(seq 30); do
	avm GET /v1/whoami
	[[ $CODE == 200 ]] && break
	sleep 1
done
[[ $CODE == 200 ]] || die "the seed key was not accepted by cluster $CLUSTER (HTTP $CODE): $(error_text)"

# ---------------------------------------------------------------- sandboxes

refresh_sandboxes() {
	avm GET /v1/sandboxes
	[[ $CODE == 2?? ]] || avm_die "list sandboxes"
	SANDBOXES=$(jq -c 'if type == "array" then . else (.sandboxes // .data // []) end' <<<"$BODY")
}

# ensure_sandbox NAME sets SANDBOX_ID and SANDBOX_CREATED (1 for a new one).
ensure_sandbox() {
	local name=$1 existing payload status
	SANDBOX_ID="" SANDBOX_CREATED=0
	refresh_sandboxes
	existing=$(jq -c --arg n "$name" '[.[] | select(.name == $n and .status != "destroyed")] | first // empty' <<<"$SANDBOXES")
	if [[ -n $existing ]]; then
		SANDBOX_ID=$(jq -r '.id' <<<"$existing")
		status=$(jq -r '.status // "?"' <<<"$existing")
		say "reusing sandbox $name ($status)"
		return 0
	fi
	payload=$(jq -nc --arg n "$name" --arg i "$IMAGE" --arg r "$RUNTIME" \
		'{image: $i, name: $n, cpu: 1, memory_mb: 512} + (if $r == "" then {} else {runtime: $r} end)')
	avm POST /v1/sandboxes "$payload"
	[[ $CODE == 2?? ]] || avm_die "create sandbox $name"
	SANDBOX_ID=$(jq -r '.id // empty' <<<"$BODY")
	[[ -n $SANDBOX_ID ]] || die "creating sandbox $name returned no id"
	SANDBOX_CREATED=1
	say "created sandbox $name ($SANDBOX_ID)"
}

wait_started() {
	local id=$1 deadline=$((SECONDS + 180)) status=''
	while ((SECONDS < deadline)); do
		avm GET "/v1/sandboxes/$id"
		[[ $CODE == 2?? ]] || avm_die "read sandbox $id"
		status=$(jq -r '.status // empty' <<<"$BODY")
		case $status in
		started) return 0 ;;
		error | destroyed)
			die "sandbox $id is $status: $(jq -r '.status_reason // ""' <<<"$BODY")"
			;;
		esac
		sleep 2
	done
	die "sandbox $id did not start within 180s (last status: ${status:-unknown})"
}

stop_sandbox() {
	local id=$1 deadline=$((SECONDS + 120)) status=''
	avm POST "/v1/sandboxes/$id/stop"
	[[ $CODE == 2?? ]] || avm_die "stop sandbox $id"
	while ((SECONDS < deadline)); do
		avm GET "/v1/sandboxes/$id"
		[[ $CODE == 2?? ]] || avm_die "read sandbox $id"
		status=$(jq -r '.status // empty' <<<"$BODY")
		[[ $status == stopped ]] && return 0
		if [[ $status == error ]]; then
			die "sandbox $id is error: $(jq -r '.status_reason // ""' <<<"$BODY")"
		fi
		sleep 1
	done
	die "sandbox $id did not stop within 120s (last status: ${status:-unknown})"
}

snapshot_exists() {
	local name=$1 url attempt all_ok
	for attempt in 1 2 3 4 5; do
		all_ok=1
		for url in $CLUSTER_NODES; do
			avm_at "$url" GET /v1/snapshots
			if [[ $CODE != 200 ]]; then
				all_ok=0
				break
			fi
			if jq -e --arg n "$name" '(.snapshots // .data // .) | map(select(.name == $n)) | length > 0' <<<"$BODY" >/dev/null; then
				return 0
			fi
		done
		if ((all_ok)); then return 1; fi
		sleep 2
	done
	die "could not list snapshots on $url (HTTP $CODE): $(error_text)"
}

ensure_snapshot() {
	local name=$1 source=$2
	if snapshot_exists "$name"; then
		say "reusing snapshot $name"
		return 0
	fi
	avm POST "/v1/sandboxes/$source/snapshot" "$(jq -n --arg n "$name" '{name: $n}')"
	if [[ $CODE == 409 ]]; then
		say "reusing snapshot $name"
		return 0
	fi
	[[ $CODE == 2?? ]] || avm_die "snapshot sandbox $source as $name"
	say "created snapshot $name"
}

say "seeding sandboxes on cluster $CLUSTER ($RUNTIME)"

ensure_sandbox seed-api
api_id=$SANDBOX_ID
if ((SANDBOX_CREATED)); then wait_started "$api_id"; fi

ensure_sandbox seed-worker
if ((SANDBOX_CREATED)); then wait_started "$SANDBOX_ID"; fi

ensure_sandbox seed-runner
if ((SANDBOX_CREATED)); then wait_started "$SANDBOX_ID"; fi

ensure_sandbox seed-idle
if ((SANDBOX_CREATED)); then
	wait_started "$SANDBOX_ID"
	stop_sandbox "$SANDBOX_ID"
fi

ensure_snapshot "$SNAPSHOT_NAME" "$api_id"

# ---------------------------------------------------------------- summary

cat <<EOF

Lewbox dev instance seeded.

  Console        $API/
  Operator       $API/admin
  Instance admin $ADMIN_EMAIL / $SEED_PASSWORD
  Customer       $CUSTOMER_EMAIL / $SEED_PASSWORD
  Workspace      $ORG_NAME ($ORG_SLUG), region $CLUSTER, plan $PLAN_SLUG
  Sandboxes      seed-api, seed-worker, seed-runner (running), seed-idle (stopped)
  Snapshot       $SNAPSHOT_NAME
  Seed key       $KEY_NAME (saved in $SEED_ENV, mode 0600)

Create more sandboxes with:

  export SB_API_URL=$SB_API_URL
  export SB_API_TOKEN=$SB_API_TOKEN

EOF
