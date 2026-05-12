package cockroachsql

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func TestAccCockroachSQLDefaultPrivileges(t *testing.T) {
	skipIfNotAcc(t)

	// We have to create the database outside of resource.Test
	// because we need to create a table to assert that grant are correctly applied
	// and we don't have this resource yet
	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	config := getTestConfig(t)
	dbName, roleName := getTestDBNames(dbSuffix)

	// Set default privileges to the test role then to public (i.e.: everyone)
	for _, role := range []string{roleName, "public"} {
		t.Run(role, func(t *testing.T) {
			withGrant := (role != "public")

			// We set PGUSER as owner as he will create the test table
			var tfConfig = fmt.Sprintf(`
resource "cockroachsql_default_privileges" "test_ro" {
	database    = "%s"
	owner       = "%s"
	role        = "%s"
	schema      = "test_schema"
	object_type = "table"
	with_grant_option = %t
	privileges   = %%s
}
	`, dbName, config.Username, role, withGrant)

			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testCheckCompatibleVersion(t, featurePrivileges)
				},
				Providers: testAccProviders,
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(tfConfig, `[]`),
						Check: resource.ComposeTestCheckFunc(
							func(*terraform.State) error {
								tables := []string{"test_schema.test_table"}
								// To test default privileges, we need to create a table
								// after having apply the state.
								dropFunc := createTestTables(t, dbSuffix, tables, "")
								defer dropFunc()

								return testCheckTablesPrivileges(t, dbName, roleName, tables, []string{})
							},
							resource.TestCheckResourceAttr("cockroachsql_default_privileges.test_ro", "object_type", "table"),
							resource.TestCheckResourceAttr("cockroachsql_default_privileges.test_ro", "with_grant_option", fmt.Sprintf("%t", withGrant)),
							resource.TestCheckResourceAttr("cockroachsql_default_privileges.test_ro", "privileges.#", "0"),
						),
					},
					{
						Config: fmt.Sprintf(tfConfig, `["SELECT"]`),
						Check: resource.ComposeTestCheckFunc(
							func(*terraform.State) error {
								tables := []string{"test_schema.test_table"}
								// To test default privileges, we need to create a table
								// after having apply the state.
								dropFunc := createTestTables(t, dbSuffix, tables, "")
								defer dropFunc()

								return testCheckTablesPrivileges(t, dbName, roleName, tables, []string{"SELECT"})
							},
							resource.TestCheckResourceAttr("cockroachsql_default_privileges.test_ro", "object_type", "table"),
							resource.TestCheckResourceAttr("cockroachsql_default_privileges.test_ro", "with_grant_option", fmt.Sprintf("%t", withGrant)),
							resource.TestCheckResourceAttr("cockroachsql_default_privileges.test_ro", "privileges.#", "1"),
							resource.TestCheckResourceAttr("cockroachsql_default_privileges.test_ro", "privileges.0", "SELECT"),
						),
					},
					{
						Config: fmt.Sprintf(tfConfig, `["SELECT", "UPDATE"]`),
						Check: resource.ComposeTestCheckFunc(
							func(*terraform.State) error {
								tables := []string{"test_schema.test_table"}
								// To test default privileges, we need to create a table
								// after having apply the state.
								dropFunc := createTestTables(t, dbSuffix, tables, "")
								defer dropFunc()

								return testCheckTablesPrivileges(t, dbName, roleName, tables, []string{"SELECT", "UPDATE"})
							},
							resource.TestCheckResourceAttr("cockroachsql_default_privileges.test_ro", "privileges.#", "2"),
							resource.TestCheckResourceAttr("cockroachsql_default_privileges.test_ro", "privileges.0", "SELECT"),
							resource.TestCheckResourceAttr("cockroachsql_default_privileges.test_ro", "privileges.1", "UPDATE"),
						),
					},
					{
						Config: fmt.Sprintf(tfConfig, `[]`),
						Check: resource.ComposeTestCheckFunc(
							func(*terraform.State) error {
								tables := []string{"test_schema.test_table"}
								// To test default privileges, we need to create a table
								// after having apply the state.
								dropFunc := createTestTables(t, dbSuffix, tables, "")
								defer dropFunc()

								return testCheckTablesPrivileges(t, dbName, roleName, tables, []string{})
							},
							resource.TestCheckResourceAttr("cockroachsql_default_privileges.test_ro", "object_type", "table"),
							resource.TestCheckResourceAttr("cockroachsql_default_privileges.test_ro", "with_grant_option", fmt.Sprintf("%t", withGrant)),
							resource.TestCheckResourceAttr("cockroachsql_default_privileges.test_ro", "privileges.#", "0"),
						),
					},
				},
			})
		})
	}
}

