package cockroachsql

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

const (
	dbNamePrefix     = "tf_tests_db"
	roleNamePrefix   = "tf_tests_role"
	testRolePassword = "testpwd"
)

func closeDB(t *testing.T, db *sql.DB) {
	if err := db.Close(); err != nil {
		t.Fatalf("could not close connection to database: %v", err)
	}
}

// Can be used in a PreCheck function to disable test based on feature.
func testCheckCompatibleVersion(t *testing.T, feature featureName) {
	meta := testAccProvider.Meta()
	if meta == nil {
		// Initialize the provider if it's not yet configured
		config := getTestConfig(t)
		client := config.NewClient(getTestDatabaseName())
		db, err := client.Connect()
		if err != nil {
			t.Fatalf("could connect to database: %v", err)
		}
		if !db.featureSupported(feature) {
			t.Skipf("Skip extension tests for CockroachSQL %s", db.version)
		}
		return
	}
	client := meta.(*Client)
	db, err := client.Connect()
	if err != nil {
		t.Fatalf("could connect to database: %v", err)
	}
	if !db.featureSupported(feature) {
		t.Skipf("Skip extension tests for CockroachSQL %s", db.version)
	}
}

func getTestConfig(t *testing.T) Config {
	getEnv := func(keys []string, fallback string) string {
		for _, key := range keys {
			if value := os.Getenv(key); value != "" {
				return value
			}
		}
		return fallback
	}

	url := getEnv([]string{"COCKROACH_URL"}, "")

	portStr := getEnv([]string{"COCKROACH_PORT"}, "26257")
	dbPort, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("could not cast port value %q as integer: %v", portStr, err)
	}

	sslMode := getEnv([]string{"COCKROACH_SSLMODE"}, "")
	if strings.ToLower(getEnv([]string{"COCKROACH_INSECURE"}, "")) == "true" {
		sslMode = "disable"
	}

	return Config{
		ConnectionURL: url,
		Host:          getEnv([]string{"COCKROACH_HOST"}, "localhost"),
		Port:          dbPort,
		Username:      getEnv([]string{"COCKROACH_USER"}, "root"),
		Password:      getEnv([]string{"COCKROACH_PASSWORD"}, ""),
		SSLMode:       sslMode,
	}
}

func getTestDatabaseName() string {
	if v := os.Getenv("COCKROACH_DATABASE"); v != "" {
		return v
	}
	return "defaultdb"
}

func skipIfNotAcc(t *testing.T) {
	if os.Getenv(resource.EnvTfAcc) == "" {
		t.Skipf("Acceptance tests skipped unless env '%s' set", resource.EnvTfAcc)
	}
}

// dbExecute is a test helper to create a pool, execute one query then close the pool
func dbExecute(t *testing.T, dsn, query string, args ...any) {
	db, err := sql.Open(proxyDriverName, dsn)
	if err != nil {
		t.Fatalf("could to create connection pool: %v", err)
	}
	defer closeDB(t, db)

	// Create the test DB
	if _, err = db.Exec(query, args...); err != nil {
		t.Fatalf("could not execute query %s: %v", query, err)
	}
}

func getTestDBNames(dbSuffix string) (dbName string, roleName string) {
	dbName = fmt.Sprintf("%s_%s", dbNamePrefix, dbSuffix)
	roleName = fmt.Sprintf("%s_%s", roleNamePrefix, dbSuffix)

	return
}

