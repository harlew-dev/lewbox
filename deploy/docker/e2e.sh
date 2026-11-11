#!/usr/bin/env bash
# End-to-end smoke test of the Lewbox orchestrator image, against the real
# container: API, web console, docs, cookie sessions, CSRF, rate limiting and
# the unprivileged user.
#
#   deploy/docker/e2e.sh                # build lewbox:local, then test it
#   SKIP_BUILD=1 deploy/docker/e2e.sh   # reuse an image built earlier
#
# Needs bash, curl and docker. jq is used when installed, grep/sed otherwise.
# The container and its anonymous volume are removed on exit; nothing
# host-wide is cleaned up.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."

IMAGE="${IMAGE:-lewbox:local}"
NAME="lewbox-e2e-$$"
EMAIL="${E2E_EMAIL:-admin@example.com}"
PASSWORD="${E2E_PASSWORD:-e2e-password-123}"
RL_EMAIL="e2e-ratelimit-$$@example.com"

TMP="$(mktemp -d)"
BODY="$TMP/body"
HEADERS="$TMP/headers"
JAR="$TMP/cookies"
INDEX="$TMP/index.html"

if command -v jq >/dev/null 2>&1; then HAVE_JQ=1; else HAVE_JQ=0; fi

STEP=0
step() { STEP=$((STEP + 1)); printf '\n\033[1m[%d] %s\033[0m\n' "$STEP" "$*"; }
pass() { printf '\033[32mPASS\033[0m %s\n' "$*"; }
fail() {
	printf '\033[31mFAIL\033[0m %s\n' "$*" >&2
	if [ -n "${BASE:-}" ]; then
		echo "--- last response body" >&2
		cat "$BODY" >&2 2>/dev/null || true
		echo >&2
	fi
	docker logs "$NAME" 2>&1 | tail -20 >&2 || true
	exit 1
}

cleanup() {
	docker rm -fv "$NAME" >/dev/null 2>&1 || true
	rm -rf "$TMP"
}
trap cleanup EXIT

# req <method> <path> [curl args...]: fills $BODY, $HEADERS and $STATUS.
req() {
	local method=$1 path=$2
	shift 2
	STATUS=$(curl -sS -o "$BODY" -D "$HEADERS" -w '%{http_code}' -X "$method" "$@" "$BASE$path") ||
		fail "curl failed for $method $path"
}

# expect <status> <description>
expect() {
	local want=$1 what=$2
	[ "$STATUS" = "$want" ] || fail "$what: HTTP $STATUS, want $want"
	pass "$what"
}

has_header() { grep -qi -- "$1" "$HEADERS"; }

json_true() { # json_true <jq filter>
	[ "$HAVE_JQ" = 1 ] && jq -e "$1" "$BODY" >/dev/null 2>&1
}

# --- build and start --------------------------------------------------------
if [ "${SKIP_BUILD:-0}" != "1" ]; then
	step "build $IMAGE"
	# Build through compose: it uses the daemon's BuildKit, so it works without
	# the buildx plugin that a plain Docker CLI build needs. The password
	# is only there to satisfy compose.yaml's required variable.
	LEWBOX_ADMIN_PASSWORD=unused-at-build-time docker compose build lewbox || fail "docker compose build failed"
	pass "image built"
fi

step "start the container as a non-root user with a fresh /data volume"
CONTAINER_ID=$(docker run -d \
	--name "$NAME" \
	-p 127.0.0.1::8080 \
	-v /data \
	-e LEWBOX_BOOTSTRAP_ADMIN_EMAIL="$EMAIL" \
	-e LEWBOX_BOOTSTRAP_ADMIN_PASSWORD="$PASSWORD" \
	-e LEWBOX_CHECK_INTERVAL=0 \
	"$IMAGE") || fail "docker run failed"
pass "container $NAME is $CONTAINER_ID"

MAPPING=$(docker port "$NAME" 8080/tcp) || fail "no published port for 8080"
BASE="http://${MAPPING}"

ready=0
for ((i = 0; i < 30; i++)); do
	if curl -fsS -o /dev/null "$BASE/v0/health" 2>/dev/null; then
		ready=1
		break
	fi
	sleep 1
done
[ "$ready" = 1 ] || fail "the container did not answer /v0/health within 30s"
pass "healthy on $BASE"

# --- web console and docs ---------------------------------------------------
step "the web console is served"
req GET /
expect 200 "GET / returns 200"
grep -q '<div id="root">' "$BODY" || fail "GET / is missing <div id=\"root\">"
has_header '^Content-Security-Policy: .*default-src' || fail "GET / has no Content-Security-Policy header"
pass "GET / is the SPA, with a Content-Security-Policy"
cp "$BODY" "$INDEX"

req GET /docs/reference/cli
expect 200 "GET /docs/reference/cli returns 200"
[ "$(cat "$BODY")" = "$(cat "$INDEX")" ] || fail "the docs route did not serve the same SPA HTML"
pass "client-side routes serve the same SPA"