// Test the case where we need to grant the owner to the connected user.
// The owner should be revoked
func TestAccCockroachSQLDefaultPrivileges_GrantOwner(t *testing.T) {
	skipIfNotAcc(t)

	// We have to create the database outside of resource.Test
	// because we need to create a table to assert that grant are correctly applied
	// and we don't have this resource yet
	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	dbName, roleName := getTestDBNames(dbSuffix)

	// We set PGUSER as owner as he will create the test table
	var stateConfig = fmt.Sprintf(`

resource cockroachsql_role "test_owner" {
    name = "test_owner"
}

// From CockroachSQL 15, schema public is not wild open anymore
resource "cockroachsql_grant" "public_usage" {
	database          = "%s"
	schema            = "public"
	role              = cockroachsql_role.test_owner.name
	object_type       = "schema"
	privileges        = ["CREATE", "USAGE"]
}

resource "cockroachsql_default_privileges" "test_ro" {
	database    = "%s"
	owner       = cockroachsql_role.test_owner.name
	role        = "%s"
	schema      = "public"
	object_type = "table"
	privileges  = ["SELECT"]
}
	`, dbName, dbName, roleName)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePrivileges)
		},
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: stateConfig,
				Check: resource.ComposeTestCheckFunc(
					func(*terraform.State) error {
						tables := []string{"public.test_table"}
						// To test default privileges, we need to create a table
						// after having apply the state.
						dropFunc := createTestTables(t, dbSuffix, tables, "test_owner")
						defer dropFunc()

						return testCheckTablesPrivileges(t, dbName, roleName, tables, []string{"SELECT"})
					},
					resource.TestCheckResourceAttr("cockroachsql_default_privileges.test_ro", "object_type", "table"),
					resource.TestCheckResourceAttr("cockroachsql_default_privileges.test_ro", "privileges.#", "1"),
					resource.TestCheckResourceAttr("cockroachsql_default_privileges.test_ro", "privileges.0", "SELECT"),
				),
			},
		},
	})
}

// Test the case where we define default privileges without specifying a schema. These
// privileges should apply to newly created resources for the named role in all schema.
func TestAccCockroachSQLDefaultPrivileges_NoSchema(t *testing.T) {
	skipIfNotAcc(t)

	// We have to create the database outside of resource.Test
	// because we need to create a table to assert that grant are correctly applied
	// and we don't have this resource yet
	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	config := getTestConfig(t)
	dbName, roleName := getTestDBNames(dbSuffix)

	// Set default privileges to the test role then to public (i.e.: everyone)
	for _, role := range []string{roleName, "public"} {
		t.Run(role, func(t *testing.T) {

			hclText := `
resource "cockroachsql_default_privileges" "test_ro" {
	database    = "%s"
	owner       = "%s"
	role        = "%s"
	object_type = "table"
	privileges  = %%s
}
`
			// We set PGUSER as owner as he will create the test table
			var tfConfig = fmt.Sprintf(hclText, dbName, config.Username, role)

			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testCheckCompatibleVersion(t, featurePrivileges)
				},
				Providers: testAccProviders,
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(tfConfig, `["SELECT"]`),
						Check: resource.ComposeTestCheckFunc(
							func(*terraform.State) error {
								tables := []string{"test_schema.test_table", "dev_schema.test_table"}
								// To test default privileges, we need to create tables
								// in both dev and test schema after having applied the state.
								dropFunc := createTestTables(t, dbSuffix, tables, "")
								defer dropFunc()

								return testCheckTablesPrivileges(t, dbName, roleName, tables, []string{"SELECT"})
							},
							resource.TestCheckResourceAttr("cockroachsql_default_privileges.test_ro", "object_type", "table"),
							resource.TestCheckResourceAttr("cockroachsql_default_privileges.test_ro", "privileges.#", "1"),
							resource.TestCheckResourceAttr("cockroachsql_default_privileges.test_ro", "privileges.0", "SELECT"),
						),
					},
					{
						Config: fmt.Sprintf(tfConfig, `["SELECT", "UPDATE"]`),
						Check: resource.ComposeTestCheckFunc(
							func(*terraform.State) error {
								tables := []string{"test_schema.test_table", "dev_schema.test_table"}
								// To test default privileges, we need to create tables
								// in both dev and test schema after having applied the state.
								dropFunc := createTestTables(t, dbSuffix, tables, "")
								defer dropFunc()

								return testCheckTablesPrivileges(t, dbName, roleName, tables, []string{"SELECT", "UPDATE"})
							},
							resource.TestCheckResourceAttr("cockroachsql_default_privileges.test_ro", "privileges.#", "2"),
							resource.TestCheckResourceAttr("cockroachsql_default_privileges.test_ro", "privileges.0", "SELECT"),
							resource.TestCheckResourceAttr("cockroachsql_default_privileges.test_ro", "privileges.1", "UPDATE"),
						),
					},
				},
			})
		})
	}
}