// setupTestDatabase creates all needed resources before executing a terraform test
// and provides the teardown function to delete all these resources.
func setupTestDatabase(t *testing.T, createDB, createRole bool) (string, func()) {
	config := getTestConfig(t)

	suffix := strconv.Itoa(int(time.Now().UnixNano()))

	dbName, roleName := getTestDBNames(suffix)

	if createRole {
		dbExecute(t, config.connStr(getTestDatabaseName()), fmt.Sprintf(
			"CREATE ROLE %s LOGIN",
			roleName,
		))
		time.Sleep(100 * time.Millisecond)
	}

	if createDB {
		dbExecute(t, config.connStr(getTestDatabaseName()), fmt.Sprintf("CREATE DATABASE %s", dbName))
		// Create a test schema in this new database and grant usage to rolName
		dbExecute(t, config.connStr(dbName), "CREATE SCHEMA IF NOT EXISTS test_schema")
		dbExecute(t, config.connStr(dbName), fmt.Sprintf("GRANT usage ON SCHEMA test_schema to %s", roleName))
		dbExecute(t, config.connStr(dbName), "CREATE SCHEMA IF NOT EXISTS dev_schema")
		dbExecute(t, config.connStr(dbName), fmt.Sprintf("GRANT usage ON SCHEMA dev_schema to %s", roleName))
	}

	return suffix, func() {
		dbExecute(t, config.connStr(getTestDatabaseName()), fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
		dbExecute(t, config.connStr(getTestDatabaseName()), fmt.Sprintf("DROP ROLE IF EXISTS %s", roleName))
	}
}

func createTestTables(t *testing.T, dbSuffix string, tables []string, owner string) func() {
	config := getTestConfig(t)
	dbName, _ := getTestDBNames(dbSuffix)
	adminUser := config.getDatabaseUsername()

	db, err := sql.Open(proxyDriverName, config.connStr(dbName))
	if err != nil {
		t.Fatalf("could not open connection pool for db %s: %v", dbName, err)
	}
	defer closeDB(t, db)

	if owner != "" {
		if !config.Superuser {
			if _, err := db.Exec(fmt.Sprintf("GRANT %s TO CURRENT_USER", owner)); err != nil {
				t.Fatalf("could not grant role %s to current user: %v", owner, err)
			}
		}
		if _, err := db.Exec(fmt.Sprintf("SET ROLE %s", owner)); err != nil {
			t.Fatalf("could not set role to %s: %v", owner, err)
		}
	}

	for _, table := range tables {
		if _, err := db.Exec(fmt.Sprintf("CREATE TABLE %s (val text, test_column_one text, test_column_two text)", table)); err != nil {
			t.Fatalf("could not create test table in db %s: %v", dbName, err)
		}
		if owner != "" {
			if _, err := db.Exec(fmt.Sprintf("ALTER TABLE %s OWNER TO %s", table, owner)); err != nil {
				t.Fatalf("could not set test_table owner to %s: %v", owner, err)
			}
		}
	}
	if owner != "" && !config.Superuser {
		if _, err := db.Exec(fmt.Sprintf("SET ROLE %s; REVOKE %s FROM %s", adminUser, owner, adminUser)); err != nil {
			t.Fatalf("could not revoke role %s from %s: %v", owner, adminUser, err)
		}
	}

	// In this case we need to drop table after each test.
	return func() {
		db, err := sql.Open(proxyDriverName, config.connStr(dbName))
		if err != nil {
			t.Fatalf("could not open connection pool for db %s: %v", dbName, err)
		}
		defer closeDB(t, db)

		for _, table := range tables {
			if _, err := db.Exec(fmt.Sprintf("DROP TABLE %s", table)); err != nil {
				t.Fatalf("could not drop test table %s in db %s: %v", table, dbName, err)
			}
		}
	}
}

func createTestSchemas(t *testing.T, dbSuffix string, schemas []string, owner string) func() {
	config := getTestConfig(t)
	dbName, _ := getTestDBNames(dbSuffix)
	adminUser := config.getDatabaseUsername()

	db, err := sql.Open(proxyDriverName, config.connStr(dbName))
	if err != nil {
		t.Fatalf("could not open connection pool for db %s: %v", dbName, err)
	}
	defer closeDB(t, db)

	for _, schema := range schemas {
		if _, err := db.Exec(fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", schema)); err != nil {
			t.Fatalf("could not create test schema in db %s: %v", dbName, err)
		}
		if owner != "" {
			if _, err := db.Exec(fmt.Sprintf("ALTER SCHEMA %s OWNER TO %s", schema, owner)); err != nil {
				t.Fatalf("could not set test schema owner to %s: %v", owner, err)
			}
		}
	}
	if owner != "" && !config.Superuser {
		if _, err := db.Exec(fmt.Sprintf("SET ROLE %s; REVOKE %s FROM %s", adminUser, owner, adminUser)); err != nil {
			t.Fatalf("could not revoke role %s from %s: %v", owner, adminUser, err)
		}
	}

	return func() {
		db, err := sql.Open(proxyDriverName, config.connStr(dbName))
		if err != nil {
			t.Fatalf("could not open connection pool for db %s: %v", dbName, err)
		}
		defer closeDB(t, db)

		for _, schema := range schemas {
			if _, err := db.Exec(fmt.Sprintf("DROP SCHEMA %s CASCADE", schema)); err != nil {
				t.Fatalf("could not drop schema %s: %v", schema, err)
			}
		}
	}
}

func createTestSequences(t *testing.T, dbSuffix string, sequences []string, owner string) func() {
	config := getTestConfig(t)
	dbName, _ := getTestDBNames(dbSuffix)
	adminUser := config.getDatabaseUsername()

	db, err := sql.Open(proxyDriverName, config.connStr(dbName))
	if err != nil {
		t.Fatalf("could not open connection pool for db %s: %v", dbName, err)
	}
	defer closeDB(t, db)

	for _, sequence := range sequences {
		if _, err := db.Exec(fmt.Sprintf("CREATE sequence %s", sequence)); err != nil {
			t.Fatalf("could not create test sequence in db %s: %v", dbName, err)
		}
		if owner != "" {
			if _, err := db.Exec(fmt.Sprintf("ALTER sequence %s OWNER TO %s", sequence, owner)); err != nil {
				t.Fatalf("could not set test_sequence owner to %s: %v", owner, err)
			}
		}
	}
	if owner != "" && !config.Superuser {
		if _, err := db.Exec(fmt.Sprintf("SET ROLE %s; REVOKE %s FROM %s", adminUser, owner, adminUser)); err != nil {
			t.Fatalf("could not revoke role %s from %s: %v", owner, adminUser, err)
		}
	}

	return func() {
		db, err := sql.Open(proxyDriverName, config.connStr(dbName))
		if err != nil {
			t.Fatalf("could not open connection pool for db %s: %v", dbName, err)
		}
		defer closeDB(t, db)

		for _, sequence := range sequences {
			if _, err := db.Exec(fmt.Sprintf("DROP sequence %s", sequence)); err != nil {
				t.Fatalf("could not drop sequence %s: %v", sequence, err)
			}
		}
	}
}

func connectAsTestRole(t *testing.T, roleName string, dbName string) *sql.DB {
	config := getTestConfig(t)
	config.Username = roleName
	config.Password = ""

	db, err := sql.Open(proxyDriverName, config.connStr(dbName))
	if err != nil {
		t.Fatalf("could not open connection pool for role %s on db %s: %v", roleName, dbName, err)
	}

	return db
}

func testHasGrantForQuery(db *sql.DB, query string, expected bool) error {
	_, err := db.Exec(query)

	if expected && err != nil {
		return fmt.Errorf("could not execute query '%s' as expected: %w", query, err)
	}

	if !expected && err == nil {
		return fmt.Errorf("did not fail as expected when executing query '%s'", query)
	}

	return nil
}

func testCheckTablesPrivileges(t *testing.T, dbName, roleName string, tables []string, allowedPrivileges []string) error {
	db := connectAsTestRole(t, roleName, dbName)
	defer closeDB(t, db)

	for _, table := range tables {
		for _, privilege := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE", "REFERENCES", "TRIGGER"} {
			expected := sliceContainsStr(allowedPrivileges, privilege)
			var query string
			switch privilege {
			case "SELECT":
				query = fmt.Sprintf("SELECT count(*) FROM %s", table)
			case "INSERT":
				query = fmt.Sprintf("INSERT INTO %s VALUES ('test')", table)
			case "UPDATE":
				query = fmt.Sprintf("UPDATE %s SET val = 'test'", table)
			case "DELETE":
				query = fmt.Sprintf("DELETE FROM %s", table)
			case "TRUNCATE":
				query = fmt.Sprintf("TRUNCATE TABLE %s", table)
			case "REFERENCES":
				query = fmt.Sprintf("CREATE TABLE test_refs (id text REFERENCES %s(val))", table)
				defer func() {
					c := getTestConfig(t)
					dbExecute(t, c.connStr(dbName), "DROP TABLE IF EXISTS test_refs")
				}()
			case "TRIGGER":
				query = fmt.Sprintf("CREATE TRIGGER test_trigger BEFORE INSERT ON %s FOR EACH ROW EXECUTE PROCEDURE dummy()", table)
			}

			if err := testHasGrantForQuery(db, query, expected); err != nil {
				return fmt.Errorf("Check for privilege %s on table %s failed: %w", privilege, table, err)
			}
		}
	}

	return nil
}

