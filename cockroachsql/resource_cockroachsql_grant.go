package cockroachsql

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"

	// Use CockroachSQL as SQL driver
	"github.com/lib/pq"
)

var allowedObjectTypes = []string{
	"database",
	"function",
	"procedure",
	"routine",
	"schema",
	"sequence",
	"table",
	"foreign_data_wrapper",
	"foreign_server",
	"column",
}

type ResourceSchemeGetter func(string) any

func resourceCockroachSQLGrant() *schema.Resource {
	return &schema.Resource{
		Create: ResourceFunc(resourceCockroachSQLGrantCreate),
		Update: ResourceFunc(resourceCockroachSQLGrantUpdate),
		Read:   ResourceFunc(resourceCockroachSQLGrantRead),
		Delete: ResourceFunc(resourceCockroachSQLGrantDelete),

		Schema: map[string]*schema.Schema{
			"role": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The name of the role to grant privileges on",
			},
			"database": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The database to grant privileges on for this role",
			},
			"schema": {
				Type:        schema.TypeString,
				Optional:    true,
				ForceNew:    true,
				Description: "The database schema to grant privileges on for this role",
			},
			"object_type": {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validation.StringInSlice(allowedObjectTypes, false),
				Description:  "The CockroachSQL object type to grant the privileges on (one of: " + strings.Join(allowedObjectTypes, ", ") + ")",
			},
			"objects": {
				Type:        schema.TypeSet,
				Optional:    true,
				ForceNew:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Set:         schema.HashString,
				Description: "The specific objects to grant privileges on for this role (empty means all objects of the requested type)",
			},
			"columns": {
				Type:        schema.TypeSet,
				Optional:    true,
				ForceNew:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Set:         schema.HashString,
				Description: "The specific columns to grant privileges on for this role",
			},
			"privileges": {
				Type:        schema.TypeSet,
				Required:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Set:         schema.HashString,
				Description: "The list of privileges to grant",
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

func resourceCockroachSQLGrantRead(db *DBConnection, d *schema.ResourceData) error {
	if err := validateFeatureSupport(db, d); err != nil {
		return fmt.Errorf("feature is not supported: %v", err)
	}

	database := d.Get("database").(string)
	targetClient := db.client.config.NewClient(database)
	targetConn, err := targetClient.Connect()
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			d.SetId("")
			return nil
		}
		return err
	}

	exists, err := checkRoleDBSchemaExists(targetConn.DB, d)
	if err != nil {
		return err
	}
	if !exists {
		d.SetId("")
		return nil
	}
	d.SetId(generateGrantID(d))

	return readRolePrivileges(targetConn.DB, d)
}

func resourceCockroachSQLGrantCreate(db *DBConnection, d *schema.ResourceData) error {
	return resourceCockroachSQLGrantCreateOrUpdate(db, d, false)
}

func resourceCockroachSQLGrantUpdate(db *DBConnection, d *schema.ResourceData) error {
	return resourceCockroachSQLGrantCreateOrUpdate(db, d, true)
}