step "the docs API is mounted"
req GET /v0/docs
expect 200 "GET /v0/docs returns 200"
if [ "$HAVE_JQ" = 1 ]; then
	json_true '.groups | length > 0' || fail "GET /v0/docs has no groups"
else
	grep -q '"groups":\[{' "$BODY" || fail "GET /v0/docs has no groups"
fi
pass "GET /v0/docs has groups"

req GET /v0/docs/getting-started/quickstart
expect 200 "GET /v0/docs/getting-started/quickstart returns 200"
if [ "$HAVE_JQ" = 1 ]; then
	json_true '.title | length > 0' || fail "the quickstart page has no title"
else
	grep -q '"title":"[^"]' "$BODY" || fail "the quickstart page has no title"
fi
pass "the quickstart page has a title"

# --- authentication ---------------------------------------------------------
step "authentication is required"
req GET /v0/me
expect 401 "GET /v0/me without a cookie returns 401"

step "a browser session needs the right password"
req POST /v0/auth/session \
	-H 'Content-Type: application/json' \
	-d "{\"email\":\"$EMAIL\",\"password\":\"definitely-wrong\"}"
expect 401 "a wrong password returns 401"

req POST /v0/auth/session \
	-c "$JAR" \
	-H 'Content-Type: application/json' \
	-d "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}"
expect 200 "the right password returns 200"
has_header '^Set-Cookie: lewbox_session=[^;]*;.*HttpOnly' || fail "the session cookie is not HttpOnly"
has_header '^Set-Cookie: lewbox_session=[^;]*;.*SameSite=Strict' || fail "the session cookie is not SameSite=Strict"
pass "the session cookie is HttpOnly and SameSite=Strict"
if grep -q '"token"' "$BODY"; then fail "the session response leaked a token"; fi
pass "the session response has no token field"

step "the cookie signs in and reads the API"
req GET /v0/me -b "$JAR"
expect 200 "GET /v0/me with the cookie returns 200"
grep -q "$EMAIL" "$BODY" || fail "GET /v0/me does not name $EMAIL"
pass "GET /v0/me names $EMAIL"

req GET /v0/clusters -b "$JAR"
expect 200 "GET /v0/clusters returns 200"
[ "$(tr -d '[:space:]' <"$BODY")" = '{"data":[]}' ] || fail "GET /v0/clusters did not return an empty list"
pass 'GET /v0/clusters is {"data":[]}'

step "cross-site and non-JSON cookie requests are refused"
req POST /v0/clusters \
	-b "$JAR" \
	-H 'Content-Type: application/json' \
	-H 'Sec-Fetch-Site: cross-site' \
	-d '{"slug":"e2e"}'
expect 403 "a cross-site POST with the cookie returns 403"

req POST /v0/clusters \
	-b "$JAR" \
	-H 'Content-Type: text/plain' \
	-d '{"slug":"e2e"}'
expect 415 "a text/plain POST with the cookie returns 415"

step "the bearer token flow still works"
req POST /v0/auth/login \
	-H 'Content-Type: application/json' \
	-d "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}"
expect 200 "POST /v0/auth/login returns 200"
if [ "$HAVE_JQ" = 1 ]; then
	TOKEN=$(jq -r '.token // empty' "$BODY")
else
	TOKEN=$(sed -n 's/.*"token":"\([^"]*\)".*/\1/p' "$BODY")
fi
[ -n "$TOKEN" ] || fail "POST /v0/auth/login returned no token"
req GET /v0/me -H "Authorization: Bearer $TOKEN"
expect 200 "GET /v0/me with a bearer token returns 200"

step "logging out ends the session"
req POST /v0/auth/logout -b "$JAR" -c "$JAR"
expect 200 "POST /v0/auth/logout returns 200"
req GET /v0/me -b "$JAR"
expect 401 "GET /v0/me after logout returns 401"

step "sign-in is rate limited"
limited=0
for i in 1 2 3 4 5 6; do
	req POST /v0/auth/session \
		-H 'Content-Type: application/json' \
		-d "{\"email\":\"$RL_EMAIL\",\"password\":\"definitely-wrong\"}"
	case "$STATUS" in
	401) ;;
	429)
		has_header '^Retry-After: [0-9]' || fail "the 429 has no Retry-After header"
		pass "the 6th request (after $i attempts) is limited, with Retry-After"
		limited=1
		break
		;;
	*) fail "a wrong password returned HTTP $STATUS, want 401 or 429" ;;
	esac
done
[ "$limited" = 1 ] || fail "no 429 after six wrong passwords"

# --- what is inside ---------------------------------------------------------
step "the process is unprivileged and the database lives in /data"
UID_OUT=$(docker exec "$NAME" id -u) || fail "docker exec id -u failed"
[ "$UID_OUT" = "10001" ] || fail "the container runs as uid $UID_OUT, want 10001"
pass "the process runs as uid 10001"
docker exec "$NAME" test -f /data/lewbox/lewbox.db || fail "no /data/lewbox/lewbox.db"
pass "the database is at /data/lewbox/lewbox.db"

echo
echo "e2e: all checks passed"
