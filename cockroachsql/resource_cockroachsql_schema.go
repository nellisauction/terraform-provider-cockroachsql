package cockroachsql

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/lib/pq"
)

const (
	schemaNameAttr     = "name"
	schemaDatabaseAttr = "database"
	schemaOwnerAttr    = "owner"
	schemaIfNotExists  = "if_not_exists"
	schemaDropCascade  = "drop_cascade"
)

func resourceCockroachSQLSchema() *schema.Resource {
	return &schema.Resource{
		Create: ResourceFunc(resourceCockroachSQLSchemaCreate),
		Read:   ResourceFunc(resourceCockroachSQLSchemaRead),
		Update: ResourceFunc(resourceCockroachSQLSchemaUpdate),
		Delete: ResourceFunc(resourceCockroachSQLSchemaDelete),
		Exists: ResourceExistsFunc(resourceCockroachSQLSchemaExists),
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			schemaNameAttr: {
				Type:        schema.TypeString,
				Required:    true,
				Description: "The name of the schema",
			},
			schemaDatabaseAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
				Description: "The database name to alter schema",
			},
			schemaOwnerAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "The ROLE name who owns the schema",
			},
			schemaIfNotExists: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     true,
				Description: "When true, use the existing schema if it exists",
			},
			schemaDropCascade: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "When true, will also drop all the objects that are contained in the schema",
			},
		},
	}
}

func resourceCockroachSQLSchemaCreate(db *DBConnection, d *schema.ResourceData) error {
	database := getDatabase(d, db.client.databaseName)

	// If the target database is different from the current connection,
	// get a connection to that database.
	conn := db.DB
	if database != db.client.databaseName {
		targetClient := db.client.config.NewClient(database)
		targetConn, err := targetClient.Connect()
		if err != nil {
			return err
		}
		conn = targetConn.DB
	}

	// If the authenticated user is not a superuser (e.g. on AWS RDS)
	// we'll need to temporarily grant it membership in the following roles:
	//  * the owner of the db (to have the permissions to create the schema)
	//  * the owner of the schema, if it has one (in order to change its owner)
	var rolesToGrant []string

	dbOwner, err := getDatabaseOwner(conn, database)
	if err != nil {
		return err
	}
	rolesToGrant = append(rolesToGrant, dbOwner)

	schemaOwner := d.Get("owner").(string)
	if schemaOwner != "" && schemaOwner != dbOwner {
		rolesToGrant = append(rolesToGrant, schemaOwner)
	}

	if err := withRolesGranted(conn, rolesToGrant, func() error {
		return createSchema(db, conn, d)
	}); err != nil {
		return err
	}

	d.SetId(generateSchemaID(d, database))

	return resourceCockroachSQLSchemaReadImpl(db, d)
}

func createSchema(db *DBConnection, conn QueryAble, d *schema.ResourceData) error {
	schemaName := d.Get(schemaNameAttr).(string)

	// Check if previous tasks haven't already create schema
	var foundSchema bool
	err := conn.QueryRow(`SELECT TRUE FROM pg_catalog.pg_namespace WHERE nspname = $1`, schemaName).Scan(&foundSchema)

	queries := []string{}
	switch {
	case err == sql.ErrNoRows:
		b := bytes.NewBufferString("CREATE SCHEMA ")
		if db.featureSupported(featureSchemaCreateIfNotExist) {
			if v := d.Get(schemaIfNotExists); v.(bool) {
				fmt.Fprint(b, "IF NOT EXISTS ")
			}
		}
		fmt.Fprint(b, pq.QuoteIdentifier(schemaName))

		switch v, ok := d.GetOk(schemaOwnerAttr); {
		case ok:
			fmt.Fprint(b, " AUTHORIZATION ", pq.QuoteIdentifier(v.(string)))
		}
		queries = append(queries, b.String())

	case err != nil:
		return fmt.Errorf("error looking for schema: %w", err)

	default:
		// The schema already exists, we just set the owner.
		if err := setSchemaOwner(conn, d); err != nil {
			return err
		}
	}

	for _, query := range queries {
		if _, err = conn.Exec(query); err != nil {
			return fmt.Errorf("error creating schema %s (SQL: %s): %w", schemaName, query, err)
		}
	}

	return nil
}

func resourceCockroachSQLSchemaDelete(db *DBConnection, d *schema.ResourceData) error {
	database := getDatabase(d, db.client.databaseName)

	conn := db.DB
	if database != db.client.databaseName {
		targetClient := db.client.config.NewClient(database)
		targetConn, err := targetClient.Connect()
		if err != nil {
			return err
		}
		conn = targetConn.DB
	}

	schemaName := d.Get(schemaNameAttr).(string)
	if schemaName == "public" {
		log.Printf("[WARN] CockroachSQL schema 'public' will not be dropped.")
		d.SetId("")
		return nil
	}

	exists, err := schemaExists(conn, schemaName)
	if err != nil {
		return err
	}
	if !exists {
		d.SetId("")
		return nil
	}

	owner := d.Get("owner").(string)

	if err = withRolesGranted(conn, []string{owner}, func() error {
		dropMode := "RESTRICT"
		if d.Get(schemaDropCascade).(bool) {
			dropMode = "CASCADE"
		}

		sql := fmt.Sprintf("DROP SCHEMA %s %s", pq.QuoteIdentifier(schemaName), dropMode)
		if _, err = conn.Exec(sql); err != nil {
			return fmt.Errorf("error deleting schema (SQL: %s): %w", sql, err)
		}

		return nil
	}); err != nil {
		return err
	}

	d.SetId("")

	return nil
}

