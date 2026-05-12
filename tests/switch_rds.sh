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
export PGHOST=localhost
export PGPORT=26257
export PGUSER=rds
export PGPASSWORD=rds
export PGSSLMODE=disable
export PGSUPERUSER=false
