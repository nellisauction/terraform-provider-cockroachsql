package cockroachsql

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func TestAccCockroachSQLDatabase_Basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		CheckDestroy:      testAccCheckCockroachSQLDatabaseDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCockroachSQLDatabaseConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckCockroachSQLDatabaseExists("cockroachsql_database.default_opts"),
					resource.TestCheckResourceAttr(
						"cockroachsql_database.default_opts", "owner", getTestConfig(t).Username),
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
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		CheckDestroy:      testAccCheckCockroachSQLDatabaseDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCockroachSQLDatabaseConfig,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cockroachsql_database.mydb_default_owner", "owner", getTestConfig(t).Username),
				),
			},
		},
	})
}

func TestAccCockroachSQLDatabase_Update(t *testing.T) {
	dbName := "update_test"

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		CheckDestroy:      testAccCheckCockroachSQLDatabaseDestroy,
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
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		CheckDestroy:      testAccCheckCockroachSQLDatabaseDestroy,
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

func TestAccCockroachSQLDatabase_NormalizedOptions(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		CheckDestroy:      testAccCheckCockroachSQLDatabaseDestroy,
		Steps: []resource.TestStep{
			{
				Config: `
					resource "cockroachsql_role" "owner" { name = "normalized_owner" }
					resource "cockroachsql_database" "test" {
						name            = "normalized_opts_db"
						owner           = cockroachsql_role.owner.name
						encoding        = "utf8"
						lc_collate      = "C"
						lc_ctype        = "C.UTF-8"
						tablespace_name = "pg_default"
					}`,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckCockroachSQLDatabaseExists("cockroachsql_database.test"),
					resource.TestCheckResourceAttr("cockroachsql_database.test", "owner", "normalized_owner"),
					resource.TestCheckResourceAttr("cockroachsql_database.test", "encoding", "UTF8"),
				),
			},
			{
				Config: `
					resource "cockroachsql_role" "owner" { name = "normalized_owner" }
					resource "cockroachsql_database" "test" {
						name            = "normalized_opts_db"
						owner           = cockroachsql_role.owner.name
						encoding        = "UTF-8"
						lc_collate      = "en_US.utf8"
						lc_ctype        = "C"
						tablespace_name = "DEFAULT"
					}`,
				PlanOnly: true,
			},
		},
	})
}

func TestAccCockroachSQLDatabase_RenameInPlace(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		CheckDestroy:      testAccCheckCockroachSQLDatabaseDestroy,
		Steps: []resource.TestStep{
			{
				Config: `resource "cockroachsql_database" "test" { name = "rename_before_db" }`,
				Check: func(*terraform.State) error {
					config := getTestConfig(t)
					dbExecute(t, config.connStr("rename_before_db"), "CREATE TABLE keep_me (id INT PRIMARY KEY)")
					return nil
				},
			},
			{
				Config: `resource "cockroachsql_database" "test" { name = "rename_after_db" }`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cockroachsql_database.test", "id", "rename_after_db"),
					testAccCheckCockroachSQLDatabaseExists("cockroachsql_database.test"),
					func(*terraform.State) error {
						config := getTestConfig(t)
						dbExecute(t, config.connStr("rename_after_db"), "SELECT 1 FROM keep_me")
						return nil
					},
				),
			},
		},
	})
}

