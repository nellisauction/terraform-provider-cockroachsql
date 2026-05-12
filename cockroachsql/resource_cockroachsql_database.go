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
	dbAllowConnsAttr       = "allow_connections"
	dbCTypeAttr            = "lc_ctype"
	dbCollationAttr        = "lc_collate"
	dbEncodingAttr         = "encoding"
	dbIsTemplateAttr       = "is_template"
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
				Description: "The CockroachSQL database name to connect to",
			},
			dbOwnerAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "The ROLE which owns the database",
			},
			dbTemplateAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Computed:    true,
				Description: "The name of the template from which to create the new database",
			},
			dbEncodingAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
				Description: "Character set encoding to use in the new database",
			},
			dbCollationAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
				Description: "Collation order (LC_COLLATE) to use in the new database",
			},
			dbCTypeAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
				Description: "Character classification (LC_CTYPE) to use in the new database",
			},
			dbTablespaceAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "The name of the tablespace that will be associated with the new database",
			},
			dbAllowConnsAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     true,
				Description: "If false then no one can connect to this database",
			},
			dbIsTemplateAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				Description: "If true, then this database can be cloned by any user with CREATEDB privileges",
			},
			dbAlterObjectOwnership: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "If true, the owner of already existing objects will change if the owner changes",
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

	var err error
	if owner != "" {

		// Needed in order to set the owner of the db if the connection user is not a
		// superuser
		ownerGranted, err := grantRoleMembership(db, owner, currentUser)
		if err != nil {
			return err
		}
		if ownerGranted {
			defer func() {
				_, err = revokeRoleMembership(db, owner, currentUser)
			}()
		}
	}

	dbName := d.Get(dbNameAttr).(string)
	b := bytes.NewBufferString("CREATE DATABASE ")
	fmt.Fprint(b, pq.QuoteIdentifier(dbName))

	// Handle each option individually and stream results into the query
	// buffer.
	switch v, ok := d.GetOk(dbOwnerAttr); {
	case ok:
		fmt.Fprint(b, " OWNER ", pq.QuoteIdentifier(v.(string)))
	default:
		// No owner specified in the config, default to using
		// the connecting username.
		fmt.Fprint(b, " OWNER ", pq.QuoteIdentifier(currentUser))
	}

	// Don't specify LC_COLLATE if user didn't specify it
	// This will use the default one (usually the one defined in the template database)
	switch v, ok := d.GetOk(dbCollationAttr); {
	case ok && strings.ToUpper(v.(string)) == "DEFAULT":
		fmt.Fprintf(b, " LC_COLLATE DEFAULT")
	case ok:
		fmt.Fprintf(b, " LC_COLLATE '%s' ", pqQuoteLiteral(v.(string)))
	}

	// Don't specify LC_CTYPE if user didn't specify it
	// This will use the default one (usually the one defined in the template database)
	switch v, ok := d.GetOk(dbCTypeAttr); {
	case ok && strings.ToUpper(v.(string)) == "DEFAULT":
		fmt.Fprintf(b, " LC_CTYPE DEFAULT")
	case ok:
		fmt.Fprintf(b, " LC_CTYPE '%s' ", pqQuoteLiteral(v.(string)))
	}

	switch v, ok := d.GetOk(dbTablespaceAttr); {
	case ok && strings.ToUpper(v.(string)) == "DEFAULT":
		fmt.Fprint(b, " TABLESPACE DEFAULT")
	case ok:
		fmt.Fprint(b, " TABLESPACE ", pq.QuoteIdentifier(v.(string)))
	}

	if db.featureSupported(featureDBIsTemplate) {
		val := d.Get(dbIsTemplateAttr).(bool)
		fmt.Fprint(b, " IS_TEMPLATE ", val)
	}

	sql := b.String()
	if _, err := db.Exec(sql); err != nil {
		return fmt.Errorf("error creating database %q (SQL: %s): %w", dbName, sql, err)
	}

	// Set err outside of the return so that the deferred revoke can override err
	// if necessary.
	return err
}

