package cockroachsql

import (
	"database/sql"
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
					testAccCheckCockroachSQLDatabaseExists("cockroachsql_database.mydb"),
					resource.TestCheckResourceAttr(
						"cockroachsql_database.mydb", "name", "mydb"),
					resource.TestCheckResourceAttr(
						"cockroachsql_database.mydb", "owner", "myrole"),
					resource.TestCheckResourceAttr(
						"cockroachsql_database.default_opts", "owner", "myrole"),
					resource.TestCheckResourceAttr(
						"cockroachsql_database.default_opts", "name", "default_opts_name"),
					resource.TestCheckResourceAttr(
						"cockroachsql_database.default_opts", "is_template", "false"),
					resource.TestCheckResourceAttr(
						"cockroachsql_database.default_opts", "alter_object_ownership", "false"),

					resource.TestCheckResourceAttr(
						"cockroachsql_database.modified_opts", "owner", "myrole"),
					resource.TestCheckResourceAttr(
						"cockroachsql_database.modified_opts", "name", "custom_template_db"),
					resource.TestCheckResourceAttr(
						"cockroachsql_database.modified_opts", "is_template", "true"),
					resource.TestCheckResourceAttr(
						"cockroachsql_database.modified_opts", "alter_object_ownership", "true"),

					resource.TestCheckResourceAttr(
						"cockroachsql_database.pathological_opts", "owner", "myrole"),
					resource.TestCheckResourceAttr(
						"cockroachsql_database.pathological_opts", "name", "bad_template_db"),
					resource.TestCheckResourceAttr(
						"cockroachsql_database.pathological_opts", "is_template", "true"),

					resource.TestCheckResourceAttr(
						"cockroachsql_database.pg_default_opts", "owner", "myrole"),
					resource.TestCheckResourceAttr(
						"cockroachsql_database.pg_default_opts", "name", "pg_defaults_db"),
					resource.TestCheckResourceAttr(
						"cockroachsql_database.pg_default_opts", "is_template", "true"),
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
					testAccCheckCockroachSQLDatabaseExists("cockroachsql_database.mydb_default_owner"),
					resource.TestCheckResourceAttr(
						"cockroachsql_database.mydb_default_owner", "name", "mydb_default_owner"),
					resource.TestCheckResourceAttrSet(
						"cockroachsql_database.mydb_default_owner", "owner"),
				),
			},
		},
	})
}

func TestAccCockroachSQLDatabase_Update(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckCockroachSQLDatabaseDestroy,
		Steps: []resource.TestStep{
			{
				Config: `resource "cockroachsql_database" "test_db" { name = "test_db" }`,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckCockroachSQLDatabaseExists("cockroachsql_database.test_db"),
					resource.TestCheckResourceAttr("cockroachsql_database.test_db", "name", "test_db"),
				),
			},
			{
				Config: `resource "cockroachsql_database" "test_db" { name = "test_db_updated" }`,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckCockroachSQLDatabaseExists("cockroachsql_database.test_db"),
					resource.TestCheckResourceAttr("cockroachsql_database.test_db", "name", "test_db_updated"),
				),
			},
		},
	})
}

func TestAccCockroachSQLDatabase_GrantOwner(t *testing.T) {
	var tfConfig = `
resource "cockroachsql_role" "owner" {
  name = "owner"
}

resource "cockroachsql_database" "test_db" {
  name  = "test_db"
  owner = cockroachsql_role.owner.name
}
`
	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckCockroachSQLDatabaseDestroy,
		Steps: []resource.TestStep{
			{
				Config: tfConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckCockroachSQLDatabaseExists("cockroachsql_database.test_db"),
					resource.TestCheckResourceAttr("cockroachsql_database.test_db", "owner", "owner"),
				),
			},
		},
	})
}