func TestCreateDatabaseQuery(t *testing.T) {
	tests := []struct {
		name    string
		raw     map[string]any
		want    string
		wantErr bool
	}{
		{
			name: "defaults",
			raw:  map[string]any{"name": "db"},
			want: `CREATE DATABASE "db" OWNER "admin"`,
		},
		{
			name: "owner is emitted after locale options",
			raw:  map[string]any{"name": "db", "owner": "o", "encoding": "utf8", "lc_collate": "C", "lc_ctype": "C.UTF-8"},
			want: `CREATE DATABASE "db" ENCODING 'utf8' LC_COLLATE 'C' LC_CTYPE 'C.UTF-8' OWNER "o"`,
		},
		{
			name: "server default locales are not emitted",
			raw:  map[string]any{"name": "db", "lc_collate": "en_US.utf8", "lc_ctype": "DEFAULT", "tablespace_name": "pg_default"},
			want: `CREATE DATABASE "db" OWNER "admin"`,
		},
		{
			name:    "non-default tablespace is rejected",
			raw:     map[string]any{"name": "db", "tablespace_name": "fast"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := schema.TestResourceDataRaw(t, resourceCockroachSQLDatabase().Schema, tt.raw)
			got, err := createDatabaseQuery(d, "admin")
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got query %q", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDatabaseDiffSuppression(t *testing.T) {
	s := resourceCockroachSQLDatabase().Schema
	tests := []struct {
		attr, old, new string
		suppress       bool
	}{
		{"encoding", "UTF8", "utf8", true},
		{"encoding", "UTF8", "UTF-8", true},
		{"encoding", "UTF8", "LATIN1", false},
		{"lc_collate", "en_US.utf8", "C", true},
		{"lc_collate", "en_US.utf8", "C.UTF-8", true},
		{"lc_collate", "en_US.utf8", "en_US.UTF-8", true},
		{"lc_collate", "en_US.utf8", "de_DE.utf8", false},
		{"lc_ctype", "en_US.utf8", "c", true},
		{"tablespace_name", "pg_default", "DEFAULT", true},
		{"tablespace_name", "pg_default", "fast", false},
	}

	for _, tt := range tests {
		got := s[tt.attr].DiffSuppressFunc(tt.attr, tt.old, tt.new, nil)
		if got != tt.suppress {
			t.Errorf("%s: old=%q new=%q suppress=%v, want %v", tt.attr, tt.old, tt.new, got, tt.suppress)
		}
	}
}

func testAccCheckCockroachSQLDatabaseDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*Client)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "cockroachsql_database" {
			continue
		}

		db, err := client.Connect(context.Background())
		if err != nil {
			return err
		}

		exists, err := dbExists(context.Background(), db, rs.Primary.ID)
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
		db, err := client.Connect(context.Background())
		if err != nil {
			return err
		}

		exists, err := dbExists(context.Background(), db, rs.Primary.ID)
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

func TestGetDBName(t *testing.T) {
	d := schema.TestResourceDataRaw(t, resourceCockroachSQLDatabase().Schema, map[string]any{"name": "renamed"})
	d.SetId("original")
	if got := getDBName(d); got != "renamed" {
		t.Errorf("getDBName() = %q, want the name in state %q", got, "renamed")
	}

	imported := schema.TestResourceDataRaw(t, resourceCockroachSQLDatabase().Schema, map[string]any{})
	imported.SetId("imported_db")
	if got := getDBName(imported); got != "imported_db" {
		t.Errorf("getDBName() on import = %q, want the ID %q", got, "imported_db")
	}
}

// A rename updates the ID, but the Pulumi bridge keeps the original one.
// Read must still find the database by the name in state.
func TestAccCockroachSQLDatabase_ReadAfterRenameWithStaleID(t *testing.T) {
	skipIfNotAcc(t)
	testAccPreCheck(t)

	dbName := fmt.Sprintf("tf_tests_stale_id_db_%d", time.Now().UnixNano())
	config := getTestConfig(t)
	dbExecute(t, config.connStr(getTestDatabaseName()), fmt.Sprintf("CREATE DATABASE %s", dbName))
	defer dbExecute(t, config.connStr(getTestDatabaseName()), fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))

	db, err := testAccProvider.Meta().(*Client).Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	d := schema.TestResourceDataRaw(t, resourceCockroachSQLDatabase().Schema, map[string]any{"name": dbName})
	d.SetId(dbName + "_before_rename")
	if err := resourceCockroachSQLDatabaseRead(context.Background(), db, d); err != nil {
		t.Fatal(err)
	}
	if d.Id() != dbName {
		t.Fatalf("Read with a stale ID set ID %q, want %q", d.Id(), dbName)
	}
}
