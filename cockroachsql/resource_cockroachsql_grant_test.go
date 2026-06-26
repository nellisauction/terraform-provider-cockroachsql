package cockroachsql

import (
	"fmt"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func TestAccCockroachSQLGrantFunction(t *testing.T) {
	skipIfNotAcc(t)

	config := getTestConfig(t)
	dsn := config.connStr(getTestDatabaseName())

	dbExecute(t, dsn, "CREATE SCHEMA IF NOT EXISTS test_schema")
	dbExecute(t, dsn, "CREATE ROLE test_role LOGIN")
	dbExecute(t, dsn, "GRANT USAGE ON SCHEMA test_schema TO test_role")

	dbExecute(t, dsn, `
CREATE FUNCTION test_schema.test_func_simple() RETURNS text
	AS $$ select 'foo'::text $$
    LANGUAGE SQL;
`)
	defer func() {
		dbExecute(t, dsn, "DROP SCHEMA test_schema CASCADE")
		dbExecute(t, dsn, "DROP ROLE test_role")
	}()

	for _, role := range []string{"test_role", "public"} {
		t.Run(role, func(t *testing.T) {

			tfConfig := fmt.Sprintf(`
resource cockroachsql_grant "test" {
  database    = "%s"
  role        = "%s"
  schema      = "test_schema"
  object_type = "function"
  objects     = ["test_func_simple"]
  privileges  = ["EXECUTE"]
}
	`, getTestDatabaseName(), role)

			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testCheckCompatibleVersion(t, featurePrivileges)
				},
				Providers: testAccProviders,
				Steps: []resource.TestStep{
					{
						Config: tfConfig,
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttrSet("cockroachsql_grant.test", "id"),
							resource.TestCheckResourceAttr("cockroachsql_grant.test", "privileges.#", "1"),
						),
					},
				},
			})
		})
	}
}

func TestAccCockroachSQLGrantFunctionWithArgs(t *testing.T) {
	skipIfNotAcc(t)

	config := getTestConfig(t)
	dsn := config.connStr(getTestDatabaseName())

	dbExecute(t, dsn, "CREATE SCHEMA IF NOT EXISTS test_schema")
	dbExecute(t, dsn, "CREATE ROLE test_role LOGIN")
	dbExecute(t, dsn, "GRANT USAGE ON SCHEMA test_schema TO test_role")

	dbExecute(t, dsn, `
CREATE FUNCTION test_schema.test_with_args(arg1 text, arg2 character) RETURNS text
	AS $$ select 'foo'::text $$
    LANGUAGE SQL;
`)
	defer func() {
		dbExecute(t, dsn, "DROP SCHEMA test_schema CASCADE")
		dbExecute(t, dsn, "DROP ROLE test_role")
	}()

	for _, role := range []string{"test_role", "public"} {
		t.Run(role, func(t *testing.T) {

			tfConfig := fmt.Sprintf(`
resource cockroachsql_grant "test" {
  database    = "%s"
  role        = "%s"
  schema      = "test_schema"
  object_type = "function"
  privileges  = ["EXECUTE"]
  objects 	  = ["test_with_args(text, char)"]
}
	`, getTestDatabaseName(), role)

			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testCheckCompatibleVersion(t, featurePrivileges)
				},
				Providers: testAccProviders,
				Steps: []resource.TestStep{
					{
						Config: tfConfig,
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttrSet("cockroachsql_grant.test", "id"),
							resource.TestCheckResourceAttr("cockroachsql_grant.test", "privileges.#", "1"),
						),
					},
				},
			})
		})
	}
}

func TestAccCockroachSQLGrantProcedure(t *testing.T) {
	skipIfNotAcc(t)

	config := getTestConfig(t)
	dsn := config.connStr(getTestDatabaseName())

	dbExecute(t, dsn, "CREATE SCHEMA IF NOT EXISTS test_schema")
	dbExecute(t, dsn, "CREATE ROLE test_role LOGIN")
	dbExecute(t, dsn, "GRANT USAGE ON SCHEMA test_schema TO test_role")

	dbExecute(t, dsn, `
CREATE PROCEDURE test_schema.test_proc()
	AS $$ select 'foo'::text $$
    LANGUAGE SQL;
`)
	defer func() {
		dbExecute(t, dsn, "DROP SCHEMA test_schema CASCADE")
		dbExecute(t, dsn, "DROP ROLE test_role")
	}()

	for _, role := range []string{"test_role", "public"} {
		t.Run(role, func(t *testing.T) {

			tfConfig := fmt.Sprintf(`
resource cockroachsql_grant "test" {
  database    = "%s"
  role        = "%s"
  schema      = "test_schema"
  object_type = "procedure"
  privileges  = ["EXECUTE"]
}
	`, getTestDatabaseName(), role)

			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testCheckCompatibleVersion(t, featureProcedure)
				},
				Providers: testAccProviders,
				Steps: []resource.TestStep{
					{
						Config: tfConfig,
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttrSet("cockroachsql_grant.test", "id"),
							resource.TestCheckResourceAttr("cockroachsql_grant.test", "privileges.#", "1"),
						),
					},
				},
			})
		})
	}
}

