package cockroachsql

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func TestParseCRDBDurationMs(t *testing.T) {
	tests := map[string]int{
		"":       0,
		"0":      0,
		"90000":  90000,
		"250ms":  250,
		"30s":    30000,
		"1.5s":   1500,
		"1m30s":  90000,
		"2h":     7200000,
		"1h0m0s": 3600000,
	}
	for in, want := range tests {
		got, err := parseCRDBDurationMs(in)
		if err != nil {
			t.Errorf("parseCRDBDurationMs(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("parseCRDBDurationMs(%q) = %d, want %d", in, got, want)
		}
	}

	if _, err := parseCRDBDurationMs("soon"); err == nil {
		t.Error("expected an error for an invalid duration")
	}
}

func TestPqQuoteLiteral(t *testing.T) {
	tests := map[string]string{
		`plain`:    `plain`,
		`it's`:     `it''s`,
		`back\sl`:  `back\sl`,
		`\'mixed'`: `\''mixed''`,
	}
	for in, want := range tests {
		if got := pqQuoteLiteral(in); got != want {
			t.Errorf("pqQuoteLiteral(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAccCockroachSQLRole_Timeouts(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		CheckDestroy:      testAccCheckCockroachSQLRoleDestroy,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cockroachsql_role" "timeouts" {
  name                                = "timeouts_role"
  statement_timeout                   = 90000
  idle_in_transaction_session_timeout = 1500
}`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cockroachsql_role.timeouts", "statement_timeout", "90000"),
					resource.TestCheckResourceAttr("cockroachsql_role.timeouts", "idle_in_transaction_session_timeout", "1500"),
				),
			},
		},
	})
}

func TestAccCockroachSQLRole_DropOwnedInOtherDatabases(t *testing.T) {
	const roleName = "xdb_owner_role"
	// Hostile name: must not be able to inject connection options when the
	// provider connects to each database during role deletion.
	const dbName = "tf_tests_xdb?host=evil.invalid&sslmode=disable#x"

	config := getTestConfig(t)
	defer func() {
		if os.Getenv(resource.EnvTfAcc) != "" {
			dbExecute(t, config.connStr(getTestDatabaseName()), fmt.Sprintf("DROP DATABASE IF EXISTS %s CASCADE", pqQuoteIdent(dbName)))
		}
	}()

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		CheckDestroy: func(s *terraform.State) error {
			if err := testAccCheckCockroachSQLRoleDestroy(s); err != nil {
				return err
			}
			db, err := sql.Open(proxyDriverName, config.connStr(dbName))
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()
			var owner string
			if err := db.QueryRow("SELECT tableowner FROM pg_tables WHERE schemaname = 'public' AND tablename = 'owned_by_role'").Scan(&owner); err != nil {
				return fmt.Errorf("table owned by the deleted role was not preserved: %w", err)
			}
			if owner != config.getDatabaseUsername() {
				return fmt.Errorf("owned_by_role is owned by %q, want %q", owner, config.getDatabaseUsername())
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`resource "cockroachsql_role" "owner" { name = %q }`, roleName),
				Check: func(*terraform.State) error {
					adminDSN := config.connStr(getTestDatabaseName())
					dbExecute(t, adminDSN, fmt.Sprintf("CREATE DATABASE %s OWNER %s", pqQuoteIdent(dbName), roleName))
					dbExecute(t, adminDSN, fmt.Sprintf("ALTER DATABASE %s OWNER TO %s", pqQuoteIdent(dbName), pqQuoteIdent(config.getDatabaseUsername())))
					dbExecute(t, config.connStr(dbName), fmt.Sprintf("GRANT CREATE ON SCHEMA public TO %s", roleName))
					dbExecute(t, config.connStr(dbName), "CREATE TABLE owned_by_role (id INT PRIMARY KEY)")
					dbExecute(t, config.connStr(dbName), fmt.Sprintf("ALTER TABLE owned_by_role OWNER TO %s", roleName))
					return nil
				},
			},
		},
	})
}

func TestAccCockroachSQLRole_WithRolesGrantedKeepsExistingMembership(t *testing.T) {
	skipIfNotAcc(t)
	testAccPreCheck(t)

	const roleName = "tf_tests_preexisting_member"
	config := getTestConfig(t)
	adminDSN := config.connStr(getTestDatabaseName())
	currentUser := pqQuoteIdent(config.getDatabaseUsername())
	dbExecute(t, adminDSN, fmt.Sprintf("CREATE ROLE IF NOT EXISTS %s", roleName))
	defer dbExecute(t, adminDSN, fmt.Sprintf("DROP ROLE IF EXISTS %s", roleName))
	dbExecute(t, adminDSN, fmt.Sprintf("GRANT %s TO %s", roleName, currentUser))
	defer dbExecute(t, adminDSN, fmt.Sprintf("REVOKE %s FROM %s", roleName, currentUser))

	db, err := testAccProvider.Meta().(*Client).Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := withRolesGranted(context.Background(), db, []string{roleName}, func() error { return nil }); err != nil {
		t.Fatal(err)
	}

	isMember, err := isMemberOfRole(context.Background(), db, roleName, config.getDatabaseUsername())
	if err != nil {
		t.Fatal(err)
	}
	if !isMember {
		t.Fatalf("withRolesGranted revoked %s's pre-existing membership in %s", config.getDatabaseUsername(), roleName)
	}
}

func TestAccCockroachSQLRole_Password(t *testing.T) {
	if strings.EqualFold(os.Getenv("COCKROACH_INSECURE"), "true") {
		t.Skip("CockroachDB does not accept passwords in insecure mode")
	}

	const roleName = "password_role"
	const password = `pa\ss'word`

	roleConfig := fmt.Sprintf(`
resource "cockroachsql_role" "pw" {
  name     = %q
  login    = true
  password = %q
}`, roleName, password)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		CheckDestroy:      testAccCheckCockroachSQLRoleDestroy,
		Steps: []resource.TestStep{
			{
				Config: roleConfig,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cockroachsql_role.pw", "password", password),
					testAccCheckRoleCanLogin(t, roleName, password),
				),
			},
			{
				Config:   roleConfig,
				PlanOnly: true,
			},
		},
	})
}

func testAccCheckRoleCanLogin(t *testing.T, roleName, password string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		config := getTestConfig(t)
		config.ConnectionURL = ""
		config.Username = roleName
		config.Password = password
		dsn := config.connStr(getTestDatabaseName())
		if _, err := url.Parse(dsn); err != nil {
			return err
		}
		db, err := sql.Open(proxyDriverName, dsn)
		if err != nil {
			return err
		}
		defer func() { _ = db.Close() }()
		if err := db.Ping(); err != nil {
			return fmt.Errorf("role %s could not log in with its configured password: %w", roleName, err)
		}
		return nil
	}
}

func pqQuoteIdent(s string) string {
	return fmt.Sprintf(`"%s"`, strings.ReplaceAll(s, `"`, `""`))
}
