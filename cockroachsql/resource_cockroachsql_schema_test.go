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
					resource.TestCheckResourceAttr("cockroachsql_schema.test2", "policy.#", "1"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test2", "policy.0.create", "true"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test2", "policy.0.create_with_grant", "false"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test2", "policy.0.usage", "true"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test2", "policy.0.usage_with_grant", "false"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test2", "policy.0.role", "role_all_without_grant"),

					resource.TestCheckResourceAttr("cockroachsql_schema.test3", "name", "baz"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test3", "owner", "role_all_without_grant"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test3", "if_not_exists", "true"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test3", "policy.#", "2"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test3", "policy.0.create_with_grant", "true"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test3", "policy.0.usage_with_grant", "true"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test3", "policy.0.role", "role_all_with_grant"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test3", "policy.1.create", "true"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test3", "policy.1.usage", "true"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test3", "policy.1.role", "role_all_without_grant"),
				),
			},
		},
	})
}

func TestAccCockroachSQLSchema_AddPolicy(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			// TODO: Need to check if removing policy is buggy
			testSuperuserPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckCockroachSQLSchemaDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCockroachSQLSchemaGrant1,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckCockroachSQLSchemaExists("cockroachsql_schema.test4", "test4"),

					resource.TestCheckResourceAttr("cockroachsql_role.all_without_grant_stay", "name", "all_without_grant_stay"),
					resource.TestCheckResourceAttr("cockroachsql_role.all_without_grant_drop", "name", "all_without_grant_drop"),
					resource.TestCheckResourceAttr("cockroachsql_role.policy_compose", "name", "policy_compose"),
					resource.TestCheckResourceAttr("cockroachsql_role.policy_move", "name", "policy_move"),

					resource.TestCheckResourceAttr("cockroachsql_role.all_with_grantstay", "name", "all_with_grantstay"),
					resource.TestCheckResourceAttr("cockroachsql_role.all_with_grantdrop", "name", "all_with_grantdrop"),

					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "name", "test4"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "owner", "all_without_grant_stay"),

					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.#", "7"),

					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.0.create", "false"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.0.create_with_grant", "true"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.0.role", "all_with_grantdrop"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.0.usage", "false"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.0.usage_with_grant", "true"),

					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.1.create", "false"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.1.create_with_grant", "true"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.1.role", "all_with_grantstay"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.1.usage", "false"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.1.usage_with_grant", "true"),

					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.2.create", "false"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.2.create_with_grant", "true"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.2.role", "policy_compose"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.2.usage", "false"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.2.usage_with_grant", "true"),

					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.3.create", "true"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.3.create_with_grant", "false"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.3.role", "all_without_grant_drop"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.3.usage", "true"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.3.usage_with_grant", "false"),

					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.4.create", "true"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.4.create_with_grant", "false"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.4.role", "all_without_grant_stay"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.4.usage", "true"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.4.usage_with_grant", "false"),

					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.5.create", "true"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.5.create_with_grant", "false"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.5.role", "policy_compose"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.5.usage", "true"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.5.usage_with_grant", "false"),

					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.6.create", "true"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.6.create_with_grant", "false"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.6.role", "policy_move"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.6.usage", "true"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.6.usage_with_grant", "false"),
				),
			},
			{
				Config: testAccCockroachSQLSchemaGrant2,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckCockroachSQLSchemaExists("cockroachsql_schema.test4", "test4"),
					resource.TestCheckResourceAttr("cockroachsql_role.all_without_grant_stay", "name", "all_without_grant_stay"),
					resource.TestCheckResourceAttr("cockroachsql_role.all_without_grant_drop", "name", "all_without_grant_drop"),
					resource.TestCheckResourceAttr("cockroachsql_role.policy_compose", "name", "policy_compose"),
					resource.TestCheckResourceAttr("cockroachsql_role.policy_move", "name", "policy_move"),

					resource.TestCheckResourceAttr("cockroachsql_role.all_with_grantstay", "name", "all_with_grantstay"),

					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "name", "test4"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "owner", "all_without_grant_stay"),

					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.#", "6"),

					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.0.create", "false"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.0.create_with_grant", "true"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.0.role", "all_with_grantstay"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.0.usage", "false"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.0.usage_with_grant", "true"),

					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.1.create", "false"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.1.create_with_grant", "true"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.1.role", "policy_compose"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.1.usage", "false"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.1.usage_with_grant", "true"),

					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.2.create", "false"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.2.create_with_grant", "true"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.2.role", "policy_move"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.2.usage", "false"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.2.usage_with_grant", "true"),

					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.3.create", "true"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.3.create_with_grant", "false"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.3.role", "all_without_grant_stay"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.3.usage", "true"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.3.usage_with_grant", "false"),

					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.4.create", "true"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.4.create_with_grant", "false"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.4.role", "policy_compose"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.4.usage", "true"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.4.usage_with_grant", "false"),

					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.5.create", "true"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.5.create_with_grant", "false"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.5.role", "policy_new"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.5.usage", "true"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test4", "policy.5.usage_with_grant", "false"),
				),
			},
		},
	})
}

