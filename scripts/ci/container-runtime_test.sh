#!/usr/bin/env bash
# No live infrastructure or secrets: smoke-test the two release images.
set -euo pipefail
docker run --rm --network none --entrypoint sh "${APP_TEST_IMAGE:-darkvoid:ci}" -ec '
test "$(id -u)" = 100
test "$(id -g)" = 101
test -w /app/uploads
test ! -w /app
test ! -w /app/darkvoid
test -x /app/darkvoid
test -x /app/seed
test -x /app/darkvoidctl
test ! -e /app/.env
test -s /etc/ssl/certs/ca-certificates.crt
test -e /usr/share/zoneinfo/Asia/Ho_Chi_Minh
/app/seed --help >/dev/null 2>&1
/app/darkvoidctl --help >/dev/null
printf "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok" | nc -l -p 8080 -w 2 >/dev/null &
probe_pid=$!
sleep 0.1
test "$(wget -qO- http://localhost:8080/health)" = ok
wait "$probe_pid"
printf "HTTP/1.1 503 Service Unavailable\r\nContent-Length: 0\r\nConnection: close\r\n\r\n" | nc -l -p 8080 -w 2 >/dev/null &
probe_pid=$!
sleep 0.1
if wget -qO- http://localhost:8080/health 2>/dev/null; then exit 1; fi
wait "$probe_pid"
'
docker run --rm --network none --read-only --mount type=volume,destination=/state \
	--tmpfs /tmp:size=256m,mode=1777 --entrypoint bash "${BACKUP_TEST_IMAGE:-darkvoid-backup:ci}" -ec '
test "$(id -u)" != 0
test -w /state
test -w /state/cache
test -w /tmp
test ! -w /usr/local/bin/postgres-restic.sh
bash -n /usr/local/bin/postgres-restic.sh
pg_dump --version
pg_restore --version
restic version
curl --version
jq --version
'
echo 'Container runtime tests passed'
