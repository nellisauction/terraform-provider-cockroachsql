package cockroachsql

import (
	"bytes"
	"database/sql"
	"fmt"
	"log"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/lib/pq"
)

const (
	dbCTypeAttr            = "lc_ctype"
	dbCollationAttr        = "lc_collate"
	dbEncodingAttr         = "encoding"
	dbNameAttr             = "name"
	dbOwnerAttr            = "owner"
	dbTablespaceAttr       = "tablespace_name"
	dbTemplateAttr         = "template"
	dbAlterObjectOwnership = "alter_object_ownership"
)

func resourceCockroachSQLDatabase() *schema.Resource {
	return &schema.Resource{
		Create: ResourceFunc(resourceCockroachSQLDatabaseCreate),
		Read:   ResourceFunc(resourceCockroachSQLDatabaseRead),
		Update: ResourceFunc(resourceCockroachSQLDatabaseUpdate),
		Delete: ResourceFunc(resourceCockroachSQLDatabaseDelete),
		Exists: ResourceExistsFunc(resourceCockroachSQLDatabaseExists),
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			dbNameAttr: {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The name of the database",
			},
			dbOwnerAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "The role name of the database owner",
			},
			dbTemplateAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Computed:    true,
				Description: "The name of the template database from which to create the new database",
			},
			dbEncodingAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Computed:    true,
				Description: "Character set encoding to use in the new database",
			},
			dbCollationAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Computed:    true,
				Description: "Collation order (LC_COLLATE) to use in the new database",
			},
			dbCTypeAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Computed:    true,
				Description: "Character classification (LC_CTYPE) to use in the new database",
			},
			dbTablespaceAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Computed:    true,
				Description: "The name of the tablespace that will be associated with the new database",
			},
		},
	}
}

func resourceCockroachSQLDatabaseCreate(db *DBConnection, d *schema.ResourceData) error {
	if err := createDatabase(db, d); err != nil {
		return err
	}

	d.SetId(d.Get(dbNameAttr).(string))

	return resourceCockroachSQLDatabaseReadImpl(db, d)
}

func createDatabase(db *DBConnection, d *schema.ResourceData) error {
	currentUser := db.client.config.getDatabaseUsername()
	owner := d.Get(dbOwnerAttr).(string)

	if owner != "" {
		ownerGranted, err := grantRoleMembership(db, owner, currentUser)
		if err != nil {
			return err
		}
		if ownerGranted {
			defer func() {
				_, _ = revokeRoleMembership(db, owner, currentUser)
			}()
		}
	}

	dbName := d.Get(dbNameAttr).(string)
	b := bytes.NewBufferString("CREATE DATABASE ")
	fmt.Fprint(b, pq.QuoteIdentifier(dbName))

	if v, ok := d.GetOk(dbOwnerAttr); ok {
		fmt.Fprint(b, " OWNER ", pq.QuoteIdentifier(v.(string)))
	} else {
		fmt.Fprint(b, " OWNER ", pq.QuoteIdentifier(currentUser))
	}

	if v, ok := d.GetOk(dbCollationAttr); ok {
		if strings.ToUpper(v.(string)) == "DEFAULT" {
			fmt.Fprintf(b, " LC_COLLATE DEFAULT")
		} else {
			fmt.Fprintf(b, " LC_COLLATE '%s' ", pqQuoteLiteral(v.(string)))
		}
	}

	if v, ok := d.GetOk(dbCTypeAttr); ok {
		if strings.ToUpper(v.(string)) == "DEFAULT" {
			fmt.Fprintf(b, " LC_CTYPE DEFAULT")
		} else {
			fmt.Fprintf(b, " LC_CTYPE '%s' ", pqQuoteLiteral(v.(string)))
		}
	}

	if v, ok := d.GetOk(dbTablespaceAttr); ok {
		if strings.ToUpper(v.(string)) == "DEFAULT" {
			fmt.Fprint(b, " TABLESPACE DEFAULT")
		} else {
			fmt.Fprint(b, " TABLESPACE ", pq.QuoteIdentifier(v.(string)))
		}
	}

	query := b.String()
	if _, err := db.Exec(query); err != nil {
		return fmt.Errorf("error creating database %q (SQL: %s): %w", dbName, query, err)
	}

	return nil
}

