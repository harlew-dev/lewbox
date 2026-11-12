#!/usr/bin/env bash
# region-smoke.sh: end-to-end check of one Lewbox region (an AerolVM cluster)
# through a workspace's sandbox API key, the way a customer's app uses it.
#
# It creates real sandboxes and snapshots, runs commands in them, checks that
# data persists (stop/start, snapshots, volumes), that lifecycle timers fire,
# and fills the workspace up to each plan limit to check that the region
# refuses the next request with 429. Everything a run creates is deleted on
# exit, including on Ctrl-C, except the long-lived set made by --persist.
#
# Usage:
#   export SB_API_URL=http://<region>:21212 SB_API_TOKEN=avm_...
#   scripts/region-smoke.sh --max-sandboxes 5 --max-cpu 4 --max-memory-mb 12000
#
# Needs bash, curl and jq.

set -uo pipefail

usage() {
	cat <<'EOF'
Usage: region-smoke.sh [options]

Checks a region through a sandbox API key: identity, sandbox lifecycle, exec,
stop/start, persistence, snapshots, lifecycle timers, volumes, and the plan's
limits. Deletes what it creates, except the long-lived set (--persist).

Environment:
  SB_API_URL, SB_API_TOKEN   the region and a workspace's sandbox API key (required)

Expected limits (the workspace's plan; 0 or "unlimited" skips that axis):
  --max-sandboxes N          env SMOKE_MAX_SANDBOXES
  --max-cpu N                env SMOKE_MAX_CPU       (vCPUs, may be fractional)
  --max-memory-mb N          env SMOKE_MAX_MEMORY_MB (MB; the portal shows MB/1024 as "GB")

Long-lived set (one sandbox "smoke-persist" and one snapshot "smoke-persist-snap"):
  --persist                  create the set if it is missing. When the set exists,
                             every run checks it, with or without this flag: it
                             is still there, its data is intact, and the snapshot
                             still restores. The limit tests count around it.
  --persist-remove           delete the set and exit

Other options:
  --image IMG                image for test sandboxes (default ubuntu:22.04)
  --runtime R                runtime, e.g. docker or firecracker (default: the region's)
  --skip-limits              skip the plan limit tests
  --quick                    don't wait for a lifecycle timer to fire (saves ~1 min)
  --timeout SECS             how long to wait for a sandbox to start (default 180)
  -h, --help                 show this help

The workspace must have no sandboxes except the long-lived set: the limit
tests count from what it holds.

Exit status: 0 if every check passed, 1 if any failed, 2 on bad usage.
EOF
}

MAX_SANDBOXES=${SMOKE_MAX_SANDBOXES:-}
MAX_CPU=${SMOKE_MAX_CPU:-}
MAX_MEM=${SMOKE_MAX_MEMORY_MB:-}
IMAGE=${SMOKE_IMAGE:-ubuntu:22.04}
RUNTIME=${SMOKE_RUNTIME:-}
SKIP_LIMITS=0
QUICK=0
PERSIST=0
PERSIST_REMOVE=0
START_TIMEOUT=180

while (($#)); do
	case $1 in
	--max-sandboxes) MAX_SANDBOXES=${2-}; shift 2 ;;
	--max-cpu) MAX_CPU=${2-}; shift 2 ;;
	--max-memory-mb) MAX_MEM=${2-}; shift 2 ;;
	--image) IMAGE=${2-}; shift 2 ;;
	--runtime) RUNTIME=${2-}; shift 2 ;;
	--skip-limits) SKIP_LIMITS=1; shift ;;
	--quick) QUICK=1; shift ;;
	--persist) PERSIST=1; shift ;;
	--persist-remove) PERSIST_REMOVE=1; shift ;;
	--timeout) START_TIMEOUT=${2-}; shift 2 ;;
	-h | --help) usage; exit 0 ;;
	*) echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
	esac
done

for tool in curl jq awk; do
	command -v "$tool" >/dev/null || { echo "$tool is required" >&2; exit 2; }
done
[[ -n ${SB_API_URL:-} && -n ${SB_API_TOKEN:-} ]] || { echo "set SB_API_URL and SB_API_TOKEN" >&2; exit 2; }
SB_API_URL=${SB_API_URL%/}

norm_limit() { # "", "0", "unlimited" -> ""; otherwise a positive number or exit 2
	local name=$1 v=$2
	case $v in "" | 0 | unlimited | none) echo ""; return ;; esac
	[[ $v =~ ^[0-9]+([.][0-9]+)?$ ]] || { echo "$name must be a number, got: $v" >&2; exit 2; }
	echo "$v"
}
MAX_SANDBOXES=$(norm_limit --max-sandboxes "$MAX_SANDBOXES")
MAX_CPU=$(norm_limit --max-cpu "$MAX_CPU")
MAX_MEM=$(norm_limit --max-memory-mb "$MAX_MEM")
[[ -z $MAX_SANDBOXES || $MAX_SANDBOXES =~ ^[0-9]+$ ]] || { echo "--max-sandboxes must be a whole number" >&2; exit 2; }
[[ -z $MAX_MEM || $MAX_MEM =~ ^[0-9]+$ ]] || { echo "--max-memory-mb must be a whole number" >&2; exit 2; }

# ---------------------------------------------------------------- output

if [[ -t 1 ]]; then
	C_OK=$'\e[32m' C_BAD=$'\e[31m' C_WARN=$'\e[33m' C_DIM=$'\e[2m' C_B=$'\e[1m' C_0=$'\e[0m'
else
	C_OK='' C_BAD='' C_WARN='' C_DIM='' C_B='' C_0=''
