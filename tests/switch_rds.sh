#!/bin/bash

HERE=$(dirname $(readlink -f ${BASH_SOURCE:-${(%):-%N}}))

source "$HERE/switch_superuser.sh"

echo "Switching to an RDS-like environment (on CockroachDB)"
psql -d defaultdb  > /dev/null <<EOS
    CREATE role rds WITH LOGIN CREATEROLE PASSWORD 'rds';
    GRANT ALL ON DATABASE defaultdb TO rds;
    ALTER DATABASE defaultdb OWNER TO rds;
    ALTER SCHEMA public OWNER TO rds;
EOS

export TF_ACC=true
export COCKROACH_HOST=localhost
export COCKROACH_PORT=26257
export COCKROACH_USER=rds
export COCKROACH_PASSWORD=rds
export COCKROACH_INSECURE=true
export COCKROACH_SUPERUSER=false
export PGHOST=localhost
export PGPORT=26257
export PGUSER=rds
export PGPASSWORD=rds
export PGSSLMODE=disable
export PGSUPERUSER=false
