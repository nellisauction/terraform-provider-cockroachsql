package cockroachsql

import (
	"bytes"
	"context"
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
		CreateContext: ResourceFunc(resourceCockroachSQLDatabaseCreate),
		ReadContext:   ResourceFunc(resourceCockroachSQLDatabaseRead),
		UpdateContext: ResourceFunc(resourceCockroachSQLDatabaseUpdate),
		DeleteContext: ResourceFunc(resourceCockroachSQLDatabaseDelete),
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			dbNameAttr: {
				Type:        schema.TypeString,
				Required:    true,
				Description: "The name of the database. Changing it renames the database in place.",
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
				Description: "Character set encoding to use in the new database. CockroachDB only supports UTF8.",
				DiffSuppressFunc: func(_, old, new string, _ *schema.ResourceData) bool {
					return normalizeDBEncoding(old) == normalizeDBEncoding(new)
				},
			},
			dbCollationAttr: {
				Type:             schema.TypeString,
				Optional:         true,
				ForceNew:         true,
				Computed:         true,
				Description:      "Collation order (LC_COLLATE) to use in the new database",
				DiffSuppressFunc: suppressDBLocaleDiff,
			},
			dbCTypeAttr: {
				Type:             schema.TypeString,
				Optional:         true,
				ForceNew:         true,
				Computed:         true,
				Description:      "Character classification (LC_CTYPE) to use in the new database",
				DiffSuppressFunc: suppressDBLocaleDiff,
			},
			dbTablespaceAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Computed:    true,
				Description: "The name of the tablespace that will be associated with the new database. CockroachDB only supports pg_default.",
				DiffSuppressFunc: func(_, old, new string, _ *schema.ResourceData) bool {
					return isDefaultDBTablespace(old) && isDefaultDBTablespace(new)
				},
			},
		},
	}
}

func resourceCockroachSQLDatabaseCreate(ctx context.Context, db *DBConnection, d *schema.ResourceData) error {
	if err := createDatabase(ctx, db, d); err != nil {
		return err
	}

	d.SetId(d.Get(dbNameAttr).(string))

	return resourceCockroachSQLDatabaseReadImpl(ctx, db, d)
}

func createDatabase(ctx context.Context, db *DBConnection, d *schema.ResourceData) error {
	currentUser := db.client.config.getDatabaseUsername()
	owner := d.Get(dbOwnerAttr).(string)

	if owner != "" {
		ownerGranted, err := grantRoleMembership(ctx, db, owner, currentUser)
		if err != nil {
			return err
		}
		if ownerGranted {
			defer func() {
				cleanupCtx, cancel := cleanupContext(ctx)
				defer cancel()
				_, _ = revokeRoleMembership(cleanupCtx, db, owner, currentUser)
			}()
		}
	}

	query, err := createDatabaseQuery(d, currentUser)
	if err != nil {
		return err
	}
	dbName := d.Get(dbNameAttr).(string)
	if _, err := db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("error creating database %q (SQL: %s): %w", dbName, query, err)
	}

	return nil
}

// CockroachDB only accepts 'C' and 'C.UTF-8' for LC_COLLATE/LC_CTYPE and
// always reports en_US.utf8 back, so all of these are the same locale.
var crdbDefaultDBLocales = map[string]bool{
	"default":    true,
	"c":          true,
	"c.utf8":     true,
	"en_us.utf8": true,
}

func normalizeDBEncoding(v string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", "_", "").Replace(strings.TrimSpace(v)))
}

func normalizeDBLocale(v string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(v)), "-", "")
}

func suppressDBLocaleDiff(_, old, new string, _ *schema.ResourceData) bool {
	o, n := normalizeDBLocale(old), normalizeDBLocale(new)
	return o == n || (crdbDefaultDBLocales[o] && crdbDefaultDBLocales[n])
}

func isDefaultDBTablespace(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "default", "pg_default":
		return true
	}
	return false
}

func createDatabaseQuery(d *schema.ResourceData, currentUser string) (string, error) {
	b := bytes.NewBufferString("CREATE DATABASE ")
	fmt.Fprint(b, pq.QuoteIdentifier(d.Get(dbNameAttr).(string)))

	if v, ok := d.GetOk(dbEncodingAttr); ok {
		fmt.Fprintf(b, " ENCODING '%s'", pqQuoteLiteral(v.(string)))
	}

	for _, opt := range []struct{ attr, keyword string }{
		{dbCollationAttr, "LC_COLLATE"},
		{dbCTypeAttr, "LC_CTYPE"},
	} {
		v := d.Get(opt.attr).(string)
		switch normalizeDBLocale(v) {
		case "", "default", "en_us.utf8":
			continue
		}
		fmt.Fprintf(b, " %s '%s'", opt.keyword, pqQuoteLiteral(v))
	}

	if v := d.Get(dbTablespaceAttr).(string); !isDefaultDBTablespace(v) {
		return "", fmt.Errorf("CockroachDB does not support tablespaces; %s must be unset or pg_default, got %q", dbTablespaceAttr, v)
	}

	owner := currentUser
	if v, ok := d.GetOk(dbOwnerAttr); ok {
		owner = v.(string)
	}
	fmt.Fprint(b, " OWNER ", pq.QuoteIdentifier(owner))

	return b.String(), nil
}