fi
PASSES=0 FAILS=0 WARNS=0 SKIPS=0
FAILED=()
section() { printf '\n%s== %s%s\n' "$C_B" "$1" "$C_0"; }
pass() { PASSES=$((PASSES + 1)); printf '  %s✓%s %s\n' "$C_OK" "$C_0" "$1"; }
fail() { FAILS=$((FAILS + 1)); FAILED+=("$1"); printf '  %s✗ %s%s\n' "$C_BAD" "$1" "$C_0"; [[ -n ${2-} ]] && printf '      %s\n' "$2"; }
warn() { WARNS=$((WARNS + 1)); printf '  %s! %s%s\n' "$C_WARN" "$1" "$C_0"; }
skip() { SKIPS=$((SKIPS + 1)); printf '  %s- skipped: %s%s\n' "$C_DIM" "$1" "$C_0"; }
info() { printf '    %s%s%s\n' "$C_DIM" "$1" "$C_0"; }
note() { printf '  %s%s%s\n' "$C_DIM" "$1" "$C_0"; }
# brief: the API's error message (or the raw body), on one short line.
brief() { local m; m=$(jq -r '.error // .message // empty' <<<"$BODY" 2>/dev/null); [[ -z $m ]] && m=$BODY; printf 'HTTP %s: %.300s' "$CODE" "$(tr '\n' ' ' <<<"$m")"; }

# ---------------------------------------------------------------- numbers

calc() { awk "BEGIN { v = $1; printf \"%g\", v }"; }
num_eq() { awk -v a="$1" -v b="$2" 'BEGIN { exit !((a - b) < 0.0001 && (b - a) < 0.0001) }'; }
num_lt() { awk -v a="$1" -v b="$2" 'BEGIN { exit !(a < b - 0.0001) }'; }
num_min() { awk -v a="$1" -v b="$2" 'BEGIN { printf "%g", (a < b ? a : b) }'; }

# ---------------------------------------------------------------- HTTP

TMP=$(mktemp -d)
chmod 700 "$TMP"
# The token goes in a header file, not on curl's command line (visible in ps).
printf 'Authorization: Bearer %s\n' "$SB_API_TOKEN" >"$TMP/auth"
printf 'Authorization: Bearer avm_not_a_real_token_000000000000\n' >"$TMP/badauth"
RUN=$(date +%s)-$RANDOM
CODE='' BODY=''

api() { # api METHOD PATH [JSON] -> CODE, BODY
	local method=$1 path=$2 data=${3-} auth=${AUTH_FILE:-$TMP/auth}
	local args=(-sS -m 150 -o "$TMP/body" -w '%{http_code}' -X "$method" -H "@$auth")
	[[ -n $data ]] && args+=(-H 'Content-Type: application/json' --data-binary "$data")
	: >"$TMP/body"
	CODE=$(curl "${args[@]}" "$SB_API_URL$path" 2>"$TMP/err") || CODE=000
	BODY=$(<"$TMP/body")
	[[ $CODE == 000 ]] && BODY=$(<"$TMP/err")
	return 0
}
is_2xx() { [[ $CODE == 2?? ]]; }

# The sandbox list is a bare array in v0.7.0; accept an envelope too.
SB_LIST='if type == "array" then . else (.sandboxes // .data // []) end'

# ---------------------------------------------------------------- sandboxes

declare -A TRACKED=()  # sandbox id -> 1, deleted on exit
declare -A SNAPSHOTS=() # snapshot name -> 1, deleted on exit
SB_ID=''

