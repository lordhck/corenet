#!/usr/bin/env bash
#
# CoreNet 0.1 end-to-end smoke test.
#
# Builds corenetd and corenet, starts two nodes and two backends on
# unprivileged ports in a temporary directory, and checks that names resolve,
# that requests are proxied, and that the two nodes exchange directories.
#
# Nothing outside the temporary directory is touched, and no privileges are
# needed. Usage: test/smoke.sh

set -u

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
work=$(mktemp -d)
failures=0

DNS_PORT=${DNS_PORT:-15353}
HTTP_PORT=${HTTP_PORT:-18080}
PEER_A_PORT=${PEER_A_PORT:-17000}
PEER_B_PORT=${PEER_B_PORT:-17001}
BACKEND_A_PORT=${BACKEND_A_PORT:-18081}
BACKEND_B_PORT=${BACKEND_B_PORT:-18082}

cleanup() {
	local pid
	for pid in "${pids[@]:-}"; do
		[ -n "$pid" ] && kill "$pid" 2>/dev/null
	done
	wait 2>/dev/null
	rm -rf "$work"
}
trap cleanup EXIT

pids=()

check() { # check <description> <expected> <actual>
	if [ "$2" = "$3" ]; then
		printf 'ok   %s\n' "$1"
	else
		printf 'FAIL %s\n     expected: %s\n     actual:   %s\n' "$1" "$2" "$3"
		failures=$((failures + 1))
	fi
}

contains() { # contains <description> <needle> <haystack>
	case "$3" in
	*"$2"*) printf 'ok   %s\n' "$1" ;;
	*)
		printf 'FAIL %s\n     expected to contain: %s\n     actual: %s\n' "$1" "$2" "$3"
		failures=$((failures + 1))
		;;
	esac
}

# wait_for <seconds> <command...>: retry until the command succeeds.
wait_for() {
	local deadline=$(($(date +%s) + $1))
	shift
	until "$@" >/dev/null 2>&1; do
		[ "$(date +%s)" -ge "$deadline" ] && return 1
		sleep 0.2
	done
}

# wait_while <seconds> <command...>: retry until the command stops succeeding.
wait_while() {
	local deadline=$(($(date +%s) + $1))
	shift
	while "$@" >/dev/null 2>&1; do
		[ "$(date +%s)" -ge "$deadline" ] && return 1
		sleep 0.2
	done
}

echo "building"
go build -o "$work/corenetd" "$root/cmd/corenetd" || exit 1
go build -o "$work/corenet" "$root/cmd/corenet" || exit 1

corenet_a() { "$work/corenet" --socket "$work/a.sock" "$@"; }

cat >"$work/a.json" <<JSON
{
  "node": { "id": "smoke-a", "name": "alpha" },
  "listen": {
    "dns": "127.0.0.1:$DNS_PORT",
    "http": "127.0.0.1:$HTTP_PORT",
    "peer": "127.0.0.1:$PEER_A_PORT",
    "control": "$work/a.sock"
  },
  "services": [
    { "name": "hello.core", "address": "127.0.0.1", "port": $BACKEND_A_PORT }
  ],
  "nodes": ["127.0.0.1:$PEER_B_PORT"],
  "pull_interval_seconds": 1
}
JSON

cat >"$work/b.json" <<JSON
{
  "node": { "id": "smoke-b", "name": "beta" },
  "listen": { "dns": "", "http": "", "peer": "127.0.0.1:$PEER_B_PORT", "control": "$work/b.sock" },
  "services": [
    { "name": "wiki.core", "address": "127.0.0.1", "port": $BACKEND_B_PORT }
  ]
}
JSON

mkdir -p "$work/wiki"
echo '<!DOCTYPE html><title>wiki.core</title><h1>wiki.core</h1>' >"$work/wiki/index.html"

echo "starting backends and nodes"
python3 -m http.server "$BACKEND_A_PORT" --directory "$root/examples/hello" >/dev/null 2>&1 &
pids+=($!)
python3 -m http.server "$BACKEND_B_PORT" --directory "$work/wiki" >/dev/null 2>&1 &
pids+=($!)
"$work/corenetd" --config "$work/a.json" >"$work/a.log" 2>&1 &
pids+=($!)
"$work/corenetd" --config "$work/b.json" >"$work/b.log" 2>&1 &
pids+=($!)

if ! wait_for 10 corenet_a status; then
	echo "corenetd did not start:"
	cat "$work/a.log"
	exit 1
fi

echo
echo "-- local service --"
contains "status reports the node" "smoke-a" "$(corenet_a status)"
contains "the configured service is listed" "hello.core" "$(corenet_a service list)"
contains "resolve returns the backend" "127.0.0.1:$BACKEND_A_PORT" "$(corenet_a resolve hello.core)"
contains "the proxy serves the example page" "hello.core" \
	"$(curl -s -H 'Host: hello.core' "http://127.0.0.1:$HTTP_PORT/")"