func resourceCockroachSQLDatabaseDelete(db *DBConnection, d *schema.ResourceData) error {
	currentUser := db.client.config.getDatabaseUsername()
	owner := d.Get(dbOwnerAttr).(string)

	var dropWithForce string
	var err error
	if owner != "" {

		// Needed in order to set the owner of the db if the connection user is not a
		// superuser
		ownerGranted, err := grantRoleMembership(db, owner, currentUser)
		if err != nil {
			return err
		}
		if ownerGranted {
			defer func() {
				_, err = revokeRoleMembership(db, owner, currentUser)
			}()
		}
	}

	dbName := d.Get(dbNameAttr).(string)
	if db.featureSupported(featureDBIsTemplate) {
		if isTemplate := d.Get(dbIsTemplateAttr).(bool); isTemplate {
			// Template databases must have this attribute cleared before
			// they can be dropped.
			if err := doSetDBIsTemplate(db, dbName, false); err != nil {
				return fmt.Errorf("error updating database IS_TEMPLATE during DROP DATABASE: %w", err)
			}
		}
	}

	if err := setDBIsTemplate(db, d); err != nil {
		return err
	}

	// Terminate all active connections and block new one
	if err := terminateBConnections(db, dbName); err != nil {
		return err
	}

	// Drop with force only for psql 13+
	if db.featureSupported(featureForceDropDatabase) {
		dropWithForce = "WITH ( FORCE )"
	}

	sql := fmt.Sprintf("DROP DATABASE %s %s", pq.QuoteIdentifier(dbName), dropWithForce)
	if _, err := db.Exec(sql); err != nil {
		return fmt.Errorf("error dropping database: %w", err)
	}

	d.SetId("")

	// Returning err even if it's nil so defer func can modify it.
	return err
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

	if db.featureSupported(featureDBAllowConnections) {
		var dbAllowConns bool
		dbSQL := fmt.Sprintf(dbSQLFmt, "d.datallowconn")
		err = db.QueryRow(dbSQL, dbId).Scan(&dbAllowConns)
		if err != nil {
			return fmt.Errorf("error reading ALLOW_CONNECTIONS property for DATABASE: %w", err)
		}

		d.Set(dbAllowConnsAttr, dbAllowConns)
	}

	if db.featureSupported(featureDBIsTemplate) {
		var dbIsTemplate bool
		dbSQL := fmt.Sprintf(dbSQLFmt, "d.datistemplate")
		err = db.QueryRow(dbSQL, dbId).Scan(&dbIsTemplate)
		if err != nil {
			return fmt.Errorf("error reading IS_TEMPLATE property for DATABASE: %w", err)
		}

		d.Set(dbIsTemplateAttr, dbIsTemplate)
	}

	return nil
}

func resourceCockroachSQLDatabaseUpdate(db *DBConnection, d *schema.ResourceData) error {
	if err := setDBName(db, d); err != nil {
		return err
	}

	if err := setAlterOwnership(db, d); err != nil {
		return err
	}

	if err := setDBOwner(db, d); err != nil {
		return err
	}

	if err := setDBTablespace(db, d); err != nil {
		return err
	}

	if err := setDBAllowConns(db, d); err != nil {
		return err
	}

	if err := setDBIsTemplate(db, d); err != nil {
		return err
	}

	// Empty values: ALTER DATABASE name RESET configuration_parameter;

	return resourceCockroachSQLDatabaseReadImpl(db, d)
}

func setDBName(db QueryAble, d *schema.ResourceData) error {
	if !d.HasChange(dbNameAttr) {
		return nil
	}

	oraw, nraw := d.GetChange(dbNameAttr)
	o := oraw.(string)
	n := nraw.(string)
	if n == "" {
		return errors.New("error setting database name to an empty string")
	}

	sql := fmt.Sprintf("ALTER DATABASE %s RENAME TO %s", pq.QuoteIdentifier(o), pq.QuoteIdentifier(n))
	if _, err := db.Exec(sql); err != nil {
		return fmt.Errorf("error updating database name: %w", err)
	}
	d.SetId(n)

	return nil
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

	//needed in order to set the owner of the db if the connection user is not a superuser
	ownerGranted, err := grantRoleMembership(db, owner, currentUser)
	if err != nil {
		return err
	}
	if ownerGranted {
		defer func() {
			_, err = revokeRoleMembership(db, owner, currentUser)
		}()
	}

	dbName := d.Get(dbNameAttr).(string)

	sql := fmt.Sprintf("ALTER DATABASE %s OWNER TO %s", pq.QuoteIdentifier(dbName), pq.QuoteIdentifier(owner))
	if _, err := db.Exec(sql); err != nil {
		return fmt.Errorf("error updating database OWNER: %w", err)
	}

	return err
}

