package cockroachsql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func TestSplitImportList(t *testing.T) {
	tests := map[string][]any{
		"":                   {},
		"a":                  {"a"},
		"a,b":                {"a", "b"},
		"f(int, text),g, h ": {"f(int, text)", "g", "h"},
	}
	for in, want := range tests {
		if got := splitImportList(in); !reflect.DeepEqual(got, want) {
			t.Errorf("splitImportList(%q) = %#v, want %#v", in, got, want)
		}
	}
}

func TestParseRoleOptions(t *testing.T) {
	tests := map[string][]string{
		"":                                   {},
		"{}":                                 {},
		"NOLOGIN":                            {"NOLOGIN"},
		"CREATEROLE, NOLOGIN, VALID UNTIL=x": {"CREATEROLE", "NOLOGIN", "VALID UNTIL"},
		`{CREATEROLE,"NOLOGIN"}`:             {"CREATEROLE", "NOLOGIN"},
	}
	for in, want := range tests {
		got := parseRoleOptions(in)
		if len(got) != len(want) {
			t.Errorf("parseRoleOptions(%q) = %v, want %v", in, got, want)
			continue
		}
		for _, w := range want {
			if !got[w] {
				t.Errorf("parseRoleOptions(%q) missing %s", in, w)
			}
		}
	}
}

func TestAccCockroachSQLGrant_DDLTransactionIsAtomic(t *testing.T) {
	skipIfNotAcc(t)
	testAccPreCheck(t)

	config := getTestConfig(t)
	dsn := config.connStr(getTestDatabaseName())
	dbExecute(t, dsn, "CREATE SCHEMA IF NOT EXISTS atomic_schema")
	dbExecute(t, dsn, "CREATE TABLE IF NOT EXISTS atomic_schema.t (id INT PRIMARY KEY)")
	dbExecute(t, dsn, "CREATE ROLE IF NOT EXISTS atomic_role")
	dbExecute(t, dsn, "GRANT SELECT ON TABLE atomic_schema.t TO atomic_role")
	defer func() {
		dbExecute(t, dsn, "DROP SCHEMA atomic_schema CASCADE")
		dbExecute(t, dsn, "DROP ROLE atomic_role")
	}()

	db, err := testAccProvider.Meta().(*Client).Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	errBoom := errors.New("boom")
	err = withDDLTransaction(context.Background(), db.DB, func(tx QueryAble) error {
		if _, err := tx.ExecContext(context.Background(), "REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA atomic_schema FROM atomic_role"); err != nil {
			return err
		}
		return errBoom
	})
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}

	conn, err := sql.Open(proxyDriverName, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	var count int
	if err := conn.QueryRow("SELECT count(*) FROM [SHOW GRANTS ON TABLE atomic_schema.t] WHERE grantee = 'atomic_role'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("revoke was not rolled back: atomic_role has %d grants on atomic_schema.t", count)
	}
}

