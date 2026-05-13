# Terraform Provider for CockroachSQL - Agent Instructions

This file serves as the core context and instruction set for OpenCode agents (and other AI assistants) working on the `terraform-provider-cockroachsql` project. It documents architectural decisions, testing rules, and known pitfalls to prevent regressions.

## 1. Core Identity & Compatibility
- **Namespace:** The provider is `cockroachsql`. The binary is `terraform-provider-cockroachsql`.
- **Target:** This provider manages CockroachDB SQL objects (databases, roles, schemas, grants, default privileges, etc.).
- **Baseline Version:** Maintain compatibility down to **CockroachDB v23.2.0 (LTS)**.

## 2. PostgreSQL "Fork-Isms" to Avoid
This provider was forked from the PostgreSQL provider but heavily modified. CockroachDB is wire-compatible with PostgreSQL but diverges in key SQL syntax and behavior:
- **`DROP DATABASE`**: CockroachDB does *not* support `DROP DATABASE ... WITH (FORCE)`. Executing this will cause a syntax error.
- **`ALTER ROLE RENAME TO`**: CockroachDB does *not* support renaming roles. In Terraform schemas, the `name` attribute for roles MUST have `ForceNew: true`.
- **Unsupported Keywords**: Do not use `CONNECTION LIMIT` or `ENCRYPTED` passwords in role/database creation.
- **Unsupported Features**: Avoid Foreign Data Wrappers (FDW), Logical Replication, or `PROCEDURE`/`ROUTINE` object types if testing against older CockroachDB versions.

## 3. Grants & Privileges (Crucial)
- **`SHOW GRANTS` vs `pg_catalog`**: Always use CockroachDB native `SHOW GRANTS ON ...` or `SHOW DEFAULT PRIVILEGES FOR ...` to read grants. Direct queries to `pg_catalog` tables (like `pg_namespace`, `pg_class`, or ACL explosion functions) are brittle, frequently fail, and can cause state drift.
- **Implicit Privileges**: CockroachDB sometimes aggregates privileges implicitly. For example, `ALL` privileges on a function inherently include `EXECUTE`. Similarly, `PUBLIC` has certain implicit execution rights on functions. Make sure the `Read` functions correctly parse and merge these privileges to prevent Terraform from detecting false drift and attempting constant updates.
- **Signatures**: When granting on functions, handle type normalization (e.g. `character` vs `char`) robustly when comparing `SHOW GRANTS` output to the Terraform state.

## 4. Test Infrastructure
- **Ephemeral Databases**: **Never modify `defaultdb`** during acceptance tests. Use the provided environment variable `COCKROACH_DATABASE` (e.g., `tf_tests`) to scope the creation of test databases/roles.
- **Containerization**: Both local development and CI use the `.devcontainer/` setup exclusively. The devcontainer's `docker-compose.yml` starts a `cockroachdb` service, and `postStartCommand` in `devcontainer.json` creates the `tf_tests` database on startup. Do NOT write custom scripts to start Docker Compose or create the database manually.
- **Unit vs Acceptance Tests**: `make test` runs only non-acceptance unit tests (uses `-run "^Test[^A]"` with a 30s timeout). This keeps it fast and safe even when `TF_ACC=true` is set in the environment. Acceptance tests (`TestAcc*`) require a running database and are executed via `make testacc` (which runs `go test -v ./cockroachsql -timeout 120m`).
- **CI**: GitHub Actions uses `devcontainers/ci@v0.3` to run all tests inside the devcontainer. A `build-devcontainer` job pre-warms the Blacksmith Docker layer cache before the 9-version CockroachDB matrix fans out. The matrix version is passed to the devcontainer via the `CRDBVERSION` env var, which `.devcontainer/docker-compose.yml` uses to select the CockroachDB image (`${CRDBVERSION:-latest-v24.1}`).

## 5. Environment Variables
The provider operates strictly via `COCKROACH_*` variables.
- Required for testing: `COCKROACH_HOST`, `COCKROACH_PORT` (default 26257), `COCKROACH_USER`, `COCKROACH_PASSWORD`, `COCKROACH_DATABASE`, `COCKROACH_INSECURE=true`, and `TF_ACC=true`.
- Inside the devcontainer (local and CI), `COCKROACH_HOST=cockroachdb` (the Docker Compose service name).
- **Do not use** legacy `PG*` environment variables for provider configuration.

## 6. Connection Pool Leakage
- **The Problem:** Parallel acceptance tests may close the database connection within a test body, but the provider's `dbRegistry` caches the closed pointer, causing subsequent tests to panic with `sql: database is closed`.
- **The Fix:** The `Connect()` method in `cockroachsql/config.go` MUST check the health of cached connections using `db.Ping()`. If `Ping()` fails, the connection must be evicted from the registry and recreated.

## 7. Toolchain & Pre-commit Hooks
- **Lefthook**: Never run `LEFTHOOK=0 git commit` to bypass hooks.
- **ASDF Go Mismatch**: If `lefthook` fails during `go vet` or `go build` complaining about Go version mismatches (e.g., `go1.24.9 does not match go tool version 1.25.8`), this is a local `asdf` shim desync.
- **Fixing ASDF**: To fix the desync, instruct the user to run `exec $SHELL` or run `source ~/.asdf/plugins/golang/set-env.bash` before executing Go commands in the automated agent shell.

## 8. Role Expiration State Drift
- When a role does not have an expiration, CockroachDB returns `NULL` for `valid_until`. The Terraform `Read` logic must translate this `NULL` into an empty string `""` to match an unset configuration, preventing continuous "infinity" diffs during Terraform plans.
- During `Update`, explicitly sending an empty `valid_until` should trigger `VALID UNTIL 'infinity'` in the SQL command to properly clear a previously set expiration.