cleanup() {
	trap - EXIT INT TERM
	local id name
	if ((${#TRACKED[@]} + ${#SNAPSHOTS[@]})); then
		printf '\n%sCleaning up %d sandbox(es), %d snapshot(s)…%s\n' "$C_DIM" "${#TRACKED[@]}" "${#SNAPSHOTS[@]}" "$C_0"
	fi
	for id in "${!TRACKED[@]}"; do
		api DELETE "/v1/sandboxes/$id"
		is_2xx || [[ $CODE == 404 ]] || printf '  %scould not delete sandbox %s (%s)%s\n' "$C_WARN" "$id" "$(brief)" "$C_0"
	done
	for name in "${!SNAPSHOTS[@]}"; do
		api DELETE "/v1/snapshots/$name"
		is_2xx || [[ $CODE == 404 ]] || printf '  %scould not delete snapshot %s (%s)%s\n' "$C_WARN" "$name" "$(brief)" "$C_0"
	done
	rm -rf "$TMP"
}
trap cleanup EXIT
trap 'echo; echo interrupted >&2; exit 130' INT TERM

# create_named NAME CPU MEMORY_MB [IMAGE] [EXTRA_JSON] -> 0 and SB_ID (tracked
# for cleanup), or 1 with CODE/BODY. EXTRA_JSON is merged into the request.
create_named() {
	local name=$1 cpu=$2 mem=$3 image=${4:-$IMAGE} extra=${5:-} body
	[[ -n $extra ]] || extra='{}'
	body=$(jq -nc --arg image "$image" --argjson cpu "$cpu" --argjson mem "$mem" \
		--arg name "$name" --arg runtime "$RUNTIME" --argjson extra "$extra" \
		'{image: $image, cpu: $cpu, memory_mb: $mem, name: $name, tags: {"lewbox-smoke": "1"}}
		 + (if $runtime == "" then {} else {runtime: $runtime} end) + $extra')
	api POST /v1/sandboxes "$body"
	if is_2xx; then
		SB_ID=$(jq -r '.id' <<<"$BODY")
		TRACKED[$SB_ID]=1
		return 0
	fi
	return 1
}
# create_sandbox SHORT_NAME ...: create_named with this run's prefix.
create_sandbox() { local n=$1; shift; create_named "smoke-$RUN-$n" "$@"; }

# create_sandbox_retry: as create_sandbox, but a 429 is retried for up to 15 s,
# in case a just-deleted sandbox has not given its room back yet.
create_sandbox_retry() {
	local deadline=$((SECONDS + 15))
	while :; do
		create_sandbox "$@" && return 0
		[[ $CODE == 429 ]] && ((SECONDS < deadline)) || return 1
		sleep 2
	done
}

# wait_status ID WANT [TIMEOUT] -> 0 when the sandbox reaches WANT; 1 on error or timeout.
wait_status() {
	local id=$1 want=$2 deadline=$((SECONDS + ${3:-$START_TIMEOUT})) status=''
	while ((SECONDS < deadline)); do
		api GET "/v1/sandboxes/$id"
		if is_2xx; then
			status=$(jq -r '.status' <<<"$BODY")
			[[ $status == "$want" ]] && return 0
			[[ $status == error ]] && { BODY="sandbox is in error: $(jq -c '{status, status_reason}' <<<"$BODY")"; return 1; }
		fi
		sleep 2
	done
	BODY="timed out after ${3:-$START_TIMEOUT}s waiting for $want (last status: ${status:-unknown}, $(brief))"
	return 1
}

delete_sandbox() {
	local id=$1
	api DELETE "/v1/sandboxes/$id"
	if is_2xx || [[ $CODE == 404 ]]; then unset 'TRACKED[$id]'; return 0; fi
	return 1
}

# snapshot ID NAME [EXTRA_JSON] -> CODE/BODY; a 2xx is tracked for cleanup.
snapshot() {
	local extra=${3:-}
	[[ -n $extra ]] || extra='{}'
	api POST "/v1/sandboxes/$1/snapshot" "$(jq -nc --arg n "$2" --argjson extra "$extra" '{name: $n} + $extra')"
	is_2xx && SNAPSHOTS[$2]=1
	return 0
}
delete_snapshot() {
	api DELETE "/v1/snapshots/$1"
	if is_2xx || [[ $CODE == 404 ]]; then unset 'SNAPSHOTS[$1]'; return 0; fi
	return 1
}
snapshot_listed() { # NAME -> 0 if GET /v1/snapshots lists it
	api GET /v1/snapshots
	is_2xx && jq -e --arg n "$1" '(.snapshots // []) | any(.name == $n)' <<<"$BODY" >/dev/null
}

# exec_in ID COMMAND -> EXEC_OUT, EXEC_ERR, EXEC_RC; 1 if the call itself failed.
# The toolbox can need a moment after start, so 409/5xx are retried for 30 s.
EXEC_OUT='' EXEC_ERR='' EXEC_RC=''
exec_in() {
	local id=$1 cmd=$2 body deadline=$((SECONDS + 30))
	body=$(jq -nc --arg c "$cmd" '{command: $c, timeout_seconds: 60}')
	while :; do
		api POST "/v1/sandboxes/$id/toolbox/process/execute" "$body"
		if is_2xx; then
			EXEC_OUT=$(jq -r '.stdout // ""' <<<"$BODY")
			EXEC_ERR=$(jq -r '.stderr // ""' <<<"$BODY")
			EXEC_RC=$(jq -r '.exit_code // .exitCode // "?"' <<<"$BODY")
			return 0
		fi
		[[ $CODE == 409 || $CODE == 5?? || $CODE == 000 ]] && ((SECONDS < deadline)) || return 1
		sleep 2
	done
}

# write_data ID DIR: write 8 MiB of random data, its SHA-256 and the time
# into DIR. -> DATA_SHA.
DATA_SHA=''
write_data() {
	exec_in "$1" "d=$2; mkdir -p \$d && head -c 8388608 /dev/urandom > \$d/blob.bin && date -u +%Y-%m-%dT%H:%M:%SZ > \$d/created.txt && sha256sum \$d/blob.bin | cut -d' ' -f1 | tee \$d/blob.sha256" || return 1
	[[ $EXEC_RC == 0 && $EXEC_OUT =~ ^[0-9a-f]{64} ]] || return 1
	DATA_SHA=${EXEC_OUT:0:64}
}
# check_data ID DIR: the blob still hashes to the SHA-256 written beside it.
# -> DATA_SHA, DATA_CREATED.
DATA_CREATED=''
check_data() {
	exec_in "$1" "cd $2 && sha256sum blob.bin | cut -d' ' -f1 && cat blob.sha256 && cat created.txt" || return 1
	local lines
	mapfile -t lines <<<"$EXEC_OUT"
	DATA_SHA=${lines[0]:-} DATA_CREATED=${lines[2]:-}
	[[ $EXEC_RC == 0 && ${lines[0]:-x} =~ ^[0-9a-f]{64}$ && ${lines[0]} == "${lines[1]:-}" ]]
}

list_sandboxes() { # -> BODY holds the list as a JSON array; 1 on error
	api GET /v1/sandboxes
	is_2xx || return 1
	BODY=$(jq -c "$SB_LIST" <<<"$BODY")
}

# delete_all_tracked: delete every sandbox this run made and wait until the
# key sees only the long-lived set, so the next limit test counts from there.
delete_all_tracked() {
	local id n deadline=$((SECONDS + 90))
	for id in "${!TRACKED[@]}"; do delete_sandbox "$id" || warn "delete $id: $(brief)"; done
	while ((SECONDS < deadline)); do
		list_sandboxes && n=$(jq --arg p "$PERSIST_NAME" '[.[] | select(.name != $p)] | length' <<<"$BODY") && [[ $n == 0 ]] && return 0
		sleep 2
	done
	fail "sandboxes still listed 90 s after delete" "the key still sees ${n:-?} besides the long-lived set"
	return 1
}

# quota_refused AXIS -> 0 if the last response is a 429 team_quota_exceeded
# whose message is about AXIS (sandboxes, CPU or memory).
quota_refused() {
	[[ $CODE == 429 ]] || return 1
	grep -q 'team_quota_exceeded' <<<"$BODY" || return 1
	grep -qi -- "$1" <<<"$BODY"
}
# quota_limit REGEX -> the limit the region's 429 message names, if any.
quota_limit() { grep -oE "$1" <<<"$BODY" | head -1 | grep -oE '[0-9]+([.][0-9]+)?' | tail -1; }

PERSIST_NAME=smoke-persist
PERSIST_SNAP=smoke-persist-snap
PERSIST_DIR=/root/persist

# ================================================================ checks

printf '%sLewbox region smoke test%s  %s\n' "$C_B" "$C_0" "$SB_API_URL"
printf 'expected limits: sandboxes=%s cpu=%s memory_mb=%s  image=%s%s\n' \
	"${MAX_SANDBOXES:-unlimited}" "${MAX_CPU:-unlimited}" "${MAX_MEM:-unlimited}" "$IMAGE" "${RUNTIME:+ runtime=$RUNTIME}"

# ---------------------------------------------------------------- --persist-remove

if ((PERSIST_REMOVE)); then
	section "Remove the long-lived set"
	list_sandboxes || { fail "list sandboxes" "$(brief)"; exit 1; }
	mapfile -t ids < <(jq -r --arg p "$PERSIST_NAME" '.[] | select(.name == $p) | .id' <<<"$BODY")
	for id in "${ids[@]}"; do
		api DELETE "/v1/sandboxes/$id"
		is_2xx || [[ $CODE == 404 ]] && pass "deleted sandbox $PERSIST_NAME ($id)" || fail "delete $id" "$(brief)"
	done
	((${#ids[@]})) || note "no sandbox named $PERSIST_NAME"
	api DELETE "/v1/snapshots/$PERSIST_SNAP"
	if is_2xx; then pass "deleted snapshot $PERSIST_SNAP"; elif [[ $CODE == 404 ]]; then note "no snapshot named $PERSIST_SNAP"; else fail "delete snapshot $PERSIST_SNAP" "$(brief)"; fi
	((FAILS)) && exit 1
	exit 0
fi

section "Region and key"

api GET /health
if is_2xx && [[ $(jq -r '.status' <<<"$BODY") == ok ]]; then
	pass "region healthy: $(jq -r '"AerolVM \(.version), \(.cluster_nodes // 1) node(s)"' <<<"$BODY")"
	info "$(jq -c 'del(.status, .version, .sandboxes, .cluster_nodes)' <<<"$BODY")"
else
	fail "region health" "$(brief)"
	echo "cannot reach the region; stopping" >&2
	exit 1
fi

TEAM=''
api GET /v1/whoami
if is_2xx; then
	ROLE=$(jq -r '.role' <<<"$BODY")
	TEAM=$(jq -r '.team // ""' <<<"$BODY")
	EXPIRES=$(jq -r '.expires_at // "never"' <<<"$BODY")
	if [[ $ROLE == runner && -n $TEAM ]]; then
		pass "key is a runner on team $TEAM (expires $EXPIRES)"
	else
		fail "key should be a team-bound runner" "role=$ROLE team=${TEAM:-none}"
	fi
else
	fail "whoami" "$(brief)"
	exit 1
fi

AUTH_FILE=$TMP/badauth api GET /v1/sandboxes
[[ $CODE == 401 ]] && pass "a wrong key is refused (401)" || fail "a wrong key should get 401" "$(brief)"

api GET "/v1/teams/${TEAM:-x}/quota"
[[ $CODE == 403 ]] && pass "the key cannot use admin routes (403 on its own quota)" || fail "admin route should be 403 for a runner" "$(brief)"

if ! list_sandboxes; then
	fail "list sandboxes" "$(brief)"
	exit 1
fi
OTHERS=$(jq --arg p "$PERSIST_NAME" '[.[] | select(.name != $p)] | length' <<<"$BODY")
if [[ $OTHERS == 0 ]]; then
	pass "the workspace holds no sandboxes besides the long-lived set"
else
	fail "the workspace must hold no other sandboxes" "the key sees $OTHERS: $(jq -c --arg p "$PERSIST_NAME" '[.[] | select(.name != $p) | .name // .id]' <<<"$BODY") (delete them first; the limit tests count from zero)"
	exit 1
fi

# ---------------------------------------------------------------- long-lived set

section "Long-lived sandbox and snapshot"

# persist_create: make the long-lived sandbox with data, and snapshot it.
persist_create() {
	if ! create_named "$PERSIST_NAME" 0.25 256; then
		fail "create the long-lived sandbox" "$(brief)"
		return
	fi
	local id=$SB_ID
	unset 'TRACKED[$id]' # long-lived: not cleaned up on exit
	if ! wait_status "$id" started; then
		fail "the long-lived sandbox should start" "$BODY"
		return
	fi
	if ! write_data "$id" "$PERSIST_DIR"; then
		fail "write data into the long-lived sandbox" "$(brief) rc=$EXEC_RC out=$EXEC_OUT err=$EXEC_ERR"
		return
	fi
	pass "created $PERSIST_NAME ($id, 0.25 vCPU, 256 MB) with 8 MiB of data, sha256 ${DATA_SHA:0:12}…"
	snapshot "$id" "$PERSIST_SNAP"
	if is_2xx; then
		unset 'SNAPSHOTS[$PERSIST_SNAP]'
		pass "created snapshot $PERSIST_SNAP"
	else
		fail "snapshot the long-lived sandbox" "$(brief)"
	fi
}

list_sandboxes
P_JSON=$(jq -c --arg p "$PERSIST_NAME" '[.[] | select(.name == $p)] | first // empty' <<<"$BODY")
SNAP_EXISTS=0
snapshot_listed "$PERSIST_SNAP" && SNAP_EXISTS=1

if [[ -z $P_JSON ]]; then
	if ((SNAP_EXISTS)); then
		fail "snapshot $PERSIST_SNAP exists but sandbox $PERSIST_NAME is gone" "something deleted the long-lived sandbox"
		if ((PERSIST)); then
			delete_snapshot "$PERSIST_SNAP" && note "deleted the orphaned snapshot; recreating the set"
			persist_create
		fi
	elif ((PERSIST)); then
		persist_create
	else
		skip "no long-lived set (--persist creates one)"
	fi
else
	PID=$(jq -r '.id' <<<"$P_JSON")
	PSTATUS=$(jq -r '.status' <<<"$P_JSON")
	PCREATED=$(jq -r '.created_at // "?"' <<<"$P_JSON")
	pass "$PERSIST_NAME is still there: $PID, created $PCREATED, $PSTATUS"
	if [[ $(jq -c '.lifecycle // {}' <<<"$P_JSON") != '{}' ]]; then
		warn "it has lifecycle timers: $(jq -c '.lifecycle' <<<"$P_JSON")"
	fi
	case $PSTATUS in
	started) ;;
	stopped)
		warn "it was stopped ($(jq -r '.status_reason // "no reason given"' <<<"$P_JSON")); starting it"
		api POST "/v1/sandboxes/$PID/start"
		if is_2xx && wait_status "$PID" started; then pass "started it again"; else fail "start the long-lived sandbox" "$(brief) $BODY"; fi
		;;
	*) fail "the long-lived sandbox is $PSTATUS" "$(jq -c '{status, status_reason}' <<<"$P_JSON")" ;;
	esac
	if check_data "$PID" "$PERSIST_DIR"; then
		pass "its data is intact (written $DATA_CREATED, sha256 ${DATA_SHA:0:12}…)"
	else
		fail "the long-lived sandbox lost or changed its data" "$(brief) rc=$EXEC_RC out=$EXEC_OUT err=$EXEC_ERR"
	fi
	if ((SNAP_EXISTS)); then
		pass "snapshot $PERSIST_SNAP is still listed"
		if create_sandbox persist-clone 0.25 256 "$PERSIST_SNAP" && wait_status "$SB_ID" started; then
			if check_data "$SB_ID" "$PERSIST_DIR"; then
				pass "the snapshot still restores with the same data"
			else
				fail "a sandbox from $PERSIST_SNAP should have the data" "$(brief) rc=$EXEC_RC out=$EXEC_OUT err=$EXEC_ERR"
			fi
		else
			fail "create from $PERSIST_SNAP" "$(brief) $BODY"
		fi
		delete_sandbox "$SB_ID" || warn "delete the clone: $(brief)"
	else
		fail "snapshot $PERSIST_SNAP is gone"
		if ((PERSIST)); then
			snapshot "$PID" "$PERSIST_SNAP"
			if is_2xx; then unset 'SNAPSHOTS[$PERSIST_SNAP]'; note "took it again"; else fail "snapshot the long-lived sandbox" "$(brief)"; fi
		fi
	fi
fi

# What the long-lived set holds, for the limit tests.
list_sandboxes
HELD_N=$(jq --arg p "$PERSIST_NAME" '[.[] | select(.name == $p)] | length' <<<"$BODY")
HELD_CPU=$(jq --arg p "$PERSIST_NAME" '[.[] | select(.name == $p) | .cpu] | add // 0' <<<"$BODY")
HELD_MEM=$(jq --arg p "$PERSIST_NAME" '[.[] | select(.name == $p) | .memory_mb] | add // 0' <<<"$BODY")

# The lifecycle timer test runs alongside the next sections: arm it now.
TIMER_ID='' TIMER_ARMED=0
if create_sandbox timer 0.25 256 "" '{"lifecycle": {"stop_at_age": 20000000000}}'; then
	TIMER_ID=$SB_ID
	TIMER_ARMED=$SECONDS
fi

# ---------------------------------------------------------------- lifecycle

section "Sandbox lifecycle"

MAIN='' MAIN_SHA=''
if create_sandbox main 1 1024; then
	MAIN=$SB_ID
	pass "create (1 vCPU, 1024 MB, $IMAGE): $MAIN"
	if wait_status "$MAIN" started; then
		pass "reaches started"
		api GET "/v1/sandboxes/$MAIN"
		if num_eq "$(jq -r '.cpu' <<<"$BODY")" 1 && [[ $(jq -r '.memory_mb' <<<"$BODY") == 1024 ]]; then
			pass "reports the size it was created with"
		else
			fail "reported size differs" "$(jq -c '{cpu, memory_mb}' <<<"$BODY")"
		fi
	else
		fail "sandbox should start" "$BODY"
		MAIN=''
	fi
else
	fail "create a sandbox" "$(brief)"
fi

if [[ -n $MAIN ]]; then
	if exec_in "$MAIN" 'echo hello-from-lewbox' && [[ $EXEC_OUT == hello-from-lewbox* && $EXEC_RC == 0 ]]; then
		pass "exec returns stdout and exit code 0"
	else
		fail "exec echo" "$(brief) rc=$EXEC_RC out=$EXEC_OUT"
	fi

	if exec_in "$MAIN" 'echo oops >&2; exit 3' && [[ $EXEC_RC == 3 && $EXEC_ERR == oops* ]]; then
		pass "exec reports a non-zero exit code and stderr"
	else
		fail "exec exit 3" "$(brief) rc=$EXEC_RC err=$EXEC_ERR"
	fi

	if write_data "$MAIN" /root/data; then
		MAIN_SHA=$DATA_SHA
		pass "writes 8 MiB of data in the sandbox (sha256 ${MAIN_SHA:0:12}…)"
	else
		fail "write data" "$(brief) rc=$EXEC_RC out=$EXEC_OUT err=$EXEC_ERR"
	fi

	if exec_in "$MAIN" 'echo "nproc=$(nproc) cpu.max=$(cat /sys/fs/cgroup/cpu.max 2>/dev/null || echo ?) memory.max=$(cat /sys/fs/cgroup/memory.max 2>/dev/null || echo ?)"'; then
		info "inside: $EXEC_OUT"
		mem_max=$(grep -oE 'memory.max=[0-9]+' <<<"$EXEC_OUT" | cut -d= -f2)
		if [[ -n $mem_max ]]; then
			[[ $mem_max == $((1024 * 1024 * 1024)) ]] && pass "memory limit is enforced inside (1024 MB)" || warn "memory.max is $mem_max bytes, expected $((1024 * 1024 * 1024))"
		fi
	fi

	if exec_in "$MAIN" 'getent hosts example.com >/dev/null && echo dns-ok'; then
		[[ $EXEC_OUT == dns-ok* ]] && pass "outbound DNS works" || warn "outbound DNS failed inside the sandbox (fine if the region blocks egress)"
	fi

	api POST "/v1/sandboxes/$MAIN/stop"
	if is_2xx && wait_status "$MAIN" stopped; then
		pass "stop"
		api POST "/v1/sandboxes/$MAIN/start"
		if is_2xx && wait_status "$MAIN" started; then
			pass "start again"
			if [[ -n $MAIN_SHA ]] && check_data "$MAIN" /root/data && [[ $DATA_SHA == "$MAIN_SHA" ]]; then
				pass "the data survives stop/start (same sha256)"
			else
				fail "data after stop/start" "$(brief) rc=$EXEC_RC out=$EXEC_OUT"
			fi
		else
			fail "start a stopped sandbox" "$(brief) $BODY"
		fi
	else
		fail "stop a sandbox" "$(brief)"
	fi

	if list_sandboxes && jq -e --arg id "$MAIN" 'any(.id == $id)' <<<"$BODY" >/dev/null; then
		pass "list shows it"
	else
		fail "list should include $MAIN" "$(brief)"
	fi
fi

# ---------------------------------------------------------------- snapshots

section "Snapshots"

if [[ -z $MAIN || -z $MAIN_SHA ]]; then
	skip "snapshots need the lifecycle sandbox and its data"
else
	SNAP="smoke-$RUN-snap"
	snapshot "$MAIN" "$SNAP"
	if is_2xx; then
		pass "snapshot a running sandbox: $SNAP"
		info "image: $(jq -r '.image // "?"' <<<"$BODY"), $(jq -r '.image_distribution_mode // "?"' <<<"$BODY")"

		snapshot_listed "$SNAP" && pass "snapshot is listed" || fail "snapshot should be listed" "$(brief)"

		if exec_in "$MAIN" 'echo still-running' && [[ $EXEC_OUT == still-running* ]]; then
			pass "the source keeps running after the snapshot"
		else
			fail "source after snapshot" "$(brief) out=$EXEC_OUT"
		fi

		snapshot "$MAIN" "$SNAP"
		is_2xx && info "the same name from the same sandbox again: $CODE (idempotent)" || info "the same name from the same sandbox again: $(brief)"

		snapshot "$MAIN" "smoke-$RUN-nopause" '{"allow_pause": false}'
		if [[ $CODE == 409 ]]; then
			pass "allow_pause=false on a running sandbox is refused (409)"
		else
			fail "allow_pause=false on a running sandbox should be 409" "$(brief)"
		fi

		CLONE=''
		if create_sandbox clone 0.5 512 "$SNAP"; then
			CLONE=$SB_ID
			if wait_status "$CLONE" started; then
				pass "create a sandbox from the snapshot: $CLONE"
				if check_data "$CLONE" /root/data && [[ $DATA_SHA == "$MAIN_SHA" ]]; then
					pass "the clone has the snapshot's data (same sha256)"
				else
					fail "clone should have the data" "$(brief) rc=$EXEC_RC out=$EXEC_OUT err=$EXEC_ERR"
				fi
			else
				fail "sandbox from snapshot should start" "$BODY"
				CLONE=''
			fi
		else
			fail "create from snapshot" "$(brief)"
		fi

		if [[ -n $CLONE ]]; then
			snapshot "$CLONE" "$SNAP"
			if [[ $CODE == 409 ]]; then
				pass "the same snapshot name from another sandbox is refused (409)"
			else
				fail "a taken snapshot name should be 409" "$(brief)"
			fi

			api POST "/v1/sandboxes/$CLONE/stop"
			if is_2xx && wait_status "$CLONE" stopped; then
				snapshot "$CLONE" "smoke-$RUN-stopped" '{"allow_pause": false}'
				if is_2xx; then
					pass "snapshot a stopped sandbox (allow_pause=false is fine)"
					delete_snapshot "smoke-$RUN-stopped" || warn "delete smoke-$RUN-stopped: $(brief)"
				else
					fail "snapshot a stopped sandbox" "$(brief)"
				fi
			else
				fail "stop the clone" "$(brief) $BODY"
			fi
			delete_sandbox "$CLONE" || warn "delete clone: $(brief)"
		fi

		# A snapshot is its own object: it outlives the sandbox it came from.
		if delete_sandbox "$MAIN"; then
			api GET "/v1/sandboxes/$MAIN"
			[[ $CODE == 404 ]] && pass "delete the source; it is gone (404)" || fail "deleted sandbox should be 404" "$(brief)"
			MAIN=''
			if create_sandbox orphan 0.25 256 "$SNAP" && wait_status "$SB_ID" started; then
				if check_data "$SB_ID" /root/data && [[ $DATA_SHA == "$MAIN_SHA" ]]; then
					pass "the snapshot still restores after its source is deleted"
				else
					fail "restore after the source is deleted should have the data" "$(brief) rc=$EXEC_RC out=$EXEC_OUT"
				fi
				delete_sandbox "$SB_ID" || warn "delete: $(brief)"
			else
				fail "create from a snapshot whose source is deleted" "$(brief) $BODY"
			fi
		else
			fail "delete a sandbox" "$(brief)"
		fi

		if delete_snapshot "$SNAP" && ! snapshot_listed "$SNAP"; then
			pass "delete the snapshot"
		else
			fail "delete the snapshot" "$(brief)"
		fi
		create_sandbox gone 0.25 256 "$SNAP"
		if is_2xx; then
			fail "a deleted snapshot should not restore" "a sandbox was created from it"
			delete_sandbox "$SB_ID"
		else
			pass "a deleted snapshot no longer restores ($CODE)"
		fi
	else
		fail "snapshot a sandbox" "$(brief)"
	fi
fi
[[ -n $MAIN ]] && { delete_sandbox "$MAIN" || warn "delete $MAIN: $(brief)"; }

# ---------------------------------------------------------------- timers

section "Lifecycle timers"

if [[ -z $TIMER_ID ]]; then
	fail "create a sandbox with a lifecycle timer" "$(brief)"
else
	api GET "/v1/sandboxes/$TIMER_ID"
	if [[ $(jq -r '.lifecycle.stop_at_age // 0' <<<"$BODY") == 20000000000 ]]; then
		pass "a sandbox keeps the timer it was created with (stop_at_age 20s)"
	else
		fail "lifecycle should show stop_at_age" "$(jq -c '.lifecycle' <<<"$BODY")"
	fi
	api PUT "/v1/sandboxes/$TIMER_ID/lifecycle" '{"stop_at_age": 20000000000, "destroy_if_idle_for": 3600000000000}'
	if is_2xx && [[ $(jq -r '.lifecycle.destroy_if_idle_for // 0' <<<"$BODY") == 3600000000000 ]]; then
		pass "update its timers (destroy_if_idle_for 1h)"
	else
		fail "update lifecycle" "$(brief)"
	fi
	if ((QUICK)); then
		skip "waiting for the timer to fire (--quick)"
	elif wait_status "$TIMER_ID" stopped 180; then
		api GET "/v1/sandboxes/$TIMER_ID"
		reason=$(jq -r '.status_reason // "?"' <<<"$BODY")
		[[ $reason == max_age ]] && pass "stopped by its age timer ~$((SECONDS - TIMER_ARMED))s after create (reason max_age)" || fail "timer stop reason should be max_age" "reason=$reason"
	else
		fail "the stop_at_age timer should stop the sandbox within 3 min" "$BODY"
	fi
	delete_sandbox "$TIMER_ID" || warn "delete $TIMER_ID: $(brief)"
fi

# ---------------------------------------------------------------- volumes

section "Shared volumes"

VOL="smoke-$RUN-vol"
VOLSPEC=$(jq -nc --arg n "$VOL" '{platform_volumes: [{name: $n, path: "/data", delete_with_sandbox: true}]}')
if create_sandbox vol-a 0.25 256 "" "$VOLSPEC"; then
	VA=$SB_ID
	if wait_status "$VA" started && exec_in "$VA" "echo $RUN > /data/hello && cat /data/hello" && [[ $EXEC_OUT == "$RUN"* ]]; then
		pass "a sandbox writes to volume $VOL"
		if create_sandbox vol-b 0.25 256 "" "$VOLSPEC" && wait_status "$SB_ID" started; then
			VB=$SB_ID
			exec_in "$VB" 'cat /data/hello' && [[ $EXEC_OUT == "$RUN"* ]] && pass "a second sandbox reads it" || fail "second sandbox should see the file" "$(brief) out=$EXEC_OUT err=$EXEC_ERR"
			delete_sandbox "$VA"
			exec_in "$VB" 'cat /data/hello' && [[ $EXEC_OUT == "$RUN"* ]] && pass "the volume outlives the first sandbox" || fail "volume after the first sandbox is deleted" "$(brief) out=$EXEC_OUT"
			delete_sandbox "$VB"
		else
			fail "a second sandbox with the same volume" "$(brief) $BODY"
		fi
	else
		fail "write to a volume" "$(brief) rc=$EXEC_RC out=$EXEC_OUT err=$EXEC_ERR"
	fi
elif [[ $CODE == 412 ]] || grep -qiE 'not enabled|not supported' <<<"$BODY"; then
	skip "the region has volumes off: $(jq -r '.error // empty' <<<"$BODY")"
else
	fail "create a sandbox with a volume" "$(brief)"
fi

delete_all_tracked

# ---------------------------------------------------------------- limits

section "Plan limits"

if ((HELD_N)); then
	note "the long-lived set holds $HELD_N sandbox(es), $HELD_CPU vCPU, $HELD_MEM MB; the tests fill the rest"
fi

SLOTS='' AVAIL_CPU='' AVAIL_MEM=''
[[ -n $MAX_SANDBOXES ]] && SLOTS=$((MAX_SANDBOXES - HELD_N))
[[ -n $MAX_CPU ]] && AVAIL_CPU=$(calc "$MAX_CPU - $HELD_CPU")
[[ -n $MAX_MEM ]] && AVAIL_MEM=$((MAX_MEM - HELD_MEM))

# The small size the count test uses, chosen to stay well inside the other limits.
TINY_CPU=0.25
TINY_MEM=256
if [[ -n $SLOTS ]] && ((SLOTS > 0)); then
	[[ -n $AVAIL_CPU ]] && TINY_CPU=$(num_min 0.25 "$(calc "$AVAIL_CPU / ($SLOTS + 1)")")
	[[ -n $AVAIL_MEM ]] && TINY_MEM=$(num_min 256 $((AVAIL_MEM / (SLOTS + 1))))
fi
# Fill CPU or memory with two sandboxes when the count limit allows a third.
FILL_K=2
[[ -n $SLOTS ]] && ((SLOTS < 3)) && FILL_K=1

limit_sandboxes() {
	local n=$SLOTS i ids=() got
	note "sandbox count: $n × (${TINY_CPU} vCPU, ${TINY_MEM} MB)"
	for ((i = 1; i <= n; i++)); do
		if create_sandbox "count$i" "$TINY_CPU" "$TINY_MEM"; then
			ids+=("$SB_ID")
		else
			got=$(quota_limit 'of its [0-9]+ sandboxes')
			fail "sandbox $((i + HELD_N)) of $MAX_SANDBOXES should be allowed" "$(brief)${got:+ (region limit: $got)}"
			delete_all_tracked
			return
		fi
	done
	for i in "${ids[@]}"; do wait_status "$i" started || { fail "count sandbox $i should start" "$BODY"; delete_all_tracked; return; }; done
	pass "$MAX_SANDBOXES sandboxes held (the limit)"

	if create_sandbox over "$TINY_CPU" "$TINY_MEM"; then
		fail "sandbox $((MAX_SANDBOXES + 1)) should be refused with 429" "it was created: the region allows more than $MAX_SANDBOXES"
	elif quota_refused sandboxes; then
		got=$(quota_limit 'of its [0-9]+ sandboxes')
		if [[ -z $got || $got == "$MAX_SANDBOXES" ]]; then pass "sandbox $((MAX_SANDBOXES + 1)) is refused: 429 team_quota_exceeded"; else fail "the region's sandbox limit is $got, expected $MAX_SANDBOXES" "$(brief)"; fi
		info "$(brief)"
	else
		fail "sandbox $((MAX_SANDBOXES + 1)) should be a 429 about sandboxes" "$(brief)"
	fi

	api POST "/v1/sandboxes/${ids[0]}/stop"
	if is_2xx && wait_status "${ids[0]}" stopped; then
		if create_sandbox over2 "$TINY_CPU" "$TINY_MEM"; then
			fail "a stopped sandbox should still count against the limit" "a create was allowed after stopping one"
		elif quota_refused sandboxes; then
			pass "a stopped sandbox still counts (create still 429)"
		else
			fail "create after stop should be a 429 about sandboxes" "$(brief)"
		fi
	else
		fail "stop a count sandbox" "$(brief)"
	fi

	if delete_sandbox "${ids[0]}" && create_sandbox_retry freed "$TINY_CPU" "$TINY_MEM"; then
		pass "deleting one frees a slot"
	else
		fail "create after deleting one should succeed" "$(brief)"
	fi
	delete_all_tracked
}

limit_cpu() {
	local each got i over
	each=$(calc "$AVAIL_CPU / $FILL_K")
	over=$(calc "$AVAIL_CPU + 1")
	note "vCPUs: one request over the limit, then $FILL_K × $each vCPU"

	if create_sandbox cpuover "$over" 256; then
		fail "a $over vCPU sandbox should be refused" "it was created: the region allows more than $MAX_CPU vCPUs"
		delete_all_tracked
	elif quota_refused cpu; then
		pass "one sandbox asking for $over vCPUs is refused (429)"
	else
		fail "$over vCPUs should be a 429 about CPU" "$(brief)"
	fi

	for ((i = 1; i <= FILL_K; i++)); do
		if ! create_sandbox_retry "cpu$i" "$each" 256 || ! wait_status "$SB_ID" started; then
			got=$(quota_limit 'of its [0-9.]+ CPU')
			fail "sandbox $i with $each vCPU should fit within $MAX_CPU" "$(brief)${got:+ (region limit: $got)}"
			delete_all_tracked
			return
		fi
	done
	pass "sandboxes using exactly $MAX_CPU vCPUs run"

	if create_sandbox cpumore 0.25 256; then
		fail "0.25 more vCPU should be refused" "it was created: the region allows more than $MAX_CPU vCPUs"
	elif quota_refused cpu; then
		got=$(quota_limit 'of its [0-9.]+ CPU')
		if [[ -z $got ]] || num_eq "$got" "$MAX_CPU"; then pass "0.25 more vCPU is refused: 429 team_quota_exceeded"; else fail "the region's vCPU limit is $got, expected $MAX_CPU" "$(brief)"; fi
		info "$(brief)"
	else
		fail "0.25 more vCPU should be a 429 about CPU" "$(brief)"
	fi
	delete_all_tracked
}

limit_memory() {
	local each got i over
	each=$((AVAIL_MEM / FILL_K))
	over=$((AVAIL_MEM + 1024))
	note "memory: one request over the limit, then $FILL_K × $each MB"

	if create_sandbox memover 0.25 "$over"; then
		fail "a $over MB sandbox should be refused" "it was created: the region allows more than $MAX_MEM MB"
		delete_all_tracked
	elif quota_refused memory; then
		pass "one sandbox asking for $over MB is refused (429)"
	else
		fail "$over MB should be a 429 about memory" "$(brief)"
	fi

	for ((i = 1; i <= FILL_K; i++)); do
		if ! create_sandbox_retry "mem$i" 0.25 "$each" || ! wait_status "$SB_ID" started; then
			got=$(quota_limit 'of its [0-9]+ MB')
			fail "sandbox $i with $each MB should fit within $MAX_MEM MB" "$(brief)${got:+ (region limit: $got MB)}"
			delete_all_tracked
			return
		fi
	done
	pass "sandboxes using $((each * FILL_K + HELD_MEM)) of $MAX_MEM MB run"

	if create_sandbox memmore 0.25 256; then
		fail "256 MB more should be refused" "it was created: the region allows more than $MAX_MEM MB"
	elif quota_refused memory; then
		got=$(quota_limit 'of its [0-9]+ MB')
		if [[ -z $got || $got == "$MAX_MEM" ]]; then pass "256 MB more is refused: 429 team_quota_exceeded"; else fail "the region's memory limit is $got MB, expected $MAX_MEM" "$(brief)"; fi
		info "$(brief)"
	else
		fail "256 MB more should be a 429 about memory" "$(brief)"
	fi
	delete_all_tracked
}

if ((SKIP_LIMITS)); then
	skip "--skip-limits"
else
	if [[ -z $MAX_SANDBOXES ]]; then
		skip "sandbox count (no --max-sandboxes)"
	elif ((SLOTS < 1)); then
		skip "sandbox count: the long-lived set already holds all $MAX_SANDBOXES"
	else
		limit_sandboxes
	fi
	# The CPU and memory tests need two slots: one to fill, one to be refused.
	if [[ -z $MAX_CPU ]]; then
		skip "vCPUs (no --max-cpu)"
	elif [[ -n $SLOTS ]] && ((SLOTS < 2)); then
		skip "vCPUs: needs 2 free sandbox slots"
	elif num_lt "$AVAIL_CPU" 0.5; then
		skip "vCPUs: only $AVAIL_CPU free"
	else
		limit_cpu
	fi
	if [[ -z $MAX_MEM ]]; then
		skip "memory (no --max-memory-mb)"
	elif [[ -n $SLOTS ]] && ((SLOTS < 2)); then
		skip "memory: needs 2 free sandbox slots"
	elif ((AVAIL_MEM < 768)); then
		skip "memory: only $AVAIL_MEM MB free"
	else
		limit_memory
	fi
fi

# ---------------------------------------------------------------- end

section "Left behind"
if list_sandboxes; then
	n=$(jq --arg p "$PERSIST_NAME" '[.[] | select(.name != $p)] | length' <<<"$BODY")
	stays=''
	((HELD_N)) && stays=' (the long-lived set stays)'
	[[ $n == 0 ]] && pass "no test sandboxes left$stays" || fail "sandboxes left after the run" "the key sees $n besides the long-lived set"
fi
api GET /v1/snapshots
if is_2xx; then
	left=$(jq -c --arg p "$PERSIST_SNAP" '[(.snapshots // [])[] | select(.name | startswith("smoke-")) | select(.name != $p) | .name]' <<<"$BODY")
	[[ $left == '[]' ]] && pass "no test snapshots left" || fail "snapshots left after the run" "$left"
fi

printf '\n%s%d passed%s, %s%d failed%s, %d warnings, %d skipped\n' \
	"$C_OK" "$PASSES" "$C_0" "$([[ $FAILS -gt 0 ]] && echo "$C_BAD")" "$FAILS" "$C_0" "$WARNS" "$SKIPS"
if ((FAILS)); then
	printf '%sFailed:%s\n' "$C_BAD" "$C_0"
	printf '  - %s\n' "${FAILED[@]}"
	exit 1
fi
exit 0