func TestAccCockroachSQLGrant_NoImplicitSchema(t *testing.T) {
	skipIfNotAcc(t)
	config := getTestConfig(t)
	dsn := config.connStr(getTestDatabaseName())
	dbExecute(t, dsn, "CREATE ROLE IF NOT EXISTS no_schema_role")
	defer dbExecute(t, dsn, "DROP ROLE IF EXISTS no_schema_role")

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "cockroachsql_grant" "test" {
  database    = %q
  role        = "no_schema_role"
  schema      = "schema_that_does_not_exist"
  object_type = "table"
  privileges  = ["SELECT"]
}`, getTestDatabaseName()),
				ExpectError: regexp.MustCompile(`schema_that_does_not_exist`),
			},
		},
		CheckDestroy: func(*terraform.State) error {
			conn, err := sql.Open(proxyDriverName, dsn)
			if err != nil {
				return err
			}
			defer func() { _ = conn.Close() }()
			var exists bool
			if err := conn.QueryRow("SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = 'schema_that_does_not_exist')").Scan(&exists); err != nil {
				return err
			}
			if exists {
				return fmt.Errorf("cockroachsql_grant created schema_that_does_not_exist")
			}
			return nil
		},
	})
}

func TestAccCockroachSQLGrant_WithGrantOptionDriftAndImport(t *testing.T) {
	skipIfNotAcc(t)
	config := getTestConfig(t)
	dsn := config.connStr(getTestDatabaseName())
	dbExecute(t, dsn, "CREATE SCHEMA IF NOT EXISTS wgo_schema")
	dbExecute(t, dsn, "CREATE TABLE IF NOT EXISTS wgo_schema.t (id INT PRIMARY KEY)")
	dbExecute(t, dsn, "CREATE ROLE IF NOT EXISTS wgo_role")
	defer func() {
		dbExecute(t, dsn, "DROP SCHEMA wgo_schema CASCADE")
		dbExecute(t, dsn, "DROP ROLE wgo_role")
	}()

	tfConfig := func(withGrantOption bool) string {
		return fmt.Sprintf(`
resource "cockroachsql_grant" "test" {
  database          = %q
  role              = "wgo_role"
  schema            = "wgo_schema"
  object_type       = "table"
  objects           = ["t"]
  privileges        = ["SELECT"]
  with_grant_option = %t
}`, getTestDatabaseName(), withGrantOption)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: tfConfig(false),
				Check:  resource.TestCheckResourceAttr("cockroachsql_grant.test", "with_grant_option", "false"),
			},
			{
				PreConfig: func() {
					dbExecute(t, dsn, "GRANT SELECT ON TABLE wgo_schema.t TO wgo_role WITH GRANT OPTION")
				},
				Config:             tfConfig(false),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: tfConfig(false),
				Check: func(*terraform.State) error {
					conn, err := sql.Open(proxyDriverName, dsn)
					if err != nil {
						return err
					}
					defer func() { _ = conn.Close() }()
					var grantable bool
					if err := conn.QueryRow("SELECT bool_or(is_grantable) FROM [SHOW GRANTS ON TABLE wgo_schema.t] WHERE grantee = 'wgo_role'").Scan(&grantable); err != nil {
						return err
					}
					if grantable {
						return fmt.Errorf("grant option was not removed")
					}
					return nil
				},
			},
			{
				Config: tfConfig(true),
				Check:  resource.TestCheckResourceAttr("cockroachsql_grant.test", "with_grant_option", "true"),
			},
			{
				ResourceName:      "cockroachsql_grant.test",
				ImportState:       true,
				ImportStateId:     fmt.Sprintf("wgo_role|%s|table|wgo_schema|t|", getTestDatabaseName()),
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccCockroachSQLRole_CoexistsWithGrantRole(t *testing.T) {
	cfg := func(login bool) string {
		return fmt.Sprintf(`
resource "cockroachsql_role" "parent_a" { name = "coexist_parent_a" }
resource "cockroachsql_role" "parent_b" { name = "coexist_parent_b" }

resource "cockroachsql_role" "member" {
  name  = "coexist_member"
  login = %t
  roles = [cockroachsql_role.parent_a.name]
}

resource "cockroachsql_grant_role" "extra" {
  role       = cockroachsql_role.member.name
  grant_role = cockroachsql_role.parent_b.name
}`, login)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		CheckDestroy:      testAccCheckCockroachSQLRoleDestroy,
		Steps: []resource.TestStep{
			{
				Config: cfg(false),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cockroachsql_role.member", "roles.#", "1"),
					testAccCheckCockroachSQLRoleExists("coexist_member", []string{"coexist_parent_a", "coexist_parent_b"}, nil),
				),
			},
			{
				Config:   cfg(false),
				PlanOnly: true,
			},
			{
				Config: cfg(true),
				Check:  testAccCheckCockroachSQLRoleExists("coexist_member", []string{"coexist_parent_a", "coexist_parent_b"}, nil),
			},
			{
				ResourceName:      "cockroachsql_grant_role.extra",
				ImportState:       true,
				ImportStateId:     "coexist_member|coexist_parent_b",
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccCockroachSQLDefaultPrivileges_Import(t *testing.T) {
	skipIfNotAcc(t)
	config := getTestConfig(t)
	dsn := config.connStr(getTestDatabaseName())
	dbExecute(t, dsn, "CREATE ROLE IF NOT EXISTS dp_import_owner")
	dbExecute(t, dsn, "CREATE ROLE IF NOT EXISTS dp_import_role")
	defer func() {
		dbExecute(t, dsn, "DROP OWNED BY dp_import_role")
		dbExecute(t, dsn, "DROP OWNED BY dp_import_owner")
		dbExecute(t, dsn, "DROP ROLE IF EXISTS dp_import_role")
		dbExecute(t, dsn, "DROP ROLE IF EXISTS dp_import_owner")
	}()

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "cockroachsql_default_privileges" "test" {
  database          = %q
  role              = "dp_import_role"
  owner             = "dp_import_owner"
  schema            = "public"
  object_type       = "table"
  privileges        = ["SELECT"]
  with_grant_option = true
}`, getTestDatabaseName()),
				Check: resource.TestCheckResourceAttr("cockroachsql_default_privileges.test", "with_grant_option", "true"),
			},
			{
				ResourceName:      "cockroachsql_default_privileges.test",
				ImportState:       true,
				ImportStateId:     fmt.Sprintf("dp_import_role|%s|table|dp_import_owner|public", getTestDatabaseName()),
				ImportStateVerify: true,
			},
		},
	})
}