func TestAccCockroachSQLGrantRoutine(t *testing.T) {
	skipIfNotAcc(t)

	config := getTestConfig(t)
	dsn := config.connStr(getTestDatabaseName())

	dbExecute(t, dsn, "CREATE SCHEMA IF NOT EXISTS test_schema")
	dbExecute(t, dsn, "CREATE ROLE test_role LOGIN")
	dbExecute(t, dsn, "GRANT USAGE ON SCHEMA test_schema TO test_role")

	dbExecute(t, dsn, `
CREATE FUNCTION test_schema.test_function() RETURNS text
	AS $$ select 'foo'::text $$
    LANGUAGE SQL;
`)
	dbExecute(t, dsn, `
CREATE PROCEDURE test_schema.test_procedure()
	AS $$ select 'foo'::text $$
    LANGUAGE SQL;
`)
	defer func() {
		dbExecute(t, dsn, "DROP SCHEMA test_schema CASCADE")
		dbExecute(t, dsn, "DROP ROLE test_role")
	}()

	for _, role := range []string{"test_role", "public"} {
		t.Run(role, func(t *testing.T) {

			tfConfigRoutine := fmt.Sprintf(`
resource cockroachsql_grant "test" {
  database    = "%s"
  role        = "%s"
  schema      = "test_schema"
  object_type = "routine"
  privileges  = ["EXECUTE"]
}
	`, getTestDatabaseName(), role)

			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testCheckCompatibleVersion(t, featureRoutine)
				},
				Providers: testAccProviders,
				Steps: []resource.TestStep{
					{
						Config: tfConfigRoutine,
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttrSet("cockroachsql_grant.test", "id"),
							resource.TestCheckResourceAttr("cockroachsql_grant.test", "privileges.#", "1"),
						),
					},
				},
			})
		})
	}
}

func TestAccCockroachSQLGrantDatabase(t *testing.T) {
	config := fmt.Sprintf(`
resource "cockroachsql_role" "test" {
	name     = "test_grant_role"
}

resource "cockroachsql_database" "test_db" {
	name = "test_grant_db_%d"
}

resource "cockroachsql_grant" "test" {
	database    = cockroachsql_database.test_db.name
	role        = cockroachsql_role.test.name
	object_type = "database"
	privileges  = %%s
}
`, time.Now().UnixNano())

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePrivileges)
		},
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(config, `["CONNECT"]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("cockroachsql_grant.test", "id"),
					resource.TestCheckResourceAttr("cockroachsql_grant.test", "privileges.#", "1"),
				),
			},
		},
	})
}

func TestAccCockroachSQLImplicitGrants(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	dbName, roleName := getTestDBNames(dbSuffix)

	// GRANT SELECT ON ALL TABLES IN SCHEMA only produces rows in SHOW GRANTS when
	// the schema contains at least one table; create one so the read-back is non-empty.
	dropTable := createTestTables(t, dbSuffix, []string{"test_schema.implicit_test_table"}, "")
	defer dropTable()

	var tfConfig = fmt.Sprintf(`
	resource "cockroachsql_grant" "test" {
		database    = "%%s"
		role        = "%%s"
		schema      = "test_schema"
		object_type = "table"
		privileges  = ["SELECT"]
	}`)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePrivileges)
		},
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(tfConfig, dbName, roleName),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("cockroachsql_grant.test", "id"),
				),
			},
		},
	})
}

func TestAccCockroachSQLGrantSchema(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, false)
	defer teardown()

	dbName, roleName := getTestDBNames(dbSuffix)

	config := fmt.Sprintf(`
resource "cockroachsql_role" "test" {
	name = "%%s"
}

resource "cockroachsql_schema" "test_schema" {
	name     = "test_schema"
	database = "%%s"
}

resource "cockroachsql_grant" "test" {
	database    = "%%s"
	schema      = cockroachsql_schema.test_schema.name
	role        = cockroachsql_role.test.name
	object_type = "schema"
	privileges  = %%s
}
`)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePrivileges)
		},
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(config, roleName, dbName, dbName, `["USAGE"]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("cockroachsql_grant.test", "id"),
					resource.TestCheckResourceAttr("cockroachsql_grant.test", "privileges.#", "1"),
					func(s *terraform.State) error {
						return testCheckSchemaPrivileges(t, roleName, dbName, "test_schema", true, false)(s)
					},
				),
			},
		},
	})
}