func resourceCockroachSQLGrantCreateOrUpdate(db *DBConnection, d *schema.ResourceData, usePrevious bool) error {
	if err := validateFeatureSupport(db, d); err != nil {
		return fmt.Errorf("feature is not supported: %v", err)
	}

	database := d.Get("database").(string)
	targetClient := db.client.config.NewClient(database)
	targetConn, err := targetClient.Connect()
	if err != nil {
		return err
	}

	objectType := d.Get("object_type").(string)
	schemaName := d.Get("schema").(string)
	if schemaName != "" && !sliceContainsStr([]string{"database", "foreign_data_wrapper", "foreign_server"}, objectType) {
		_, _ = targetConn.Exec(fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", pq.QuoteIdentifier(schemaName)))
	}

	if d.Get("schema").(string) == "" && !sliceContainsStr([]string{"database", "foreign_data_wrapper", "foreign_server"}, objectType) {
		return fmt.Errorf("parameter 'schema' is mandatory for cockroachsql_grant resource")
	}
	if d.Get("objects").(*schema.Set).Len() > 0 && (objectType == "database" || objectType == "schema") {
		return fmt.Errorf("cannot specify `objects` when `object_type` is `database` or `schema`")
	}
	if d.Get("columns").(*schema.Set).Len() > 0 && (objectType != "column") {
		return fmt.Errorf("cannot specify `columns` when `object_type` is not `column`")
	}
	if d.Get("columns").(*schema.Set).Len() == 0 && (objectType == "column") {
		return fmt.Errorf("must specify `columns` when `object_type` is `column`")
	}
	if d.Get("privileges").(*schema.Set).Len() != 1 && (objectType == "column") {
		return fmt.Errorf("must specify exactly 1 `privileges` when `object_type` is `column`")
	}
	if (d.Get("objects").(*schema.Set).Len() != 1) && (objectType == "column") {
		return fmt.Errorf("must specify exactly 1 table in the `objects` field when `object_type` is `column`")
	}
	if d.Get("objects").(*schema.Set).Len() != 1 && (objectType == "foreign_data_wrapper" || objectType == "foreign_server") {
		return fmt.Errorf("one element must be specified in `objects` when `object_type` is `foreign_data_wrapper` or `foreign_server`")
	}
	if err := validatePrivileges(d); err != nil {
		return err
	}

	owners, err := getRolesToGrant(targetConn, d)
	if err != nil {
		return err
	}
	if err := withRolesGranted(targetConn, owners, func() error {
		if err := revokeRolePrivileges(targetConn, d, usePrevious); err != nil {
			return err
		}
		if err := grantRolePrivileges(targetConn, d); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}

	d.SetId(generateGrantID(d))

	return readRolePrivileges(targetConn.DB, d)
}

func resourceCockroachSQLGrantDelete(db *DBConnection, d *schema.ResourceData) error {
	if err := validateFeatureSupport(db, d); err != nil {
		return fmt.Errorf("feature is not supported: %v", err)
	}

	database := d.Get("database").(string)
	targetClient := db.client.config.NewClient(database)
	targetConn, err := targetClient.Connect()
	if err != nil {
		return err
	}

	owners, err := getRolesToGrant(targetConn, d)
	if err != nil {
		return err
	}

	if err := withRolesGranted(targetConn, owners, func() error {
		return revokeRolePrivileges(targetConn, d, false)
	}); err != nil {
		return err
	}

	return nil
}

func readRolePrivileges(db QueryAble, d *schema.ResourceData) error {
	role := d.Get("role").(string)
	objectType := strings.ToUpper(d.Get("object_type").(string))
	schemaName := d.Get("schema").(string)
	objects := d.Get("objects").(*schema.Set)
	database := d.Get("database").(string)

	objectGrants := make(map[string]*schema.Set)
	allRelevantObjects := make(map[string]bool)

	if objects.Len() == 0 && schemaName != "" {
		objQuery := ""
		switch objectType {
		case "TABLE":
			objQuery = fmt.Sprintf("SELECT table_name FROM information_schema.tables WHERE table_schema = %s AND table_type = 'BASE TABLE'", pq.QuoteLiteral(schemaName))
		case "SEQUENCE":
			objQuery = fmt.Sprintf("SELECT sequence_name FROM information_schema.sequences WHERE sequence_schema = %s", pq.QuoteLiteral(schemaName))
		case "FUNCTION", "PROCEDURE", "ROUTINE":
			objQuery = fmt.Sprintf("SELECT routine_name FROM information_schema.routines WHERE routine_schema = %s", pq.QuoteLiteral(schemaName))
		}
		if objQuery != "" {
			objRows, err := db.Query(objQuery)
			if err == nil {
				defer func() { _ = objRows.Close() }()
				for objRows.Next() {
					var name string
					if err := objRows.Scan(&name); err == nil {
						if !strings.HasPrefix(name, "crdb_internal") {
							allRelevantObjects[name] = true
						}
					}
				}
			}
		}
	}

	allPrivs := allowedPrivileges[d.Get("object_type").(string)]

	grantees := []string{role, "public"}
	for _, granteeName := range grantees {
		query := fmt.Sprintf("SHOW GRANTS FOR %s", pq.QuoteIdentifier(granteeName))
		rows, err := db.Query(query)
		if err != nil {
			continue
		}
		cols, _ := rows.Columns()
		for rows.Next() {
			dest := make([]any, len(cols))
			vals := make([]sql.NullString, len(cols))
			for i := range dest {
				dest[i] = &vals[i]
			}
			if err := rows.Scan(dest...); err != nil {
				_ = rows.Close()
				return err
			}
			m := make(map[string]string)
			for i, colName := range cols {
				if vals[i].Valid {
					m[colName] = vals[i].String
				}
			}

			r_grantee := m["grantee"]
			r_privilege := strings.ToUpper(m["privilege_type"])
			r_obj_type := strings.ToUpper(m["object_type"])
			r_schema := m["schema_name"]
			r_db := m["database_name"]
			r_name := m["name"]
			if r_name == "" {
				r_name = m["table_name"]
			}
			if r_name == "" {
				r_name = m["routine_signature"]
			}

			if !strings.EqualFold(strings.Trim(r_grantee, `"`), strings.Trim(granteeName, `"`)) {
				continue
			}
			if database != "" && r_db != "" && !strings.EqualFold(r_db, database) {
				continue
			}

			matchType := false
			switch objectType {
			case "TABLE":
				matchType = (r_obj_type == "TABLE" || r_obj_type == "TABLES")
			case "SCHEMA":
				matchType = (r_obj_type == "SCHEMA" || r_obj_type == "SCHEMAS")
			case "DATABASE":
				matchType = (r_obj_type == "DATABASE" || r_obj_type == "DATABASES")
			case "FUNCTION", "PROCEDURE", "ROUTINE":
				matchType = (r_obj_type == "FUNCTION" || r_obj_type == "FUNCTIONS" || r_obj_type == "ROUTINE" || r_obj_type == "ROUTINES")
			case "SEQUENCE":
				matchType = (r_obj_type == "SEQUENCE" || r_obj_type == "SEQUENCES")
			default:
				matchType = strings.HasPrefix(r_obj_type, objectType)
			}
			if !matchType {
				continue
			}
			if schemaName != "" && r_schema != "" && !strings.EqualFold(r_schema, schemaName) {
				continue
			}

			clean_r_name := strings.Trim(r_name, `"`)
			if _, ok := objectGrants[clean_r_name]; !ok {
				objectGrants[clean_r_name] = schema.NewSet(schema.HashString, []any{})
			}
			if r_privilege == "ALL" {
				for _, p := range allPrivs {
					if p != "ALL" {
						objectGrants[clean_r_name].Add(p)
					}
				}
			} else {
				objectGrants[clean_r_name].Add(r_privilege)
			}
		}
		_ = rows.Close()
	}

	grantedSet := schema.NewSet(schema.HashString, []any{})
	if objects.Len() > 0 {
		first := true
		for _, obj := range objects.List() {
			objName := obj.(string)
			var foundSet *schema.Set
			for k, v := range objectGrants {
				if strings.EqualFold(k, objName) {
					foundSet = v
					break
				}
				if strings.Contains(k, "(") {
					norm := func(s string) string {
						s = strings.ToLower(strings.ReplaceAll(s, " ", ""))
						s = strings.ReplaceAll(s, "character", "char")
						s = strings.ReplaceAll(s, "varying", "")
						s = strings.ReplaceAll(s, "integer", "int")
						s = strings.ReplaceAll(s, "boolean", "bool")
						return s
					}
					if norm(k) == norm(objName) {
						foundSet = v
						break
					}
				}
			}
			if foundSet == nil {
				grantedSet = schema.NewSet(schema.HashString, []any{})
				break
			}
			if first {
				grantedSet = foundSet
				first = false
			} else {
				grantedSet = grantedSet.Intersection(foundSet)
			}
		}
	} else if len(allRelevantObjects) > 0 {
		first := true
		for objName := range allRelevantObjects {
			foundSet := objectGrants[objName]
			if foundSet == nil {
				grantedSet = schema.NewSet(schema.HashString, []any{})
				break
			}
			if first {
				grantedSet = foundSet
				first = false
			} else {
				grantedSet = grantedSet.Intersection(foundSet)
			}
		}
	} else {
		for _, v := range objectGrants {
			grantedSet = v
			break
		}
	}

	if !resourcePrivilegesEqual(grantedSet, d) {
		return d.Set("privileges", grantedSet)
	}
	return nil
}

func createGrantQuery(d *schema.ResourceData, privileges []string) string {
	var query string
	switch strings.ToUpper(d.Get("object_type").(string)) {
	case "DATABASE":
		query = fmt.Sprintf("GRANT %s ON DATABASE %s TO %s", strings.Join(privileges, ","), pq.QuoteIdentifier(d.Get("database").(string)), pq.QuoteIdentifier(d.Get("role").(string)))
	case "SCHEMA":
		query = fmt.Sprintf("GRANT %s ON SCHEMA %s TO %s", strings.Join(privileges, ","), pq.QuoteIdentifier(d.Get("schema").(string)), pq.QuoteIdentifier(d.Get("role").(string)))
	case "FOREIGN_DATA_WRAPPER":
		fdwName := d.Get("objects").(*schema.Set).List()[0]
		query = fmt.Sprintf("GRANT %s ON FOREIGN DATA WRAPPER %s TO %s", strings.Join(privileges, ","), pq.QuoteIdentifier(fdwName.(string)), pq.QuoteIdentifier(d.Get("role").(string)))
	case "FOREIGN_SERVER":
		srvName := d.Get("objects").(*schema.Set).List()[0]
		query = fmt.Sprintf("GRANT %s ON FOREIGN SERVER %s TO %s", strings.Join(privileges, ","), pq.QuoteIdentifier(srvName.(string)), pq.QuoteIdentifier(d.Get("role").(string)))
	case "COLUMN":
		objects := d.Get("objects").(*schema.Set)
		query = fmt.Sprintf("GRANT %s (%s) ON TABLE %s TO %s", strings.Join(privileges, ","), setToPgIdentListWithoutSchema(d.Get("columns").(*schema.Set)), setToPgIdentList(d.Get("schema").(string), objects), pq.QuoteIdentifier(d.Get("role").(string)))
	case "TABLE", "SEQUENCE", "FUNCTION", "PROCEDURE", "ROUTINE":
		objects := d.Get("objects").(*schema.Set)
		if objects.Len() > 0 {
			query = fmt.Sprintf("GRANT %s ON %s %s TO %s", strings.Join(privileges, ","), strings.ToUpper(d.Get("object_type").(string)), setToPgIdentList(d.Get("schema").(string), objects), pq.QuoteIdentifier(d.Get("role").(string)))
		} else {
			query = fmt.Sprintf("GRANT %s ON ALL %sS IN SCHEMA %s TO %s", strings.Join(privileges, ","), strings.ToUpper(d.Get("object_type").(string)), pq.QuoteIdentifier(d.Get("schema").(string)), pq.QuoteIdentifier(d.Get("role").(string)))
		}
	}
	if d.Get("with_grant_option").(bool) {
		query += " WITH GRANT OPTION"
	}
	return query
}

func createRevokeQuery(getter ResourceSchemeGetter) string {
	var query string
	switch strings.ToUpper(getter("object_type").(string)) {
	case "DATABASE":
		query = fmt.Sprintf("REVOKE ALL PRIVILEGES ON DATABASE %s FROM %s", pq.QuoteIdentifier(getter("database").(string)), pq.QuoteIdentifier(getter("role").(string)))
	case "SCHEMA":
		query = fmt.Sprintf("REVOKE ALL PRIVILEGES ON SCHEMA %s FROM %s", pq.QuoteIdentifier(getter("schema").(string)), pq.QuoteIdentifier(getter("role").(string)))
	case "FOREIGN_DATA_WRAPPER":
		fdwName := getter("objects").(*schema.Set).List()[0]
		query = fmt.Sprintf("REVOKE ALL PRIVILEGES ON FOREIGN DATA WRAPPER %s FROM %s", pq.QuoteIdentifier(fdwName.(string)), pq.QuoteIdentifier(getter("role").(string)))
	case "FOREIGN_SERVER":
		srvName := getter("objects").(*schema.Set).List()[0]
		query = fmt.Sprintf("REVOKE ALL PRIVILEGES ON FOREIGN SERVER %s FROM %s", pq.QuoteIdentifier(srvName.(string)), pq.QuoteIdentifier(getter("role").(string)))
	case "COLUMN":
		objects := getter("objects").(*schema.Set)
		columns := getter("columns").(*schema.Set)
		privs := getter("privileges").(*schema.Set)
		if privs.Len() == 0 || columns.Len() == 0 {
			query = "SELECT NULL"
		} else {
			query = fmt.Sprintf("REVOKE %s (%s) ON TABLE %s FROM %s", setToPgIdentSimpleList(privs), setToPgIdentListWithoutSchema(columns), setToPgIdentList(getter("schema").(string), objects), pq.QuoteIdentifier(getter("role").(string)))
		}
	case "TABLE", "SEQUENCE", "FUNCTION", "PROCEDURE", "ROUTINE":
		objects := getter("objects").(*schema.Set)
		privs := getter("privileges").(*schema.Set)
		if objects.Len() > 0 {
			if privs.Len() > 0 {
				query = fmt.Sprintf("REVOKE %s ON %s %s FROM %s", setToPgIdentSimpleList(privs), strings.ToUpper(getter("object_type").(string)), setToPgIdentList(getter("schema").(string), objects), pq.QuoteIdentifier(getter("role").(string)))
			} else {
				query = fmt.Sprintf("REVOKE ALL PRIVILEGES ON %s %s FROM %s", strings.ToUpper(getter("object_type").(string)), setToPgIdentList(getter("schema").(string), objects), pq.QuoteIdentifier(getter("role").(string)))
			}
		} else {
			query = fmt.Sprintf("REVOKE ALL PRIVILEGES ON ALL %sS IN SCHEMA %s FROM %s", strings.ToUpper(getter("object_type").(string)), pq.QuoteIdentifier(getter("schema").(string)), pq.QuoteIdentifier(getter("role").(string)))
		}
	}
	return query
}

func grantRolePrivileges(db QueryAble, d *schema.ResourceData) error {
	privs := []string{}
	for _, p := range d.Get("privileges").(*schema.Set).List() {
		privs = append(privs, p.(string))
	}
	if len(privs) == 0 {
		return nil
	}
	_, err := db.Exec(createGrantQuery(d, privs))
	return err
}

func revokeRolePrivileges(db QueryAble, d *schema.ResourceData, usePrevious bool) error {
	getter := d.Get
	if usePrevious {
		getter = func(name string) any {
			if d.HasChange(name) {
				old, _ := d.GetChange(name)
				return old
			}
			return d.Get(name)
		}
	}
	query := createRevokeQuery(getter)
	if query == "" {
		return nil
	}
	_, err := db.Exec(query)
	return err
}

func checkRoleDBSchemaExists(db QueryAble, d *schema.ResourceData) (bool, error) {
	database := d.Get("database").(string)
	exists, err := dbExists(db, database)
	if err != nil || !exists {
		return false, err
	}
	role := d.Get("role").(string)
	if role != publicRole {
		exists, err = roleExists(db, role)
		if err != nil || !exists {
			return false, err
		}
	}
	pgSchema := d.Get("schema").(string)
	if !sliceContainsStr([]string{"database", "foreign_data_wrapper", "foreign_server"}, d.Get("object_type").(string)) && pgSchema != "" {
		exists, err = schemaExists(db, pgSchema)
		if err != nil || !exists {
			return false, err
		}
	}
	return true, nil
}

func generateGrantID(d *schema.ResourceData) string {
	parts := []string{d.Get("role").(string), d.Get("database").(string)}
	objectType := d.Get("object_type").(string)
	if !sliceContainsStr([]string{"database", "foreign_data_wrapper", "foreign_server"}, objectType) {
		parts = append(parts, d.Get("schema").(string))
	}
	parts = append(parts, objectType)
	for _, obj := range d.Get("objects").(*schema.Set).List() {
		parts = append(parts, obj.(string))
	}
	for _, col := range d.Get("columns").(*schema.Set).List() {
		parts = append(parts, col.(string))
	}
	return strings.Join(parts, "_")
}

func getRolesToGrant(db QueryAble, d *schema.ResourceData) ([]string, error) {
	objectType := d.Get("object_type").(string)
	if sliceContainsStr([]string{"database", "foreign_data_wrapper", "foreign_server"}, objectType) {
		return []string{}, nil
	}
	schemaName := d.Get("schema").(string)
	owners := []string{}
	if objectType != "schema" {
		tblOwners, err := getTablesOwner(db, schemaName)
		if err != nil {
			return nil, err
		}
		owners = append(owners, tblOwners...)
	}
	schOwner, err := getSchemaOwner(db, schemaName)
	if err != nil {
		return nil, err
	}
	if !sliceContainsStr(owners, schOwner) {
		owners = append(owners, schOwner)
	}
	return resolveOwners(db, owners)
}

func validateFeatureSupport(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featurePrivileges) {
		return fmt.Errorf("cockroachsql_grant resource is not supported for version %s", db.version)
	}
	if d.Get("object_type") == "procedure" && !db.featureSupported(featureProcedure) {
		return fmt.Errorf("object type PROCEDURE is not supported for version %s", db.version)
	}
	if d.Get("object_type") == "routine" && !db.featureSupported(featureRoutine) {
		return fmt.Errorf("object type ROUTINE is not supported for version %s", db.version)
	}
	return nil
}