check "an unknown name is 404" "404" \
	"$(curl -s -o /dev/null -w '%{http_code}' -H 'Host: nope.core' "http://127.0.0.1:$HTTP_PORT/")"

echo
echo "-- runtime registration --"
contains "a service can be registered" "docs.core" "$(corenet_a service add docs.core 127.0.0.1 "$BACKEND_A_PORT")"
contains "registering it twice is refused" "already registered" "$(corenet_a service add docs.core 127.0.0.1 1234 2>&1)"
contains "a name outside .core is refused" "namespace" "$(corenet_a service add docs.test 127.0.0.1 1234 2>&1)"
contains "a configured service cannot be removed" "configuration file" "$(corenet_a service remove hello.core 2>&1)"
contains "a registered service can be removed" "removed" "$(corenet_a service remove docs.core)"

echo
echo "-- dns --"
if command -v dig >/dev/null 2>&1; then
	dig_flags="+time=2 +tries=1 @127.0.0.1 -p $DNS_PORT"
	# shellcheck disable=SC2086
	check "a known name resolves to the proxy" "127.0.0.1" "$(dig +short $dig_flags hello.core A)"
	# shellcheck disable=SC2086
	contains "an unknown .core name is NXDOMAIN" "NXDOMAIN" "$(dig $dig_flags nope.core A)"
	# shellcheck disable=SC2086
	contains "a name outside .core is REFUSED" "REFUSED" "$(dig $dig_flags example.com A)"
	# shellcheck disable=SC2086
	check "dns answers over tcp too" "127.0.0.1" "$(dig +short +tcp $dig_flags hello.core A)"
else
	echo "skip dig is not installed; skipping the DNS checks"
fi

echo
echo "-- two nodes --"
if wait_for 15 corenet_a resolve wiki.core; then
	contains "the peer's service is resolvable" "127.0.0.1:$BACKEND_B_PORT" "$(corenet_a resolve wiki.core)"
	contains "it is attributed to the peer" "smoke-b" "$(corenet_a service list)"
	contains "the peer is online" "online" "$(corenet_a node list)"
	contains "the peer's service is proxied" "wiki.core" \
		"$(curl -s -H 'Host: wiki.core' "http://127.0.0.1:$HTTP_PORT/")"
	contains "a node does not relay a directory" "hello.core" \
		"$(curl -s "http://127.0.0.1:$PEER_A_PORT/v1/services")"
	check "the peer API accepts no writes" "404" \
		"$(curl -s -o /dev/null -w '%{http_code}' -X POST -d '{}' "http://127.0.0.1:$PEER_A_PORT/v1/services")"

	# Stopping the peer must withdraw everything it advertised.
	kill "${pids[3]}" 2>/dev/null
	if wait_while 15 corenet_a resolve wiki.core; then
		printf 'ok   %s\n' "a stopped node's services are withdrawn"
	else
		printf 'FAIL %s\n' "a stopped node's services are withdrawn"
		failures=$((failures + 1))
	fi
else
	printf 'FAIL %s\n' "the peer's directory was never pulled"
	failures=$((failures + 1))
	cat "$work/a.log"
fi

echo
echo "-- docker discovery --"
if [ -S /var/run/docker.sock ] && docker info >/dev/null 2>&1; then
	name="corenet-smoke-$$"
	if docker run -d --rm --name "$name" \
		--label corenet.name=smoke.core \
		--label corenet.service=http \
		--label corenet.port=80 \
		nginx:alpine >/dev/null 2>&1; then

		if wait_for 20 corenet_a resolve smoke.core; then
			contains "a labelled container becomes a service" "docker" "$(corenet_a resolve smoke.core)"
			contains "it is served through the proxy" "nginx" \
				"$(curl -s -D - -o /dev/null -H 'Host: smoke.core' "http://127.0.0.1:$HTTP_PORT/")"
			contains "it cannot be removed through the API" "container" \
				"$(corenet_a service remove smoke.core 2>&1)"
		else
			printf 'FAIL %s\n' "a labelled container becomes a service"
			failures=$((failures + 1))
		fi

		docker rm -f "$name" >/dev/null 2>&1
		if wait_while 20 corenet_a resolve smoke.core; then
			printf 'ok   %s\n' "the service goes when the container goes"
		else
			printf 'FAIL %s\n' "the service goes when the container goes"
			failures=$((failures + 1))
		fi
	else
		echo "skip could not start a test container; skipping the Docker checks"
	fi
else
	echo "skip Docker is not available; skipping the Docker checks"
fi

echo
if [ "$failures" -eq 0 ]; then
	echo "all checks passed"
else
	echo "$failures check(s) failed"
fi
exit "$failures"