func setAlterOwnership(db *DBConnection, d *schema.ResourceData) error {
	if !d.HasChange(dbOwnerAttr) && !d.HasChange(dbAlterObjectOwnership) {
		return nil
	}
	owner := d.Get(dbOwnerAttr).(string)
	if owner == "" {
		return nil
	}

	alterOwnership := d.Get(dbAlterObjectOwnership).(bool)
	if !alterOwnership {
		return nil
	}
	currentUser := db.client.config.getDatabaseUsername()

	dbName := d.Get(dbNameAttr).(string)

	currentOwner, err := getDatabaseOwner(db, dbName)
	if err != nil {
		return fmt.Errorf("error getting current database OWNER: %w", err)
	}

	newOwner := d.Get(dbOwnerAttr).(string)

	if currentOwner == newOwner {
		return nil
	}

	currentOwnerGranted, err := grantRoleMembership(db, currentOwner, currentUser)
	if err != nil {
		return err
	}
	if currentOwnerGranted {
		defer func() {
			_, err = revokeRoleMembership(db, currentOwner, currentUser)
		}()
	}
	sql := fmt.Sprintf("REASSIGN OWNED BY %s TO %s", pq.QuoteIdentifier(currentOwner), pq.QuoteIdentifier(newOwner))
	if _, err := db.Exec(sql); err != nil {
		return fmt.Errorf("error reassigning objects owned by '%s': %w", currentOwner, err)
	}

	return nil
}

func setDBTablespace(db QueryAble, d *schema.ResourceData) error {
	if !d.HasChange(dbTablespaceAttr) {
		return nil
	}

	tbspName := d.Get(dbTablespaceAttr).(string)
	dbName := d.Get(dbNameAttr).(string)
	var sql string
	if tbspName == "" || strings.ToUpper(tbspName) == "DEFAULT" {
		sql = fmt.Sprintf("ALTER DATABASE %s RESET TABLESPACE", pq.QuoteIdentifier(dbName))
	} else {
		sql = fmt.Sprintf("ALTER DATABASE %s SET TABLESPACE %s", pq.QuoteIdentifier(dbName), pq.QuoteIdentifier(tbspName))
	}

	if _, err := db.Exec(sql); err != nil {
		return fmt.Errorf("error updating database TABLESPACE: %w", err)
	}

	return nil
}

func setDBAllowConns(db *DBConnection, d *schema.ResourceData) error {
	if !d.HasChange(dbAllowConnsAttr) {
		return nil
	}

	if !db.featureSupported(featureDBAllowConnections) {
		return fmt.Errorf("CockroachSQL client is talking with a server (%q) that does not support database ALLOW_CONNECTIONS", db.version.String())
	}

	allowConns := d.Get(dbAllowConnsAttr).(bool)
	dbName := d.Get(dbNameAttr).(string)
	sql := fmt.Sprintf("ALTER DATABASE %s ALLOW_CONNECTIONS %t", pq.QuoteIdentifier(dbName), allowConns)
	if _, err := db.Exec(sql); err != nil {
		return fmt.Errorf("error updating database ALLOW_CONNECTIONS: %w", err)
	}

	return nil
}

func setDBIsTemplate(db *DBConnection, d *schema.ResourceData) error {
	if !d.HasChange(dbIsTemplateAttr) {
		return nil
	}

	if err := doSetDBIsTemplate(db, d.Get(dbNameAttr).(string), d.Get(dbIsTemplateAttr).(bool)); err != nil {
		return fmt.Errorf("error updating database IS_TEMPLATE: %w", err)
	}

	return nil
}

func doSetDBIsTemplate(db *DBConnection, dbName string, isTemplate bool) error {
	if !db.featureSupported(featureDBIsTemplate) {
		return fmt.Errorf("CockroachSQL client is talking with a server (%q) that does not support database IS_TEMPLATE", db.version.String())
	}

	sql := fmt.Sprintf("ALTER DATABASE %s IS_TEMPLATE %t", pq.QuoteIdentifier(dbName), isTemplate)
	if _, err := db.Exec(sql); err != nil {
		return fmt.Errorf("error updating database IS_TEMPLATE: %w", err)
	}

	return nil
}

func terminateBConnections(db *DBConnection, dbName string) error {
	// CockroachDB does not support pg_terminate_backend in a way that is
	// consistent with CockroachSQL. Concurrency is handled by its serializable
	// transaction isolation level.
	return nil
}
