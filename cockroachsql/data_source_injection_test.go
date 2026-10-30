package cockroachsql

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
)

func TestAccCockroachSQLDataSources_FilterValuesAreNotSQL(t *testing.T) {
	skipIfNotAcc(t)

	dbSuffix, teardown := setupTestDatabase(t, true, true)
	defer teardown()
	createTestSchemas(t, dbSuffix, []string{`"it's_schema"`}, "")
	dbName, _ := getTestDBNames(dbSuffix)

	const injection = `x' OR '1'='1`

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
data "cockroachsql_schemas" "injected_like" {
  database          = %[1]q
  like_any_patterns = [%[2]q]
}

data "cockroachsql_schemas" "injected_regex" {
  database      = %[1]q
  regex_pattern = %[2]q
}

data "cockroachsql_schemas" "quoted_name" {
  database          = %[1]q
  like_all_patterns = ["it's%%"]
}

data "cockroachsql_tables" "injected_schema" {
  database = %[1]q
  schemas  = [%[2]q]
}

data "cockroachsql_sequences" "injected_schema" {
  database          = %[1]q
  not_like_all_patterns = [%[2]q]
  schemas           = [%[2]q]
}
`, dbName, injection),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.cockroachsql_schemas.injected_like", "schemas.#", "0"),
					resource.TestCheckResourceAttr("data.cockroachsql_schemas.injected_regex", "schemas.#", "0"),
					resource.TestCheckResourceAttr("data.cockroachsql_schemas.quoted_name", "schemas.#", "1"),
					resource.TestCheckTypeSetElemAttr("data.cockroachsql_schemas.quoted_name", "schemas.*", "it's_schema"),
					resource.TestCheckResourceAttr("data.cockroachsql_tables.injected_schema", "tables.#", "0"),
					resource.TestCheckResourceAttr("data.cockroachsql_sequences.injected_schema", "sequences.#", "0"),
				),
			},
		},
	})
}
