package cockroachsql

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func TestAccCockroachSQLSchema_Basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckCockroachSQLSchemaDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCockroachSQLSchemaConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckCockroachSQLSchemaExists("cockroachsql_schema.test1", "foo"),
					resource.TestCheckResourceAttr("cockroachsql_role.role_all_without_grant", "name", "role_all_without_grant"),
					resource.TestCheckResourceAttr("cockroachsql_role.role_all_without_grant", "login", "true"),
					resource.TestCheckResourceAttr("cockroachsql_role.role_all_with_grant", "name", "role_all_with_grant"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test1", "name", "foo"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test2", "name", "bar"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test2", "owner", "role_all_without_grant"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test2", "if_not_exists", "false"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test3", "name", "baz"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test3", "owner", "role_all_without_grant"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test3", "if_not_exists", "true"),
				),
			},
		},
	})
}

func TestAccCockroachSQLSchema_Database(t *testing.T) {
	dbSuffix, teardown := setupTestDatabase(t, true, false)
	defer teardown()

	dbName, _ := getTestDBNames(dbSuffix)

	config := fmt.Sprintf(`
resource "cockroachsql_schema" "test" {
  name = "foo"
  database = "%s"
}
`, dbName)

	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckCockroachSQLSchemaDestroy,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckCockroachSQLSchemaExistsWithDatabase("cockroachsql_schema.test", "foo", dbName),
				),
			},
		},
	})
}

func TestAccCockroachSQLSchema_DropCascade(t *testing.T) {
	dbSuffix, teardown := setupTestDatabase(t, true, false)
	defer teardown()

	dbName, _ := getTestDBNames(dbSuffix)

	config := fmt.Sprintf(`
resource "cockroachsql_schema" "test" {
  name = "foo"
  database = "%s"
  drop_cascade = true
}
`, dbName)

	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckCockroachSQLSchemaDestroy,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckCockroachSQLSchemaExistsWithDatabase("cockroachsql_schema.test", "foo", dbName),
					func(s *terraform.State) error {
						config := getTestConfig(t)
						db, err := sql.Open(proxyDriverName, config.connStr(dbName))
						if err != nil {
							return err
						}
						defer closeDB(t, db)
						_, err = db.Exec("CREATE TABLE foo.bar (id int)")
						return err
					},
				),
			},
		},
	})
}

func TestAccCockroachSQLSchema_AlreadyExists(t *testing.T) {
	config := `
resource "cockroachsql_schema" "test" {
  name = "public"
}
`

	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckCockroachSQLSchemaDestroy,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckCockroachSQLSchemaExists("cockroachsql_schema.test", "public"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test", "owner", "root"),
				),
			},
		},
	})
}

func testAccCheckCockroachSQLSchemaDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*Client)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "cockroachsql_schema" {
			continue
		}

		schemaName := rs.Primary.Attributes["name"]
		if schemaName == "public" {
			continue
		}

		db, err := client.Connect()
		if err != nil {
			return err
		}

		var _rez bool
		err = db.QueryRow("SELECT TRUE FROM pg_catalog.pg_namespace WHERE nspname=$1", schemaName).Scan(&_rez)

		if err != sql.ErrNoRows {
			return fmt.Errorf("Schema still exists after destroy")
		}
	}

	return nil
}

func testAccCheckCockroachSQLSchemaExists(n, schemaName string) resource.TestCheckFunc {
	return testAccCheckCockroachSQLSchemaExistsWithDatabase(n, schemaName, "")
}

func testAccCheckCockroachSQLSchemaExistsWithDatabase(n, schemaName, dbName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Not found: %s", n)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("No ID is set")
		}

		client := testAccProvider.Meta().(*Client)
		if dbName == "" {
			dbName = getTestDatabaseName()
		}
		db, err := client.Connect()
		if err != nil {
			return err
		}

		var _rez bool
		err = db.QueryRow("SELECT TRUE FROM pg_catalog.pg_namespace WHERE nspname=$1", schemaName).Scan(&_rez)

		if err != nil {
			return fmt.Errorf("error reading info about schema: %s", err)
		}

		return nil
	}
}

const testAccCockroachSQLSchemaConfig = `
resource "cockroachsql_role" "role_all_without_grant" {
  name = "role_all_without_grant"
  login = true
}

resource "cockroachsql_role" "role_all_with_grant" {
  name = "role_all_with_grant"
}

resource "cockroachsql_schema" "test1" {
  name = "foo"
}

resource "cockroachsql_schema" "test2" {
  name = "bar"
  owner = cockroachsql_role.role_all_without_grant.name
  if_not_exists = false
}

resource "cockroachsql_schema" "test3" {
  name = "baz"
  owner = cockroachsql_role.role_all_without_grant.name
  if_not_exists = true
}
`