func resourceCockroachSQLDatabaseDelete(db *DBConnection, d *schema.ResourceData) error {
	currentUser := db.client.config.getDatabaseUsername()
	owner := d.Get(dbOwnerAttr).(string)

	if owner != "" {
		ownerGranted, err := grantRoleMembership(db, owner, currentUser)
		if err != nil {
			return err
		}
		if ownerGranted {
			defer func() {
				_, _ = revokeRoleMembership(db, owner, currentUser)
			}()
		}
	}

	dbName := d.Get(dbNameAttr).(string)

	if err := terminateBConnections(db, dbName); err != nil {
		return err
	}

	query := fmt.Sprintf("DROP DATABASE %s", pq.QuoteIdentifier(dbName))
	if _, err := db.Exec(query); err != nil {
		return fmt.Errorf("error dropping database: %w", err)
	}

	d.SetId("")

	return nil
}

func resourceCockroachSQLDatabaseExists(db *DBConnection, d *schema.ResourceData) (bool, error) {
	return dbExists(db, d.Id())
}

func resourceCockroachSQLDatabaseRead(db *DBConnection, d *schema.ResourceData) error {
	return resourceCockroachSQLDatabaseReadImpl(db, d)
}

func resourceCockroachSQLDatabaseReadImpl(db *DBConnection, d *schema.ResourceData) error {
	dbId := d.Id()
	var dbName, ownerName string
	err := db.QueryRow("SELECT d.datname, pg_catalog.pg_get_userbyid(d.datdba) from pg_database d WHERE datname=$1", dbId).Scan(&dbName, &ownerName)
	switch {
	case err == sql.ErrNoRows:
		log.Printf("[WARN] CockroachSQL database (%q) not found", dbId)
		d.SetId("")
		return nil
	case err != nil:
		return fmt.Errorf("error reading database: %w", err)
	}

	var dbEncoding, dbCollation, dbCType, dbTablespaceName string

	columns := []string{
		"pg_catalog.pg_encoding_to_char(d.encoding)",
		"d.datcollate",
		"d.datctype",
		"ts.spcname",
	}

	dbSQLFmt := `SELECT %s ` +
		`FROM pg_catalog.pg_database AS d, pg_catalog.pg_tablespace AS ts ` +
		`WHERE d.datname = $1 AND d.dattablespace = ts.oid`
	dbSQL := fmt.Sprintf(dbSQLFmt, strings.Join(columns, ", "))
	err = db.QueryRow(dbSQL, dbId).
		Scan(
			&dbEncoding,
			&dbCollation,
			&dbCType,
			&dbTablespaceName,
		)
	switch {
	case err == sql.ErrNoRows:
		log.Printf("[WARN] CockroachSQL database (%q) not found", dbId)
		d.SetId("")
		return nil
	case err != nil:
		return fmt.Errorf("error reading database: %w", err)
	}

	d.Set(dbNameAttr, dbName)
	d.Set(dbOwnerAttr, ownerName)
	d.Set(dbEncodingAttr, dbEncoding)
	d.Set(dbCollationAttr, dbCollation)
	d.Set(dbCTypeAttr, dbCType)
	d.Set(dbTablespaceAttr, dbTablespaceName)
	dbTemplate := d.Get(dbTemplateAttr).(string)
	if dbTemplate == "" {
		dbTemplate = "template0"
	}
	d.Set(dbTemplateAttr, dbTemplate)

	return nil
}

func resourceCockroachSQLDatabaseUpdate(db *DBConnection, d *schema.ResourceData) error {
	if err := setDBOwner(db, d); err != nil {
		return err
	}

	return resourceCockroachSQLDatabaseReadImpl(db, d)
}

func setDBOwner(db *DBConnection, d *schema.ResourceData) error {
	if !d.HasChange(dbOwnerAttr) {
		return nil
	}

	owner := d.Get(dbOwnerAttr).(string)
	if owner == "" {
		return nil
	}
	currentUser := db.client.config.getDatabaseUsername()

	ownerGranted, err := grantRoleMembership(db, owner, currentUser)
	if err != nil {
		return err
	}
	if ownerGranted {
		defer func() {
			_, _ = revokeRoleMembership(db, owner, currentUser)
		}()
	}

	dbName := d.Get(dbNameAttr).(string)

	query := fmt.Sprintf("ALTER DATABASE %s OWNER TO %s", pq.QuoteIdentifier(dbName), pq.QuoteIdentifier(owner))
	if _, err := db.Exec(query); err != nil {
		return fmt.Errorf("error updating database OWNER: %w", err)
	}

	return nil
}

func terminateBConnections(db *DBConnection, dbName string) error {
	// CockroachDB handles concurrency via its serializable transaction isolation level.
	// Terminating connections is not explicitly required for DROP DATABASE.
	return nil
}