func TestAccCockroachSQLDatabase_AlterObjectOwnership(t *testing.T) {
	var tfConfig = `
resource "cockroachsql_role" "owner1" {
  name = "owner1"
}

resource "cockroachsql_role" "owner2" {
  name = "owner2"
}

resource "cockroachsql_database" "test_db" {
  name                   = "tf_tests_db_ownership"
  owner                  = cockroachsql_role.owner1.name
  alter_object_ownership = true
}
`

	var tfConfigUpdate = `
resource "cockroachsql_role" "owner1" {
  name = "owner1"
}

resource "cockroachsql_role" "owner2" {
  name = "owner2"
}

resource "cockroachsql_database" "test_db" {
  name                   = "tf_tests_db_ownership"
  owner                  = cockroachsql_role.owner2.name
  alter_object_ownership = true
}
`
	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckCockroachSQLDatabaseDestroy,
		Steps: []resource.TestStep{
			{
				Config: tfConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckCockroachSQLDatabaseExists("cockroachsql_database.test_db"),
					resource.TestCheckResourceAttr("cockroachsql_database.test_db", "owner", "owner1"),
					func(*terraform.State) error {
						return createTableAsOwner(t, "tf_tests_db_ownership", "owner1", "test_table")
					},
				),
			},
			{
				Config: tfConfigUpdate,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckCockroachSQLDatabaseExists("cockroachsql_database.test_db"),
					resource.TestCheckResourceAttr("cockroachsql_database.test_db", "owner", "owner2"),
					func(*terraform.State) error {
						cfg := getTestConfig(t)
						return checkTableOwnership(t, (&cfg).connStr("tf_tests_db_ownership"), "owner2", "test_table")(nil)
					},
				),
			},
		},
	})
}

func createTableAsOwner(t *testing.T, dbName, owner, tableName string) error {
	config := getTestConfig(t)
	db, err := sql.Open(proxyDriverName, config.connStr(dbName))
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	if _, err := db.Exec(fmt.Sprintf("GRANT %s TO CURRENT_USER", owner)); err != nil {
		return err
	}
	if _, err := db.Exec(fmt.Sprintf("SET ROLE %s", owner)); err != nil {
		return err
	}
	if _, err := db.Exec(fmt.Sprintf("CREATE TABLE %s (id int)", tableName)); err != nil {
		return err
	}
	if _, err := db.Exec("RESET ROLE"); err != nil {
		return err
	}
	return nil
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
			return fmt.Errorf("error checking database %s", err)
		}

		if exists {
			return fmt.Errorf("Database still exists after destroy")
		}
	}

	return nil
}

func testAccCheckCockroachSQLDatabaseExists(n string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Resource not found: %s", n)
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
			return fmt.Errorf("error checking database %s", err)
		}

		if !exists {
			return fmt.Errorf("Database not found")
		}

		return nil
	}
}

func checkTableOwnership(t *testing.T, dsn, owner, tableName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		db, err := sql.Open(proxyDriverName, dsn)
		if err != nil {
			return err
		}
		defer func() { _ = db.Close() }()

		var _rez int
		err = db.QueryRow("SELECT 1 FROM pg_tables WHERE tablename = $1 AND tableowner = $2", tableName, owner).Scan(&_rez)
		if err != nil {
			return err
		}
		return nil
	}
}

var testAccCockroachSQLDatabaseConfig = `
resource "cockroachsql_role" "myrole" {
  name = "myrole"
}

resource "cockroachsql_database" "mydb" {
  name  = "mydb"
  owner = cockroachsql_role.myrole.name
}

resource "cockroachsql_database" "default_opts" {
  name             = "default_opts_name"
  owner            = cockroachsql_role.myrole.name
  is_template      = false
}

resource "cockroachsql_database" "modified_opts" {
  name                   = "custom_template_db"
  owner                  = cockroachsql_role.myrole.name
  is_template            = true
  alter_object_ownership = true
}

resource "cockroachsql_database" "pathological_opts" {
  name        = "bad_template_db"
  owner       = cockroachsql_role.myrole.name
  is_template = true
}

resource "cockroachsql_database" "pg_default_opts" {
  name        = "pg_defaults_db"
  owner       = cockroachsql_role.myrole.name
  template    = "DEFAULT"
  is_template = true
}

resource "cockroachsql_database" "mydb_default_owner" {
  name = "mydb_default_owner"
}
`
