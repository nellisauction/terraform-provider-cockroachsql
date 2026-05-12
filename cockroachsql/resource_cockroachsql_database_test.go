package cockroachsql

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func TestAccCockroachSQLDatabase_Basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckCockroachSQLDatabaseDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCockroachSQLDatabaseConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckCockroachSQLDatabaseExists("cockroachsql_database.default_opts"),
					resource.TestCheckResourceAttr(
						"cockroachsql_database.default_opts", "owner", "root"),
					resource.TestCheckResourceAttr(
						"cockroachsql_database.default_opts", "name", "default_db"),
					testAccCheckCockroachSQLDatabaseExists("cockroachsql_database.modified_opts"),
					resource.TestCheckResourceAttr(
						"cockroachsql_database.modified_opts", "owner", "myrole"),
					resource.TestCheckResourceAttr(
						"cockroachsql_database.modified_opts", "name", "modified_db"),
					testAccCheckCockroachSQLDatabaseExists("cockroachsql_database.pathological_opts"),
					resource.TestCheckResourceAttr(
						"cockroachsql_database.pathological_opts", "name", "pathological_opts"),
					resource.TestCheckResourceAttr(
						"cockroachsql_database.crdb_default_opts", "owner", "myrole"),
					resource.TestCheckResourceAttr(
						"cockroachsql_database.crdb_default_opts", "name", "crdb_defaults_db"),
				),
			},
		},
	})
}

func TestAccCockroachSQLDatabase_DefaultOwner(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckCockroachSQLDatabaseDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCockroachSQLDatabaseConfig,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cockroachsql_database.mydb_default_owner", "owner", "root"),
				),
			},
		},
	})
}

func TestAccCockroachSQLDatabase_Update(t *testing.T) {
	dbName := "update_test"

	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckCockroachSQLDatabaseDestroy,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`resource "cockroachsql_database" "test" { name = "%s" }`, dbName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckCockroachSQLDatabaseExists("cockroachsql_database.test"),
				),
			},
			{
				Config: fmt.Sprintf(`
					resource "cockroachsql_role" "owner" { name = "new_owner" }
					resource "cockroachsql_database" "test" {
						name  = "%s"
						owner = cockroachsql_role.owner.name
					}`, dbName),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cockroachsql_database.test", "owner", "new_owner"),
				),
			},
		},
	})
}

func TestAccCockroachSQLDatabase_GrantOwner(t *testing.T) {
	dbName := "grant_owner_test"

	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckCockroachSQLDatabaseDestroy,
		Steps: []resource.TestStep{
			{
				Config: `resource "cockroachsql_role" "owner" { name = "initial_owner" }`,
			},
			{
				Config: fmt.Sprintf(`
					resource "cockroachsql_role" "owner" { name = "initial_owner" }
					resource "cockroachsql_database" "test" {
						name  = "%s"
						owner = cockroachsql_role.owner.name
					}`, dbName),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cockroachsql_database.test", "owner", "initial_owner"),
				),
			},
			{
				Config: fmt.Sprintf(`
					resource "cockroachsql_role" "owner" { name = "initial_owner" }
					resource "cockroachsql_role" "new_owner" { name = "new_owner" }
					resource "cockroachsql_database" "test" {
						name  = "%s"
						owner = cockroachsql_role.new_owner.name
					}`, dbName),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cockroachsql_database.test", "owner", "new_owner"),
				),
			},
		},
	})
}

func testAccCheckCockroachSQLDatabaseDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*Client)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "cockroachsql_database" {
			continue
		}

		db, err := client.Connect()
		if err != nil {
			return err
		}

		exists, err := dbExists(db, rs.Primary.ID)
		if err != nil {
			return err
		}

		if exists {
			return fmt.Errorf("Database %s still exists", rs.Primary.ID)
		}
	}

	return nil
}

func testAccCheckCockroachSQLDatabaseExists(n string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Not found: %s", n)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("No ID is set")
		}

		client := testAccProvider.Meta().(*Client)
		db, err := client.Connect()
		if err != nil {
			return err
		}

		exists, err := dbExists(db, rs.Primary.ID)
		if err != nil {
			return err
		}

		if !exists {
			return fmt.Errorf("Database %s not found", rs.Primary.ID)
		}

		return nil
	}
}

const testAccCockroachSQLDatabaseConfig = `
resource "cockroachsql_role" "myrole" {
  name     = "myrole"
}

resource "cockroachsql_database" "default_opts" {
  name             = "default_db"
}

resource "cockroachsql_database" "modified_opts" {
  name                   = "modified_db"
  owner                  = cockroachsql_role.myrole.name
}

resource "cockroachsql_database" "pathological_opts" {
  name        = "pathological_opts"
  owner       = cockroachsql_role.myrole.name
}

resource "cockroachsql_database" "crdb_default_opts" {
  name        = "crdb_defaults_db"
  owner       = cockroachsql_role.myrole.name
}

resource "cockroachsql_database" "mydb_default_owner" {
  name = "mydb_default_owner"
}
`
