package cockroachsql

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

var schemaQueries = map[string]string{
	"query_include_system_schemas": `
	SELECT schema_name
	FROM information_schema.schemata
	`,
	"query_exclude_system_schemas": `
	SELECT schema_name
	FROM information_schema.schemata
	WHERE schema_name NOT LIKE 'pg_%'
	AND schema_name <> 'information_schema'
	`,
}

const schemaPatternMatchingTarget = "schema_name"

func dataSourceCockroachSQLDatabaseSchemas() *schema.Resource {
	return &schema.Resource{
		Read: ResourceFunc(dataSourceCockroachSQLSchemasRead),
		Schema: map[string]*schema.Schema{
			"database": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The CockroachSQL database which will be queried for schema names",
			},
			"include_system_schemas": {
				Type:        schema.TypeBool,
				Default:     false,
				Optional:    true,
				Description: "Determines whether to include system schemas (pg_ prefix and information_schema). 'public' will always be included.",
			},
			"like_any_patterns": {
				Type:        schema.TypeList,
				Optional:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				MinItems:    0,
				Description: "Expression(s) which will be pattern matched in the query using the CockroachSQL LIKE ANY operator",
			},
			"like_all_patterns": {
				Type:        schema.TypeList,
				Optional:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				MinItems:    0,
				Description: "Expression(s) which will be pattern matched in the query using the CockroachSQL LIKE ALL operator",
			},
			"not_like_all_patterns": {
				Type:        schema.TypeList,
				Optional:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				MinItems:    0,
				Description: "Expression(s) which will be pattern matched in the query using the CockroachSQL NOT LIKE ALL operator",
			},
			"regex_pattern": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Expression which will be pattern matched in the query using the CockroachSQL ~ (regular expression match) operator",
			},
			"schemas": {
				Type:        schema.TypeSet,
				Computed:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Set:         schema.HashString,
				Description: "The list of CockroachSQL schemas retrieved by this data source",
			},
		},
	}
}

func dataSourceCockroachSQLSchemasRead(db *DBConnection, d *schema.ResourceData) error {
	database := d.Get("database").(string)

	conn := db.DB
	if database != db.client.databaseName {
		targetClient := db.client.config.NewClient(database)
		targetConn, err := targetClient.Connect()
		if err != nil {
			return err
		}
		conn = targetConn.DB
	}

	includeSystemSchemas := d.Get("include_system_schemas").(bool)

	var query string
	var queryConcatKeyword string
	if includeSystemSchemas {
		query = schemaQueries["query_include_system_schemas"]
		queryConcatKeyword = queryConcatKeywordWhere
	} else {
		query = schemaQueries["query_exclude_system_schemas"]
		queryConcatKeyword = queryConcatKeywordAnd
	}

	query = applySchemaDataSourceQueryFilters(query, queryConcatKeyword, d)

	rows, err := conn.Query(query)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	schemas := []string{}
	for rows.Next() {
		var schema string

		if err = rows.Scan(&schema); err != nil {
			return fmt.Errorf("could not scan schema name for database: %w", err)
		}
		schemas = append(schemas, schema)
	}

	d.Set("schemas", stringSliceToSet(schemas))
	d.SetId(generateDataSourceSchemasID(d, database))

	return nil
}

func generateDataSourceSchemasID(d *schema.ResourceData, databaseName string) string {
	return strings.Join([]string{
		databaseName, strconv.FormatBool(d.Get("include_system_schemas").(bool)),
		generatePatternArrayString(d.Get("like_any_patterns").([]any), queryArrayKeywordAny),
		generatePatternArrayString(d.Get("like_all_patterns").([]any), queryArrayKeywordAll),
		generatePatternArrayString(d.Get("not_like_all_patterns").([]any), queryArrayKeywordAll),
		d.Get("regex_pattern").(string),
	}, "_")
}

func applySchemaDataSourceQueryFilters(query string, queryConcatKeyword string, d *schema.ResourceData) string {
	filters := []string{}
	filters = append(filters, applyPatternMatchingToQuery(schemaPatternMatchingTarget, d)...)

	return finalizeQueryWithFilters(query, queryConcatKeyword, filters)
}
