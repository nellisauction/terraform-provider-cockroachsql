package cockroachsql

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func TestConnectionURLForDatabase(t *testing.T) {
	tests := []struct {
		url, database, want string
	}{
		{"postgresql://u:p@h:26257/defaultdb?sslmode=require", "other", "postgresql://u:p@h:26257/other?sslmode=require"},
		{"postgresql://u@h:26257?sslmode=disable", "other", "postgresql://u@h:26257/other?sslmode=disable"},
		{"postgresql://u@h/defaultdb", "", "postgresql://u@h/defaultdb"},
		{"postgresql://u@h/defaultdb", "my db", "postgresql://u@h/my%20db"},
		{"host=h user=u dbname=defaultdb", "other", "host=h user=u dbname=defaultdb dbname='other'"},
		{"host=h user=u", `it's`, `host=h user=u dbname='it\'s'`},
	}
	for _, tt := range tests {
		if got := connectionURLForDatabase(tt.url, tt.database); got != tt.want {
			t.Errorf("connectionURLForDatabase(%q, %q) = %q, want %q", tt.url, tt.database, got, tt.want)
		}
	}
}

func TestDatabaseFromConnectionURL(t *testing.T) {
	tests := map[string]string{
		"postgresql://u@h:26257/appdb?sslmode=require": "appdb",
		"postgresql://u@h:26257":                       "",
		"host=h dbname='appdb' user=u":                 "appdb",
		"host=h user=u":                                "",
	}
	for in, want := range tests {
		if got := databaseFromConnectionURL(in); got != want {
			t.Errorf("databaseFromConnectionURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveProviderDatabase(t *testing.T) {
	tests := []struct {
		name, database string
		explicit       bool
		url, want      string
	}{
		{"no url uses configured database", "defaultdb", false, "", "defaultdb"},
		{"url database replaces the default", "defaultdb", false, "postgresql://u@h/appdb", "appdb"},
		{"explicit database wins over url", "other", true, "postgresql://u@h/appdb", "other"},
		{"url without database falls back", "defaultdb", false, "postgresql://u@h", "defaultdb"},
	}
	for _, tt := range tests {
		if got := resolveProviderDatabase(tt.database, tt.explicit, tt.url); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestAccCockroachSQLSchema_URLWithResourceDatabase(t *testing.T) {
	skipIfNotAcc(t)
	dbSuffix, teardown := setupTestDatabase(t, true, false)
	defer teardown()
	dbName, _ := getTestDBNames(dbSuffix)

	testConfig := getTestConfig(t)
	if testConfig.ConnectionURL != "" {
		t.Skip("COCKROACH_URL already set")
	}
	connectionURL := testConfig.connStr(getTestDatabaseName())

	provider := Provider()
	resource.Test(t, resource.TestCase{
		PreCheck: func() { testAccPreCheck(t) },
		ProviderFactories: map[string]func() (*schema.Provider, error){
			"cockroachsql": func() (*schema.Provider, error) { return provider, nil },
		},
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
provider "cockroachsql" {
  url = %q
}

resource "cockroachsql_schema" "test" {
  name     = "url_schema"
  database = %q
}
`, connectionURL, dbName),
				Check: testAccCheckSchemaOnlyIn(t, "url_schema", dbName, getTestDatabaseName()),
			},
		},
	})
}

func TestAccCockroachSQLSchema_URLDatabaseIsProviderDefault(t *testing.T) {
	skipIfNotAcc(t)
	dbSuffix, teardown := setupTestDatabase(t, true, false)
	defer teardown()
	dbName, _ := getTestDBNames(dbSuffix)

	testConfig := getTestConfig(t)
	if testConfig.ConnectionURL != "" {
		t.Skip("COCKROACH_URL already set")
	}
	fallbackDatabase := getTestDatabaseName()
	connectionURL := testConfig.connStr(dbName)
	t.Setenv("COCKROACH_DATABASE", "")

	provider := Provider()
	resource.Test(t, resource.TestCase{
		PreCheck: func() { testAccPreCheck(t) },
		ProviderFactories: map[string]func() (*schema.Provider, error){
			"cockroachsql": func() (*schema.Provider, error) { return provider, nil },
		},
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
provider "cockroachsql" {
  url = %q
}

resource "cockroachsql_schema" "test" {
  name = "url_default_schema"
}
`, connectionURL),
				Check: testAccCheckSchemaOnlyIn(t, "url_default_schema", dbName, fallbackDatabase),
			},
		},
	})
}

func testAccCheckSchemaOnlyIn(t *testing.T, schemaName, wantDatabase, otherDatabase string) resource.TestCheckFunc {
	testConfig := getTestConfig(t)
	return func(*terraform.State) error {
		for database, want := range map[string]bool{wantDatabase: true, otherDatabase: false} {
			db, err := sql.Open(proxyDriverName, testConfig.connStr(database))
			if err != nil {
				return err
			}
			var found bool
			err = db.QueryRow("SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = $1)", schemaName).Scan(&found)
			_ = db.Close()
			if err != nil {
				return err
			}
			if found != want {
				return fmt.Errorf("schema %s exists in %s = %v, want %v", schemaName, database, found, want)
			}
		}
		return nil
	}
}