func TestAccCockroachSQLSchema_Database(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	dbName, _ := getTestDBNames(dbSuffix)

	testAccCockroachSQLSchemaDatabaseConfig := fmt.Sprintf(`
	resource "cockroachsql_schema" "test_database" {
		name     = "test_database"
		database = "%s"
	}
	`, dbName)

	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckCockroachSQLSchemaDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCockroachSQLSchemaDatabaseConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckCockroachSQLSchemaExists("cockroachsql_schema.test_database", "test_database"),
					resource.TestCheckResourceAttr(
						"cockroachsql_schema.test_database", "name", "test_database"),
					resource.TestCheckResourceAttr(
						"cockroachsql_schema.test_database", "database", dbName),
				),
			},
		},
	})
}

func TestAccCockroachSQLSchema_DropCascade(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	dbName, _ := getTestDBNames(dbSuffix)

	var testAccCockroachSQLSchemaConfig = fmt.Sprintf(`
resource "cockroachsql_schema" "test_cascade" {
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
				Config: testAccCockroachSQLSchemaConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckCockroachSQLSchemaExists("cockroachsql_schema.test_cascade", "foo"),
					resource.TestCheckResourceAttr("cockroachsql_schema.test_cascade", "name", "foo"),

					// This will create a table in the schema to check if the drop will work thanks to the cascade
					testAccCreateSchemaTable(dbName, "foo"),
				),
			},
		},
	})
}

func TestAccCockroachSQLSchema_AlreadyExists(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()

	dbName, roleName := getTestDBNames(dbSuffix)

	// Test to create the schema 'public' that already exists
	// to assert it does not fail.
	var testAccCockroachSQLSchemaConfig = fmt.Sprintf(`
resource "cockroachsql_schema" "public" {
  name = "public"
  database = "%s"
  owner = "%s"
}
`, dbName, roleName)
	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckCockroachSQLSchemaDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCockroachSQLSchemaConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckCockroachSQLSchemaExists("cockroachsql_schema.public", "public"),
					testAccCheckSchemaOwner(dbName, "public", roleName),
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

		database, ok := rs.Primary.Attributes[schemaDatabaseAttr]
		if !ok {
			return fmt.Errorf("No Attribute for database is set")
		}

		txn, err := startTransaction(client, database)
		if err != nil {
			return err
		}
		defer deferredRollback(txn)

		schemaName, ok := rs.Primary.Attributes["name"]
		if !ok {
			return fmt.Errorf("No Attribute for name is set")
		}

		exists, err := checkSchemaExists(txn, schemaName)

		if err != nil {
			return fmt.Errorf("error checking schema %s", err)
		}

		if exists {
			return fmt.Errorf("Schema still exists after destroy")
		}
	}

	return nil
}

func testAccCheckCockroachSQLSchemaExists(n string, schemaName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Resource not found: %s", n)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("No ID is set")
		}

		database, ok := rs.Primary.Attributes[schemaDatabaseAttr]
		if !ok {
			return fmt.Errorf("No Attribute for database is set")
		}

		actualSchemaName := rs.Primary.Attributes["name"]
		if actualSchemaName != schemaName {
			return fmt.Errorf("Wrong value for schema name expected %s got %s", schemaName, actualSchemaName)
		}

		client := testAccProvider.Meta().(*Client)
		txn, err := startTransaction(client, database)
		if err != nil {
			return err
		}
		defer deferredRollback(txn)

		exists, err := checkSchemaExists(txn, schemaName)

		if err != nil {
			return fmt.Errorf("error checking schema %s", err)
		}

		if !exists {
			return fmt.Errorf("Schema not found")
		}

		return nil
	}
}

func checkSchemaExists(txn *sql.Tx, schemaName string) (bool, error) {
	var _rez bool
	err := txn.QueryRow("SELECT TRUE FROM pg_catalog.pg_namespace WHERE nspname=$1", schemaName).Scan(&_rez)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("error reading info about schema: %w", err)
	}

	return true, nil
}

func testAccCreateSchemaTable(database, schemaName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {

		client := testAccProvider.Meta().(*Client).config.NewClient(database)
		db, err := client.Connect()
		if err != nil {
			return err
		}

		if _, err = db.Exec(fmt.Sprintf("CREATE TABLE %s.test_table (id serial)", schemaName)); err != nil {
			return fmt.Errorf("could not create test table in schema %s: %s", schemaName, err)
		}

		return nil
	}
}

func testAccCheckSchemaOwner(database, schemaName, expectedOwner string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		client := testAccProvider.Meta().(*Client).config.NewClient(database)
		db, err := client.Connect()
		if err != nil {
			return err
		}

		var owner string

		query := "SELECT pg_catalog.pg_get_userbyid(n.nspowner)  FROM pg_catalog.pg_namespace n WHERE n.nspname=$1"
		switch err := db.QueryRow(query, schemaName).Scan(&owner); {
		case err == sql.ErrNoRows:
			return fmt.Errorf("could not find schema %s while checking owner", schemaName)
		case err != nil:
			return fmt.Errorf("error reading owner of schema %s: %w", schemaName, err)
		}

		if owner != expectedOwner {
			return fmt.Errorf("expected owner of schema %s to be %s; got %s", schemaName, expectedOwner, owner)
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
  owner = "${cockroachsql_role.role_all_without_grant.name}"
  if_not_exists = false

  }
}

resource "cockroachsql_schema" "test3" {
  name = "baz"
  owner = "${cockroachsql_role.role_all_without_grant.name}"
  if_not_exists = true

  }

  }
}
`

const testAccCockroachSQLSchemaGrant1 = `
resource "cockroachsql_role" "all_without_grant_stay" {
  name = "all_without_grant_stay"
}

resource "cockroachsql_role" "all_without_grant_drop" {
  name = "all_without_grant_drop"
}

resource "cockroachsql_role" "policy_compose" {
  name = "policy_compose"
}

resource "cockroachsql_role" "policy_move" {
  name = "policy_move"
}

resource "cockroachsql_role" "all_with_grantstay" {
  name = "all_with_grantstay"
}

resource "cockroachsql_role" "all_with_grantdrop" {
  name = "all_with_grantdrop"
}

resource "cockroachsql_schema" "test4" {
  name = "test4"
  owner = "${cockroachsql_role.all_without_grant_stay.name}"

  }

  }

  }

  }

  }

  }

  }
}
`

const testAccCockroachSQLSchemaGrant2 = `
resource "cockroachsql_role" "all_without_grant_stay" {
  name = "all_without_grant_stay"
}

resource "cockroachsql_role" "all_without_grant_drop" {
  name = "all_without_grant_drop"
}

resource "cockroachsql_role" "policy_compose" {
  name = "policy_compose"
}

resource "cockroachsql_role" "policy_move" {
  name = "policy_move"
}

resource "cockroachsql_role" "all_with_grantstay" {
  name = "all_with_grantstay"
}

resource "cockroachsql_role" "policy_new" {
  name = "policy_new"
}

resource "cockroachsql_schema" "test4" {
  name = "test4"
  owner = "${cockroachsql_role.all_without_grant_stay.name}"

  }

  }

  }

  }

  }

  }
}
`
