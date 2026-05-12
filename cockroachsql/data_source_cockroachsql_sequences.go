package cockroachsql

import (
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

const (
	sequenceQuery = `
	SELECT sequence_name, sequence_schema, data_type
	FROM information_schema.sequences
	`
	sequencePatternMatchingTarget = "sequence_name"
	sequenceSchemaKeyword         = "sequence_schema"
)

func dataSourceCockroachSQLDatabaseSequences() *schema.Resource {
	return &schema.Resource{
		Read: ResourceFunc(dataSourceCockroachSQLSequencesRead),
		Schema: map[string]*schema.Schema{
			"database": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The CockroachSQL database which will be queried for sequence names",
			},
			"schemas": {
				Type:        schema.TypeList,
				Optional:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				MinItems:    0,
				Description: "The CockroachSQL schema(s) which will be queried for sequence names. Queries all schemas in the database by default",
			},
			"like_any_patterns": {
				Type:        schema.TypeList,
				Optional:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				MinItems:    0,
				Description: "Expression(s) which will be pattern matched against sequence names in the query using the CockroachSQL LIKE ANY operator",
			},
			"like_all_patterns": {
				Type:        schema.TypeList,
				Optional:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				MinItems:    0,
				Description: "Expression(s) which will be pattern matched against sequence names in the query using the CockroachSQL LIKE ALL operator",
			},
			"not_like_all_patterns": {
				Type:        schema.TypeList,
				Optional:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				MinItems:    0,
				Description: "Expression(s) which will be pattern matched against sequence names in the query using the CockroachSQL NOT LIKE ALL operator",
			},
			"regex_pattern": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Expression which will be pattern matched against sequence names in the query using the CockroachSQL ~ (regular expression match) operator",
			},
			"sequences": {
				Type:     schema.TypeList,
				Computed: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"object_name": {
							Type:     schema.TypeString,
							Computed: true,
						},
						"schema_name": {
							Type:     schema.TypeString,
							Computed: true,
						},
						"data_type": {
							Type:     schema.TypeString,
							Computed: true,
						},
					},
				},
				Description: "The list of CockroachSQL sequence names retrieved by this data source. Note that this returns a set, so duplicate table names across different schemas will be consolidated.",
			},
		},
	}
}

func dataSourceCockroachSQLSequencesRead(db *DBConnection, d *schema.ResourceData) error {
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

	query := sequenceQuery
	queryConcatKeyword := queryConcatKeywordWhere

	query = applySequenceDataSourceQueryFilters(query, queryConcatKeyword, d)

	rows, err := conn.Query(query)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	sequences := make([]any, 0)
	for rows.Next() {
		var object_name string
		var schema_name string
		var data_type string

		if err = rows.Scan(&object_name, &schema_name, &data_type); err != nil {
			return fmt.Errorf("could not scan sequence output for database: %w", err)
		}

		result := make(map[string]any)
		result["object_name"] = object_name
		result["schema_name"] = schema_name
		result["data_type"] = data_type
		sequences = append(sequences, result)
	}

	d.Set("sequences", sequences)
	d.SetId(generateDataSourceSequencesID(d, database))

	return nil
}

func generateDataSourceSequencesID(d *schema.ResourceData, databaseName string) string {
	return strings.Join([]string{
		databaseName,
		generatePatternArrayString(d.Get("schemas").([]any), queryArrayKeywordAny),
		generatePatternArrayString(d.Get("like_any_patterns").([]any), queryArrayKeywordAny),
		generatePatternArrayString(d.Get("like_all_patterns").([]any), queryArrayKeywordAll),
		generatePatternArrayString(d.Get("not_like_all_patterns").([]any), queryArrayKeywordAll),
		d.Get("regex_pattern").(string),
	}, "_")
}

func applySequenceDataSourceQueryFilters(query string, queryConcatKeyword string, d *schema.ResourceData) string {
	filters := []string{}
	schemasTypeFilter := applyTypeMatchingToQuery(sequenceSchemaKeyword, d.Get("schemas").([]any))
	if len(schemasTypeFilter) > 0 {
		filters = append(filters, schemasTypeFilter)
	}
	filters = append(filters, applyPatternMatchingToQuery(sequencePatternMatchingTarget, d)...)

	return finalizeQueryWithFilters(query, queryConcatKeyword, filters)
}