// Test defaults privileges on schemas
func TestAccCockroachSQLDefaultPrivilegesOnSchemas(t *testing.T) {
	skipIfNotAcc(t)

	// We have to create the database outside of resource.Test
	// because we need to create schemas to assert that grant are correctly applied
	// and we don't have this resource yet
	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	config := getTestConfig(t)
	dbName, roleName := getTestDBNames(dbSuffix)

	// Set default privileges to the test role then to public (i.e.: everyone)
	for _, role := range []string{roleName, "public"} {
		t.Run(role, func(t *testing.T) {

			hclText := `
resource "cockroachsql_default_privileges" "test_ro" {
	database    = "%s"
	owner       = "%s"
	role        = "%s"
	object_type = "schema"
	privileges  = %%s
}
`
			// We set PGUSER as owner as he will create the test schemas
			var tfConfig = fmt.Sprintf(hclText, dbName, config.Username, role)

			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testCheckCompatibleVersion(t, featurePrivileges)
					testCheckCompatibleVersion(t, featurePrivilegesOnSchemas)
				},
				Providers: testAccProviders,
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(tfConfig, `[]`),
						Check: resource.ComposeTestCheckFunc(
							func(*terraform.State) error {
								schemas := []string{"test_schema2", "dev_schema2"}
								// To test default privileges, we need to create a schema
								// after having apply the state.
								dropFunc := createTestSchemas(t, dbSuffix, schemas, "")
								defer dropFunc()

								return testCheckSchemasPrivileges(t, dbName, roleName, schemas, []string{})
							},
							resource.TestCheckResourceAttr("cockroachsql_default_privileges.test_ro", "object_type", "schema"),
							resource.TestCheckResourceAttr("cockroachsql_default_privileges.test_ro", "privileges.#", "0"),
						),
					},
					{
						Config: fmt.Sprintf(tfConfig, `["CREATE", "USAGE"]`),
						Check: resource.ComposeTestCheckFunc(
							func(*terraform.State) error {
								schemas := []string{"test_schema2", "dev_schema2"}
								// To test default privileges, we need to create a schema
								// after having apply the state.
								dropFunc := createTestSchemas(t, dbSuffix, schemas, "")
								defer dropFunc()

								return testCheckSchemasPrivileges(t, dbName, roleName, schemas, []string{"CREATE", "USAGE"})
							},
							resource.TestCheckResourceAttr(
								"cockroachsql_default_privileges.test_ro", "id", fmt.Sprintf("%s_%s_noschema_%s_schema", role, dbName, config.Username),
							),
							resource.TestCheckResourceAttr("cockroachsql_default_privileges.test_ro", "privileges.#", "2"),
							resource.TestCheckResourceAttr("cockroachsql_default_privileges.test_ro", "privileges.0", "CREATE"),
							resource.TestCheckResourceAttr("cockroachsql_default_privileges.test_ro", "privileges.1", "USAGE"),
						),
					},
				},
			})
		})
	}
}

func TestAccCockroachSQLDefaultPrivileges_Routines(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	config := getTestConfig(t)
	dbName, roleName := getTestDBNames(dbSuffix)

	resourceConfig := fmt.Sprintf(`
resource "cockroachsql_default_privileges" "test" {
	database          = "%s"
	schema            = "test_schema"
	owner             = "%s"
	role              = "%s"
	object_type       = "routine"
	privileges        = ["EXECUTE"]
}
`, dbName, config.Username, roleName)

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePrivileges)
		},
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: resourceConfig,
				Check: resource.ComposeTestCheckFunc(
					// Create a test function to check if default privileges are applied
					func(*terraform.State) error {
						dbExecute(
							t, config.connStr(dbName),
							"CREATE FUNCTION test_schema.test_function() RETURNS int AS $$ SELECT 1; $$ LANGUAGE sql;",
						)

						db := connectAsTestRole(t, roleName, dbName)
						defer closeDB(t, db)

						if _, err := db.Exec("SELECT test_schema.test_function();"); err != nil {
							t.Fatalf("Expected test role to be able to execute function, got error: %s", err)
						}

						return nil
					},
				),
			},
		},
	})
}