func resourceCockroachSQLDatabaseDelete(ctx context.Context, db *DBConnection, d *schema.ResourceData) error {
	currentUser := db.client.config.getDatabaseUsername()
	owner := d.Get(dbOwnerAttr).(string)

	if owner != "" {
		ownerGranted, err := grantRoleMembership(ctx, db, owner, currentUser)
		if err != nil {
			return err
		}
		if ownerGranted {
			defer func() {
				cleanupCtx, cancel := cleanupContext(ctx)
				defer cancel()
				_, _ = revokeRoleMembership(cleanupCtx, db, owner, currentUser)
			}()
		}
	}

	dbName := d.Get(dbNameAttr).(string)

	if err := terminateBConnections(ctx, db, dbName); err != nil {
		return err
	}

	query := fmt.Sprintf("DROP DATABASE %s", pq.QuoteIdentifier(dbName))
	if _, err := db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("error dropping database: %w", err)
	}

	d.SetId("")

	return nil
}

// getDBName returns the database to look up. It prefers the name in state
// over the ID: a rename updates the ID, but callers such as the Pulumi bridge
// keep the original ID, so the ID can be stale. On import the state is empty
// and the ID is the database name.
func getDBName(d *schema.ResourceData) string {
	if name := d.Get(dbNameAttr).(string); name != "" {
		return name
	}
	return d.Id()
}

func resourceCockroachSQLDatabaseExists(ctx context.Context, db *DBConnection, d *schema.ResourceData) (bool, error) {
	return dbExists(ctx, db, getDBName(d))
}

func resourceCockroachSQLDatabaseRead(ctx context.Context, db *DBConnection, d *schema.ResourceData) error {
	exists, err := resourceCockroachSQLDatabaseExists(ctx, db, d)
	if err != nil {
		return err
	}
	if !exists {
		d.SetId("")
		return nil
	}

	return resourceCockroachSQLDatabaseReadImpl(ctx, db, d)
}

func resourceCockroachSQLDatabaseReadImpl(ctx context.Context, db *DBConnection, d *schema.ResourceData) error {
	dbId := getDBName(d)
	var dbName, ownerName string
	err := db.QueryRowContext(ctx, "SELECT d.datname, pg_catalog.pg_get_userbyid(d.datdba) from pg_database d WHERE datname=$1", dbId).Scan(&dbName, &ownerName)
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
	err = db.QueryRowContext(ctx, dbSQL, dbId).
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

	d.SetId(dbName)
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

func resourceCockroachSQLDatabaseUpdate(ctx context.Context, db *DBConnection, d *schema.ResourceData) error {
	if err := renameDB(ctx, db, d); err != nil {
		return err
	}

	if err := setDBOwner(ctx, db, d); err != nil {
		return err
	}

	return resourceCockroachSQLDatabaseReadImpl(ctx, db, d)
}

func renameDB(ctx context.Context, db *DBConnection, d *schema.ResourceData) error {
	if !d.HasChange(dbNameAttr) {
		return nil
	}

	oldRaw, newRaw := d.GetChange(dbNameAttr)
	oldName, newName := oldRaw.(string), newRaw.(string)

	query := fmt.Sprintf("ALTER DATABASE %s RENAME TO %s", pq.QuoteIdentifier(oldName), pq.QuoteIdentifier(newName))
	if _, err := db.ExecContext(ctx, query); err != nil {
		// Keep the old name in state: Read looks the database up by name.
		d.Partial(true)
		return fmt.Errorf("error renaming database %q to %q: %w", oldName, newName, err)
	}

	d.SetId(newName)

	return nil
}

func setDBOwner(ctx context.Context, db *DBConnection, d *schema.ResourceData) error {
	if !d.HasChange(dbOwnerAttr) {
		return nil
	}

	owner := d.Get(dbOwnerAttr).(string)
	if owner == "" {
		return nil
	}
	currentUser := db.client.config.getDatabaseUsername()

	ownerGranted, err := grantRoleMembership(ctx, db, owner, currentUser)
	if err != nil {
		return err
	}
	if ownerGranted {
		defer func() {
			cleanupCtx, cancel := cleanupContext(ctx)
			defer cancel()
			_, _ = revokeRoleMembership(cleanupCtx, db, owner, currentUser)
		}()
	}

	dbName := d.Get(dbNameAttr).(string)

	query := fmt.Sprintf("ALTER DATABASE %s OWNER TO %s", pq.QuoteIdentifier(dbName), pq.QuoteIdentifier(owner))
	if _, err := db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("error updating database OWNER: %w", err)
	}

	return nil
}

func terminateBConnections(ctx context.Context, db *DBConnection, dbName string) error {
	// CockroachDB handles concurrency via its serializable transaction isolation level.
	// Terminating connections is not explicitly required for DROP DATABASE.
	return nil
}
