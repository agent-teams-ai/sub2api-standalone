#!/usr/bin/env bash
set -euo pipefail

# Runs as the image's postgres user, with no network, no capabilities and a
# read-only root. Only disposable tmpfs and the bounded receipt directory write.
test "$(id -u)" -ne 0
case "$(postgres --version)" in
  'postgres (PostgreSQL) 17.10'|'postgres (PostgreSQL) 17.10 '*) ;;
  *) exit 1 ;;
esac
test "$GATEWAY_NATIVE_ROLLBACK_READER_SOURCE" = a59da2e24adec39b8b21a512bc6721bf7fa2a4c1
test -x /probe/reader.test
test -x /probe/current-writer.test
test -d /probe/output

probe_tmp=$(mktemp -d /tmp/native-rollback.XXXXXXXX)
chmod 700 "$probe_tmp"
pg_started=0
stop_pg() {
  if [ "$pg_started" -eq 1 ]; then
    pg_ctl -D "$probe_tmp/data" -m fast -w -t 10 stop > /dev/null ||
      pg_ctl -D "$probe_tmp/data" -m immediate -w -t 10 stop > /dev/null
  fi
}
trap stop_pg EXIT
initdb -D "$probe_tmp/data" --username=postgres --auth=trust --no-locale > "$probe_tmp/initdb.log"
pg_ctl -D "$probe_tmp/data" -l "$probe_tmp/postgres.log" -w -t 15 \
  -o "-h 127.0.0.1 -p 15432 -F -k $probe_tmp" start > /dev/null
pg_started=1
createdb -h 127.0.0.1 -p 15432 -U postgres native_connection_rollback_ci

export GATEWAY_NATIVE_ROLLBACK_DSN='postgres://postgres@127.0.0.1:15432/native_connection_rollback_ci?sslmode=disable'
export GATEWAY_NATIVE_ROLLBACK_READER=/probe/reader.test
export GATEWAY_NATIVE_ROLLBACK_RECEIPT=/probe/output/native-rollback-reader.json
/probe/current-writer.test -test.run '^TestGatewayNativeConnectionRollbackWriter$' \
  -test.count=1 -test.v -test.timeout=120s | tee /probe/output/rollback.log
test "$(grep -c '^--- PASS: TestGatewayNativeConnectionRollbackWriter ' /probe/output/rollback.log)" -eq 1
if grep -Fq -- '--- SKIP:' /probe/output/rollback.log; then exit 1; fi