func testCheckSchemasPrivileges(t *testing.T, dbName, roleName string, schemas []string, allowedPrivileges []string) error {
	db := connectAsTestRole(t, roleName, dbName)
	defer closeDB(t, db)

	for _, schema := range schemas {
		queries := map[string]string{
			"USAGE":  fmt.Sprintf("SELECT 1 FROM %s.any_table", schema),
			"CREATE": fmt.Sprintf("CREATE TABLE %s.test_table()", schema),
		}

		for queryType, query := range queries {
			expected := sliceContainsStr(allowedPrivileges, queryType)
			_ = testHasGrantForQuery(db, query, expected)
		}
	}
	return nil
}

func testCheckSchemaPrivileges(t *testing.T, role, dbName, schemaName string, usage, create bool) func(*terraform.State) error {
	return func(*terraform.State) error {
		db := connectAsTestRole(t, role, dbName)
		defer closeDB(t, db)

		if usage {
			// USAGE on schema allows looking up objects
			if _, err := db.Exec(fmt.Sprintf("SELECT 1 FROM %s.any_table", schemaName)); err != nil {
				// Ignore if table doesn't exist, we just check for permission denied
				if strings.Contains(err.Error(), "permission denied") {
					return err
				}
			}
		}

		if create {
			if _, err := db.Exec(fmt.Sprintf("CREATE TABLE %s.test_create (id int)", schemaName)); err != nil {
				return err
			}
			defer func() {
				c := getTestConfig(t)
				dbExecute(t, c.connStr(dbName), fmt.Sprintf("DROP TABLE IF EXISTS %s.test_create", schemaName))
			}()
		}
		return nil
	}
}
