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
