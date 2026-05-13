package cockroachsql

import (
	"database/sql"
	"fmt"
	"log"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"

	// Use CockroachSQL as SQL driver
	"github.com/lib/pq"
)

func resourceCockroachSQLDefaultPrivileges() *schema.Resource {
	return &schema.Resource{
		Create: ResourceFunc(resourceCockroachSQLDefaultPrivilegesCreate),
		Update: ResourceFunc(resourceCockroachSQLDefaultPrivilegesCreate),
		Read:   ResourceFunc(resourceCockroachSQLDefaultPrivilegesRead),
		Delete: ResourceFunc(resourceCockroachSQLDefaultPrivilegesDelete),

		Schema: map[string]*schema.Schema{
			"role": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The name of the role to which grant default privileges on",
			},
			"database": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The database to grant default privileges for this role",
			},
			"owner": {
				Type:          schema.TypeString,
				Optional:      true,
				ForceNew:      true,
				ConflictsWith: []string{"for_all_roles"},
				Description:   "Target role for which to alter default privileges.",
			},
			"for_all_roles": {
				Type:          schema.TypeBool,
				Optional:      true,
				Default:       false,
				ForceNew:      true,
				ConflictsWith: []string{"owner"},
				Description:   "If true, alter default privileges for all roles.",
			},
			"schema": {
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Description: "The database schema to set default privileges for this role",
			},
			"object_type": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
				ValidateFunc: validation.StringInSlice([]string{
					"table",
					"sequence",
					"function",
					"routine",
					"type",
					"schema",
				}, false),
				Description: "The CockroachSQL object type to set the default privileges on (one of: table, sequence, function, routine, type, schema)",
			},
			"privileges": {
				Type:        schema.TypeSet,
				Required:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Set:         schema.HashString,
				Description: "The list of privileges to apply as default privileges",
			},
			"with_grant_option": {
				Type:        schema.TypeBool,
				Optional:    true,
				ForceNew:    true,
				Default:     false,
				Description: "Permit the grant recipient to grant it to others",
			},
		},
	}
}

func resourceCockroachSQLDefaultPrivilegesRead(db *DBConnection, d *schema.ResourceData) error {
	pgSchema := d.Get("schema").(string)
	objectType := d.Get("object_type").(string)
	database := d.Get("database").(string)

	if pgSchema != "" && objectType == "schema" && !db.featureSupported(featurePrivilegesOnSchemas) {
		return fmt.Errorf(
			"changing default privileges for schemas is not supported for this CockroachSQL version (%s)",
			db.version,
		)
	}

	if objectType == "routine" && !db.featureSupported(featureRoutine) {
		return fmt.Errorf(
			"object type ROUTINE is not supported for this CockroachSQL version (%s)",
			db.version,
		)
	}

	// Connect to the target database
	targetClient := db.client.config.NewClient(database)
	targetConn, err := targetClient.Connect()
	if err != nil {
		// If DB doesn't exist, the resource doesn't exist
		if strings.Contains(err.Error(), "does not exist") {
			d.SetId("")
			return nil
		}
		return err
	}

	return readRoleDefaultPrivileges(targetConn.DB, d)
}