func resourceCockroachSQLSchemaExists(db *DBConnection, d *schema.ResourceData) (bool, error) {
	database, schemaName, err := getDBSchemaName(d, db.client.databaseName)
	if err != nil {
		return false, err
	}

	// Check if the database exists
	exists, err := dbExists(db, database)
	if err != nil || !exists {
		return false, err
	}

	conn := db.DB
	if database != db.client.databaseName {
		targetClient := db.client.config.NewClient(database)
		targetConn, err := targetClient.Connect()
		if err != nil {
			return false, err
		}
		conn = targetConn.DB
	}

	err = conn.QueryRow("SELECT n.nspname FROM pg_catalog.pg_namespace n WHERE n.nspname=$1", schemaName).Scan(&schemaName)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("error reading schema: %w", err)
	}

	return true, nil
}

func resourceCockroachSQLSchemaRead(db *DBConnection, d *schema.ResourceData) error {
	return resourceCockroachSQLSchemaReadImpl(db, d)
}

func resourceCockroachSQLSchemaReadImpl(db *DBConnection, d *schema.ResourceData) error {
	database, schemaName, err := getDBSchemaName(d, db.client.databaseName)
	if err != nil {
		return err
	}

	conn := db.DB
	if database != db.client.databaseName {
		targetClient := db.client.config.NewClient(database)
		targetConn, err := targetClient.Connect()
		if err != nil {
			return err
		}
		conn = targetConn.DB
	}

	var schemaOwner string
	err = conn.QueryRow("SELECT pg_catalog.pg_get_userbyid(n.nspowner) FROM pg_catalog.pg_namespace n WHERE n.nspname=$1", schemaName).Scan(&schemaOwner)
	switch {
	case err == sql.ErrNoRows:
		log.Printf("[WARN] CockroachSQL schema (%s) not found in database %s", schemaName, database)
		d.SetId("")
		return nil
	case err != nil:
		return fmt.Errorf("error reading schema: %w", err)
	default:
		d.Set(schemaNameAttr, schemaName)
		d.Set(schemaOwnerAttr, schemaOwner)
		d.Set(schemaDatabaseAttr, database)
		d.SetId(generateSchemaID(d, database))

		return nil
	}
}

func resourceCockroachSQLSchemaUpdate(db *DBConnection, d *schema.ResourceData) error {
	databaseName := getDatabase(d, db.client.databaseName)

	conn := db.DB
	if databaseName != db.client.databaseName {
		targetClient := db.client.config.NewClient(databaseName)
		targetConn, err := targetClient.Connect()
		if err != nil {
			return err
		}
		conn = targetConn.DB
	}

	if err := setSchemaName(conn, d, databaseName); err != nil {
		return err
	}

	if err := setSchemaOwner(conn, d); err != nil {
		return err
	}

	return resourceCockroachSQLSchemaReadImpl(db, d)
}

func setSchemaName(conn QueryAble, d *schema.ResourceData, databaseName string) error {
	if !d.HasChange(schemaNameAttr) {
		return nil
	}

	oraw, nraw := d.GetChange(schemaNameAttr)
	o := oraw.(string)
	n := nraw.(string)
	if n == "" {
		return errors.New("error setting schema name to an empty string")
	}

	sql := fmt.Sprintf("ALTER SCHEMA %s RENAME TO %s", pq.QuoteIdentifier(o), pq.QuoteIdentifier(n))
	if _, err := conn.Exec(sql); err != nil {
		return fmt.Errorf("error updating schema NAME (SQL: %s): %w", sql, err)
	}
	d.SetId(generateSchemaID(d, databaseName))

	return nil
}

func setSchemaOwner(conn QueryAble, d *schema.ResourceData) error {
	if !d.HasChange(schemaOwnerAttr) {
		return nil
	}

	schemaName := d.Get(schemaNameAttr).(string)
	schemaOwner := d.Get(schemaOwnerAttr).(string)

	if schemaOwner == "" {
		return errors.New("error setting schema owner to an empty string")
	}

	sql := fmt.Sprintf("ALTER SCHEMA %s OWNER TO %s", pq.QuoteIdentifier(schemaName), pq.QuoteIdentifier(schemaOwner))
	if _, err := conn.Exec(sql); err != nil {
		return fmt.Errorf("error updating schema OWNER (SQL: %s): %w", sql, err)
	}

	return nil
}

func generateSchemaID(d *schema.ResourceData, databaseName string) string {
	SchemaID := strings.Join([]string{
		getDatabase(d, databaseName),
		d.Get(schemaNameAttr).(string),
	}, ".")

	return SchemaID
}

func getDBSchemaName(d *schema.ResourceData, databaseName string) (string, string, error) {
	database := getDatabase(d, databaseName)
	schemaName := d.Get(schemaNameAttr).(string)

	// When importing, we have to parse the ID to find schema and database names.
	if schemaName == "" {
		parsed := strings.Split(d.Id(), ".")
		if len(parsed) != 2 {
			return "", "", fmt.Errorf("schema ID %s has not the expected format 'database.schema': %v", d.Id(), parsed)
		}
		database = parsed[0]
		schemaName = parsed[1]
	}
	return database, schemaName, nil
}
