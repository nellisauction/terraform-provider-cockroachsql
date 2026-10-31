#!/usr/bin/env bash
# Starts a secure single-node CockroachDB in Docker and creates a non-root
# admin user for the acceptance tests.
#
# Usage: start-secure-cockroach.sh <image> <host-port> <user> <password> <database>
set -euo pipefail

image=$1
port=$2
user=$3
password=$4
database=$5

workdir=$(mktemp -d)
mkdir -p "$workdir/certs" "$workdir/safe"

crdb() {
  docker run --rm -v "$workdir/certs:/certs" -v "$workdir/safe:/safe" "$image" "$@"
}

crdb cert create-ca --certs-dir=/certs --ca-key=/safe/ca.key
crdb cert create-node localhost 127.0.0.1 --certs-dir=/certs --ca-key=/safe/ca.key
crdb cert create-client root --certs-dir=/certs --ca-key=/safe/ca.key

docker run -d --name crdb-secure -p "$port:26257" -v "$workdir/certs:/certs" "$image" \
  start-single-node --certs-dir=/certs --listen-addr=:26257 --advertise-addr=localhost:26257 --http-addr=:8080

ready=false
for _ in $(seq 1 30); do
  if timeout 10 docker exec crdb-secure cockroach sql --certs-dir=/certs -e 'SELECT 1' >/dev/null 2>&1; then
    ready=true
    break
  fi
  sleep 2
done
if [ "$ready" != true ]; then
  echo "CockroachDB did not become ready" >&2
  docker logs crdb-secure >&2
  exit 1
fi

docker exec -i crdb-secure cockroach sql --certs-dir=/certs <<SQL
CREATE USER ${user} WITH PASSWORD '${password}';
GRANT admin TO ${user};
CREATE DATABASE ${database} OWNER ${user};
SQL