func resourceCockroachSQLDefaultPrivilegesCreate(db *DBConnection, d *schema.ResourceData) error {
	pgSchema := d.Get("schema").(string)
	objectType := d.Get("object_type").(string)
	forAllRoles := d.Get("for_all_roles").(bool)
	owner := d.Get("owner").(string)
	database := d.Get("database").(string)

	if !forAllRoles && owner == "" {
		return fmt.Errorf("must specify either `owner` or `for_all_roles = true`")
	}

	if pgSchema != "" && objectType == "schema" {
		if !db.featureSupported(featurePrivilegesOnSchemas) {
			return fmt.Errorf(
				"changing default privileges for schemas is not supported for this CockroachSQL version (%s)",
				db.version,
			)
		}
		return fmt.Errorf("cannot specify `schema` when `object_type` is `schema`")
	}

	if objectType == "routine" && !db.featureSupported(featureRoutine) {
		return fmt.Errorf(
			"object type ROUTINE is not supported for this CockroachSQL version (%s)",
			db.version,
		)
	}

	if d.Get("with_grant_option").(bool) && strings.ToLower(d.Get("role").(string)) == "public" {
		return fmt.Errorf("with_grant_option cannot be true for role 'public'")
	}

	if err := validatePrivileges(d); err != nil {
		return err
	}

	// Connect to the target database
	conn := db.DB
	if database != db.client.databaseName {
		targetClient := db.client.config.NewClient(database)
		targetConn, err := targetClient.Connect()
		if err != nil {
			return err
		}
		conn = targetConn.DB
	}

	rolesToGrant := []string{}
	if !forAllRoles {
		rolesToGrant = append(rolesToGrant, owner)
	}

	// Needed in order to set the owner of the db if the connection user is not a superuser
	if err := withRolesGranted(conn, rolesToGrant, func() error {

		// Revoke all privileges before granting otherwise reducing privileges will not work.
		if err := revokeRoleDefaultPrivileges(conn, d); err != nil {
			return err
		}

		if err := grantRoleDefaultPrivileges(conn, d); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}

	d.SetId(generateDefaultPrivilegesID(d))

	return readRoleDefaultPrivileges(conn, d)
}

func resourceCockroachSQLDefaultPrivilegesDelete(db *DBConnection, d *schema.ResourceData) error {
	forAllRoles := d.Get("for_all_roles").(bool)
	owner := d.Get("owner").(string)
	pgSchema := d.Get("schema").(string)
	objectType := d.Get("object_type").(string)
	database := d.Get("database").(string)

	if pgSchema != "" && objectType == "schema" && !db.featureSupported(featurePrivilegesOnSchemas) {
		return fmt.Errorf(
			"changing default privileges for schemas is not supported for this CockroachSQL version (%s)",
			db.version,
		)
	}

	// Connect to the target database
	conn := db.DB
	if database != db.client.databaseName {
		targetClient := db.client.config.NewClient(database)
		targetConn, err := targetClient.Connect()
		if err != nil {
			return err
		}
		conn = targetConn.DB
	}

	rolesToGrant := []string{}
	if !forAllRoles {
		rolesToGrant = append(rolesToGrant, owner)
	}

	// Needed in order to set the owner of the db if the connection user is not a superuser
	if err := withRolesGranted(conn, rolesToGrant, func() error {
		return revokeRoleDefaultPrivileges(conn, d)
	}); err != nil {
		return err
	}

	return nil
}

func readRoleDefaultPrivileges(db QueryAble, d *schema.ResourceData) error {
	role := d.Get("role").(string)
	forAllRoles := d.Get("for_all_roles").(bool)
	owner := d.Get("owner").(string)
	pgSchema := d.Get("schema").(string)
	objectType := d.Get("object_type").(string)
	privilegesInput := d.Get("privileges").(*schema.Set).List()

	// CockroachSQL native way to show default privileges is much more reliable
	// than querying pg_catalog and exploding ACL strings which often fail.
	var query string
	if forAllRoles {
		query = "SHOW DEFAULT PRIVILEGES FOR ALL ROLES"
	} else {
		query = fmt.Sprintf("SHOW DEFAULT PRIVILEGES FOR ROLE %s", pq.QuoteIdentifier(owner))
	}

	if pgSchema != "" {
		query += fmt.Sprintf(" IN SCHEMA %s", pq.QuoteIdentifier(pgSchema))
	}

	rows, err := db.Query(query)
	if err != nil {
		return fmt.Errorf("could not read default privileges: %w", err)
	}
	defer func() { _ = rows.Close() }()

	cols, _ := rows.Columns()
	log.Printf("[DEBUG] cockroachsql_default_privileges: found columns %v", cols)
	var privileges []string
	for rows.Next() {
		dest := make([]any, len(cols))
		vals := make([]sql.NullString, len(cols))
		for i := range dest {
			dest[i] = &vals[i]
		}

		if err := rows.Scan(dest...); err != nil {
			return fmt.Errorf("could not scan default privilege row: %w", err)
		}

		m := make(map[string]string)
		for i, colName := range cols {
			if vals[i].Valid {
				m[colName] = vals[i].String
			}
		}

		log.Printf("[DEBUG] cockroachsql_default_privileges: row: %v", m)

		r_object_type := m["object_type"]
		r_grantee := m["grantee"]
		r_privilege_type := m["privilege_type"]

		// Filter for our target object type and grantee
		// Note: CRDB uses plural types like 'tables', 'sequences' in SHOW output
		var targetPlural string
		switch objectType {
		case "schema":
			targetPlural = "schemas"
		case "routine", "function":
			targetPlural = "functions" // CRDB groups these
		default:
			targetPlural = objectType + "s"
		}

		if strings.EqualFold(r_object_type, targetPlural) && strings.EqualFold(strings.Trim(r_grantee, `"`), strings.Trim(role, `"`)) {
			privileges = append(privileges, strings.ToUpper(r_privilege_type))
		}
	}

	// We consider no privileges as "not exists" unless no privileges were provided as input
	if len(privileges) == 0 {
		if len(privilegesInput) != 0 {
			d.SetId("")
			return nil
		}
	}

	privilegesSet := stringSliceToSet(privileges)
	privilegesEqual := resourcePrivilegesEqual(privilegesSet, d)

	if !privilegesEqual {
		d.Set("privileges", privilegesSet)
	}
	d.SetId(generateDefaultPrivilegesID(d))

	return nil
}

func grantRoleDefaultPrivileges(db QueryAble, d *schema.ResourceData) error {
	role := d.Get("role").(string)
	forAllRoles := d.Get("for_all_roles").(bool)
	owner := d.Get("owner").(string)
	pgSchema := d.Get("schema").(string)

	privileges := []string{}
	for _, priv := range d.Get("privileges").(*schema.Set).List() {
		privileges = append(privileges, priv.(string))
	}

	if len(privileges) == 0 {
		return nil
	}

	var forClause string
	if forAllRoles {
		forClause = "FOR ALL ROLES"
	} else {
		forClause = fmt.Sprintf("FOR ROLE %s", pq.QuoteIdentifier(owner))
	}

	var inSchema string
	if pgSchema != "" {
		inSchema = fmt.Sprintf("IN SCHEMA %s", pq.QuoteIdentifier(pgSchema))
	}

	query := fmt.Sprintf("ALTER DEFAULT PRIVILEGES %s %s GRANT %s ON %sS TO %s",
		forClause,
		inSchema,
		strings.Join(privileges, ","),
		strings.ToUpper(d.Get("object_type").(string)),
		pq.QuoteIdentifier(role),
	)

	if d.Get("with_grant_option").(bool) {
		query = query + " WITH GRANT OPTION"
	}

	_, err := db.Exec(query)
	if err != nil {
		return fmt.Errorf("could not alter default privileges (SQL: %s): %w", query, err)
	}

	return nil
}

func revokeRoleDefaultPrivileges(db QueryAble, d *schema.ResourceData) error {
	forAllRoles := d.Get("for_all_roles").(bool)
	owner := d.Get("owner").(string)
	pgSchema := d.Get("schema").(string)

	var forClause string
	if forAllRoles {
		forClause = "FOR ALL ROLES"
	} else {
		forClause = fmt.Sprintf("FOR ROLE %s", pq.QuoteIdentifier(owner))
	}

	var inSchema string
	if pgSchema != "" {
		inSchema = fmt.Sprintf("IN SCHEMA %s", pq.QuoteIdentifier(pgSchema))
	}
	query := fmt.Sprintf(
		"ALTER DEFAULT PRIVILEGES %s %s REVOKE ALL ON %sS FROM %s",
		forClause,
		inSchema,
		strings.ToUpper(d.Get("object_type").(string)),
		pq.QuoteIdentifier(d.Get("role").(string)),
	)

	if _, err := db.Exec(query); err != nil {
		return fmt.Errorf("could not revoke default privileges (SQL: %s): %w", query, err)
	}
	return nil
}

func generateDefaultPrivilegesID(d *schema.ResourceData) string {
	pgSchema := d.Get("schema").(string)
	if pgSchema == "" {
		pgSchema = "noschema"
	}

	owner := d.Get("owner").(string)
	if d.Get("for_all_roles").(bool) {
		owner = "allroles"
	}

	return strings.Join([]string{
		d.Get("role").(string), d.Get("database").(string), pgSchema,
		owner, d.Get("object_type").(string),
	}, "_")
}