// TestAccCockroachSQLGrantDatabaseNotPolluteByPublic verifies that an explicit
// "CONNECT" grant on a database is read back as exactly ["CONNECT"] and not
// inflated by CockroachDB's built-in CONNECT/TEMPORARY grants on the "public"
// role. This is a regression test for a drift bug where the read function
// merged every `public`-grantee row into the named role's privilege set,
// producing permanent drift on every refresh.
func TestAccCockroachSQLGrantDatabaseNotPolluteByPublic(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	dbName, roleName := getTestDBNames(dbSuffix)

	tfConfig := fmt.Sprintf(`
resource "cockroachsql_grant" "test" {
	database    = "%s"
	role        = "%s"
	object_type = "database"
	privileges  = ["CONNECT"]
}
`, dbName, roleName)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePrivileges)
		},
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: tfConfig,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cockroachsql_grant.test", "privileges.#", "1"),
					resource.TestCheckTypeSetElemAttr("cockroachsql_grant.test", "privileges.*", "CONNECT"),
				),
			},
			// Second step with identical config must produce no plan diff; if
			// the read function had inflated state with public's TEMPORARY this
			// step would fail with "After applying this test step, the plan
			// was not empty".
			{
				Config:   tfConfig,
				PlanOnly: true,
			},
		},
	})
}

// TestAccCockroachSQLGrantSchemaNotPolluteByPublic verifies that an explicit
// "USAGE" grant on a schema is read back as exactly ["USAGE"] and not inflated
// by `public`'s implicit USAGE/CREATE grant on the `public` schema. See
// TestAccCockroachSQLGrantDatabaseNotPolluteByPublic for context.
func TestAccCockroachSQLGrantSchemaNotPolluteByPublic(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	dbName, roleName := getTestDBNames(dbSuffix)

	// Grant USAGE on the built-in `public` schema specifically because that's
	// the schema where CockroachDB's implicit `public`-role grants live.
	tfConfig := fmt.Sprintf(`
resource "cockroachsql_grant" "test" {
	database    = "%s"
	role        = "%s"
	schema      = "public"
	object_type = "schema"
	privileges  = ["USAGE"]
}
`, dbName, roleName)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePrivileges)
		},
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: tfConfig,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cockroachsql_grant.test", "privileges.#", "1"),
					resource.TestCheckTypeSetElemAttr("cockroachsql_grant.test", "privileges.*", "USAGE"),
				),
			},
			{
				Config:   tfConfig,
				PlanOnly: true,
			},
		},
	})
}

// TestAccCockroachSQLGrantAllSequencesPartialCoverage verifies that a
// "GRANT ... ON ALL SEQUENCES IN SCHEMA" grant is read back correctly when the
// schema contains a sequence that has no explicit grant row for the managed
// role (e.g. one created after the grant was applied). Previously the read
// function intersected privileges across every sequence in the schema, so a
// single ungranted sequence collapsed the reported set to empty and produced
// permanent drift.
func TestAccCockroachSQLGrantAllSequencesPartialCoverage(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	dbName, roleName := getTestDBNames(dbSuffix)

	// Pre-create one covered sequence so that the initial GRANT ON ALL
	// SEQUENCES has at least one row to apply to.
	dropSeq := createTestSequences(t, dbSuffix, []string{"test_schema.covered_seq"}, "")
	defer dropSeq()

	tfConfig := fmt.Sprintf(`
resource "cockroachsql_grant" "test" {
	database    = "%s"
	role        = "%s"
	schema      = "test_schema"
	object_type = "sequence"
	privileges  = ["USAGE", "SELECT"]
}
`, dbName, roleName)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePrivileges)
		},
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: tfConfig,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cockroachsql_grant.test", "privileges.#", "2"),
				),
			},
			// Inject a sequence that is NOT covered by the existing grant
			// (because it was created afterwards), then re-plan with the
			// same config. The read function must still report
			// ["USAGE","SELECT"] for the resource — otherwise the resource
			// would flap on every refresh.
			{
				PreConfig: func() {
					config := getTestConfig(t)
					dbExecute(t, config.connStr(dbName),
						"CREATE SEQUENCE test_schema.uncovered_seq")
				},
				Config:   tfConfig,
				PlanOnly: true,
			},
		},
	})
}
