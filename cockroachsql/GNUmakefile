# env TESTARGS='-run TestAccCockroachSQLSchema_AddPolicy' TF_LOG=warn make test
#
# NOTE: As of CockroachDB 9.6.1 the -test.parallel=1 is required when
# performing `DROP ROLE`-related actions.  This behavior and requirement
# may change in the future and is likely not required when doing
# non-delete related operations. But for now it is.

CRDBVERSION?=v23.2.11
COCKROACH?=$(wildcard /usr/local/bin/cockroach)
PSQL?=$(wildcard /usr/local/bin/psql)

PGDATA?=$(GOPATH)/src/github.com/nellisauction/terraform-provider-cockroachsql/data

initdb::
	-cat /dev/urandom | strings | grep -o '[[:alnum:]]' | head -n 32 | tr -d '\n' > pwfile
	$(COCKROACH) start-single-node --insecure --store=$(PGDATA) --background

startdb::
	$(COCKROACH) start-single-node --insecure --store=$(PGDATA) --background
	| tee cockroachdb.log

cleandb::
	rm -rf $(PGDATA)
	rm -f pwfile

freshdb:: cleandb initdb

test::
	2>&1 PGSSLMODE=disable PGHOST=localhost PGPORT=26257 PGUSER=root make -C ../ testacc TEST=./cockroachsql | tee test.log

psql::
	$(PSQL) -h localhost -p 26257 -d defaultdb -u root
