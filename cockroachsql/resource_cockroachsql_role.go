package cockroachsql

import (
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/lib/pq"
)

const (
	roleCreateRoleAttr                      = "create_role"
	roleIdleInTransactionSessionTimeoutAttr = "idle_in_transaction_session_timeout"
	roleLoginAttr                           = "login"
	roleNameAttr                            = "name"
	rolePasswordAttr                        = "password"
	rolePasswordWOAttr                      = "password_wo"
	rolePasswordWOVersionAttr               = "password_wo_version"
	roleSkipDropRoleAttr                    = "skip_drop_role"
	roleSkipReassignOwnedAttr               = "skip_reassign_owned"
	roleValidUntilAttr                      = "valid_until"
	roleRolesAttr                           = "roles"
	roleSearchPathAttr                      = "search_path"
	roleStatementTimeoutAttr                = "statement_timeout"

	// Deprecated options
	roleDepEncryptedAttr = "encrypted"
)

var durationRegex = regexp.MustCompile(`^(\d+)([a-z]*)$`)

func parseCRDBDurationMs(s string) (int, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "0" || s == "" {
		return 0, nil
	}

	matches := durationRegex.FindStringSubmatch(s)
	if len(matches) != 3 {
		return 0, fmt.Errorf("invalid duration format: %s", s)
	}

	val, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, err
	}

	unit := matches[2]
	switch unit {
	case "ms":
		return val, nil
	case "s", "":
		return val * 1000, nil
	case "m":
		return val * 60 * 1000, nil
	case "h":
		return val * 60 * 60 * 1000, nil
	default:
		return val, nil // Fallback to raw value
	}
}

func resourceCockroachSQLRole() *schema.Resource {
	return &schema.Resource{
		Create: ResourceFunc(resourceCockroachSQLRoleCreate),
		Read:   ResourceFunc(resourceCockroachSQLRoleRead),
		Update: ResourceFunc(resourceCockroachSQLRoleUpdate),
		Delete: ResourceFunc(resourceCockroachSQLRoleDelete),
		Exists: ResourceExistsFunc(resourceCockroachSQLRoleExists),
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			roleNameAttr: {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The name of the role",
			},
			rolePasswordAttr: {
				Type:          schema.TypeString,
				Optional:      true,
				Sensitive:     true,
				ConflictsWith: []string{rolePasswordWOAttr, rolePasswordWOVersionAttr},
				Description:   "Sets the role's password",
			},
			rolePasswordWOAttr: {
				Type:          schema.TypeString,
				Optional:      true,
				Sensitive:     true,
				ConflictsWith: []string{rolePasswordAttr},
				RequiredWith:  []string{rolePasswordWOVersionAttr},
				WriteOnly:     true,
				Description:   "Sets the role's password without storing it in the state file.",
			},
			rolePasswordWOVersionAttr: {
				Type:          schema.TypeString,
				Optional:      true,
				ConflictsWith: []string{rolePasswordAttr},
				RequiredWith:  []string{rolePasswordWOAttr},
				Description:   "Prevents applies from updating the role password on every apply unless the value changes.",
			},
			roleDepEncryptedAttr: {
				Type:       schema.TypeString,
				Optional:   true,
				Deprecated: fmt.Sprintf("Rename CockroachSQL role resource attribute %q", roleDepEncryptedAttr),
			},
			roleRolesAttr: {
				Type:        schema.TypeSet,
				Optional:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Set:         schema.HashString,
				MinItems:    0,
				Description: "Role(s) to grant to this new role",
			},
			roleSearchPathAttr: {
				Type:        schema.TypeList,
				Optional:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				MinItems:    0,
				Description: "Sets the role's search path",
			},
			roleValidUntilAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Sets a date and time after which the role's password is no longer valid",
				DiffSuppressFunc: func(k, old, new string, d *schema.ResourceData) bool {
					if (old == "infinity" || old == "") && (new == "infinity" || new == "") {
						return true
					}
					return old == new
				},
			},
			roleCreateRoleAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "Determine whether this role will be permitted to create new roles",
			},
			roleIdleInTransactionSessionTimeoutAttr: {
				Type:         schema.TypeInt,
				Optional:     true,
				Description:  "Terminate any session with an open transaction that has been idle for longer than the specified duration in milliseconds",
				ValidateFunc: validation.IntAtLeast(0),
			},
			roleLoginAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "Determine whether a role is allowed to log in",
			},
			roleSkipDropRoleAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "Skip actually running the DROP ROLE command when removing a ROLE from CockroachSQL",
			},
			roleSkipReassignOwnedAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "Skip actually running the REASSIGN OWNED command when removing a role from CockroachSQL",
			},
			roleStatementTimeoutAttr: {
				Type:         schema.TypeInt,
				Optional:     true,
				Description:  "Abort any statement that takes more than the specified number of milliseconds",
				ValidateFunc: validation.IntAtLeast(0),
			},
		},
	}
}

func resourceCockroachSQLRoleCreate(db *DBConnection, d *schema.ResourceData) error {
	var stringOpts []struct {
		hclKey string
		sqlKey string
	}

	if v, ok := d.GetOk(roleValidUntilAttr); ok && v.(string) != "" {
		stringOpts = append(stringOpts, struct {
			hclKey string
			sqlKey string
		}{roleValidUntilAttr, "VALID UNTIL"})
	}

	if v, ok := d.GetOk(rolePasswordAttr); ok && v.(string) != "" {
		stringOpts = append(
			[]struct{ hclKey, sqlKey string }{{rolePasswordAttr, "PASSWORD"}},
			stringOpts...,
		)
	} else if _, ok := getWO(d, rolePasswordWOAttr); ok {
		stringOpts = append(
			[]struct{ hclKey, sqlKey string }{{rolePasswordWOAttr, "PASSWORD"}},
			stringOpts...,
		)
	}

	type boolOptType struct {
		hclKey        string
		sqlKeyEnable  string
		sqlKeyDisable string
	}
	boolOpts := []boolOptType{
		{roleLoginAttr, "LOGIN", "NOLOGIN"},
		{roleCreateRoleAttr, "CREATEROLE", "NOCREATEROLE"},
	}

	createOpts := make([]string, 0, len(stringOpts)+len(boolOpts))

	for _, opt := range stringOpts {
		var val string
		var ok bool

		if opt.hclKey == rolePasswordWOAttr {
			v, found := getWO(d, opt.hclKey)
			if found {
				val = v
				if val != "" {
					ok = true
				}
			}
		} else {
			v, found := d.GetOk(opt.hclKey)
			if found {
				val = v.(string)
				if val != "" {
					ok = true
				}
			}
		}

		if !ok {
			continue
		}

		switch opt.hclKey {
		case rolePasswordWOAttr, rolePasswordAttr:
			if strings.ToUpper(val) == "NULL" {
				createOpts = append(createOpts, "PASSWORD NULL")
			} else {
				createOpts = append(createOpts,
					fmt.Sprintf("%s '%s'", opt.sqlKey, pqQuoteLiteral(val)))
			}

		case roleValidUntilAttr:
			if val == "" || strings.ToLower(val) == "infinity" {
				createOpts = append(createOpts,
					fmt.Sprintf("%s 'infinity'", opt.sqlKey))
			} else {
				createOpts = append(createOpts,
					fmt.Sprintf("%s '%s'", opt.sqlKey, pqQuoteLiteral(val)))
			}

		default:
			createOpts = append(createOpts,
				fmt.Sprintf("%s %s", opt.sqlKey, pq.QuoteIdentifier(val)))
		}
	}

	for _, opt := range boolOpts {
		val := d.Get(opt.hclKey).(bool)
		valStr := opt.sqlKeyDisable
		if val {
			valStr = opt.sqlKeyEnable
		}
		createOpts = append(createOpts, valStr)
	}

	roleName := d.Get(roleNameAttr).(string)
	createStr := strings.Join(createOpts, " ")
	if len(createOpts) > 0 {
		if db.featureSupported(featureCreateRoleWith) {
			createStr = " WITH " + createStr
		} else {
			createStr = " " + createStr
		}
	}

	stmt := fmt.Sprintf("CREATE ROLE %s%s", pq.QuoteIdentifier(roleName), createStr)

	if _, err := db.Exec(stmt); err != nil {
		return fmt.Errorf("error creating role %s (SQL: %s): %w", roleName, stmt, err)
	}

	if err := grantRoles(db, d); err != nil {
		return err
	}

	if err := alterSearchPath(db, d); err != nil {
		return err
	}

	// Small delay to ensure CRDB propagates settings to pg_roles
	time.Sleep(100 * time.Millisecond)

	if err := setStatementTimeout(db, d); err != nil {
		return err
	}

	time.Sleep(100 * time.Millisecond)

	if err := setIdleInTransactionSessionTimeout(db, d); err != nil {
		return err
	}

	d.SetId(roleName)

	return resourceCockroachSQLRoleReadImpl(db, d)
}

func resourceCockroachSQLRoleDelete(db *DBConnection, d *schema.ResourceData) error {
	roleName := d.Get(roleNameAttr).(string)

	if !d.Get(roleSkipReassignOwnedAttr).(bool) {
		if err := withRolesGranted(db, []string{roleName}, func() error {
			currentUser := db.client.config.getDatabaseUsername()
			if _, err := db.Exec(fmt.Sprintf("REASSIGN OWNED BY %s TO %s", pq.QuoteIdentifier(roleName), pq.QuoteIdentifier(currentUser))); err != nil {
				return fmt.Errorf("could not reassign owned by role %s to %s: %w", roleName, currentUser, err)
			}

			if _, err := db.Exec(fmt.Sprintf("DROP OWNED BY %s", pq.QuoteIdentifier(roleName))); err != nil {
				return fmt.Errorf("could not drop owned by role %s: %w", roleName, err)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	if !d.Get(roleSkipDropRoleAttr).(bool) {
		if _, err := db.Exec(fmt.Sprintf("DROP ROLE %s", pq.QuoteIdentifier(roleName))); err != nil {
			return fmt.Errorf("could not delete role %s: %w", roleName, err)
		}
	}

	d.SetId("")

	return nil
}

func resourceCockroachSQLRoleExists(db *DBConnection, d *schema.ResourceData) (bool, error) {
	var roleName string
	err := db.QueryRow("SELECT rolname FROM pg_catalog.pg_roles WHERE rolname=$1", d.Id()).Scan(&roleName)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, err
	}

	return true, nil
}

func resourceCockroachSQLRoleRead(db *DBConnection, d *schema.ResourceData) error {
	return resourceCockroachSQLRoleReadImpl(db, d)
}

func resourceCockroachSQLRoleReadImpl(db *DBConnection, d *schema.ResourceData) error {
	var roleCreateRole, roleCanLogin bool
	var roleName, roleValidUntil string
	var roleRoles, roleConfig []string

	roleID := d.Id()

	columns := []string{
		"rolname",
		"rolcreaterole",
		"rolcanlogin",
		`CASE WHEN rolvaliduntil IS NULL THEN '' WHEN rolvaliduntil > '9999-12-31'::TIMESTAMPTZ THEN 'infinity' ELSE rolvaliduntil::TEXT END`,
		"COALESCE(s.setconfig, rolconfig)",
	}

	values := []any{
		pq.Array(&roleRoles),
		&roleName,
		&roleCreateRole,
		&roleCanLogin,
		&roleValidUntil,
		pq.Array(&roleConfig),
	}

	roleSQL := fmt.Sprintf(`SELECT ARRAY(
			SELECT pg_get_userbyid(roleid) FROM pg_catalog.pg_auth_members members WHERE member = pg_roles.oid
		), %s
		FROM pg_catalog.pg_roles
		LEFT JOIN pg_catalog.pg_db_role_setting s ON pg_roles.oid = s.setrole AND s.setdatabase = 0
		WHERE rolname=$1`,
		strings.Join(columns, ", "),
	)
	err := db.QueryRow(roleSQL, roleID).Scan(values...)

	switch {
	case err == sql.ErrNoRows:
		log.Printf("[WARN] CockroachSQL ROLE (%s) not found", roleID)
		d.SetId("")
		return nil
	case err != nil:
		return fmt.Errorf("error reading ROLE: %w", err)
	}

	d.Set(roleNameAttr, roleName)
	d.Set(roleCreateRoleAttr, roleCreateRole)
	d.Set(roleLoginAttr, roleCanLogin)
	d.Set(roleSkipDropRoleAttr, d.Get(roleSkipDropRoleAttr).(bool))
	d.Set(roleSkipReassignOwnedAttr, d.Get(roleSkipReassignOwnedAttr).(bool))
	d.Set(roleValidUntilAttr, roleValidUntil)
	d.Set(roleRolesAttr, pgArrayToSet(roleRoles))
	d.Set(roleSearchPathAttr, readSearchPath(roleConfig))

	statementTimeout, err := readStatementTimeout(roleConfig)
	if err != nil {
		return err
	}
	d.Set(roleStatementTimeoutAttr, statementTimeout)

	idleInTransactionSessionTimeout, err := readIdleInTransactionSessionTimeout(roleConfig)
	if err != nil {
		return err
	}
	d.Set(roleIdleInTransactionSessionTimeoutAttr, idleInTransactionSessionTimeout)

	d.SetId(roleName)

	if _, ok := d.GetOk(rolePasswordAttr); ok {
		password, err := readRolePassword(db, d, roleCanLogin)
		if err != nil {
			return err
		}
		d.Set(rolePasswordAttr, password)
	}
	return nil
}

func readSearchPath(roleConfig []string) []string {
	for _, config := range roleConfig {
		if strings.HasPrefix(config, roleSearchPathAttr+"=") {
			val := strings.TrimPrefix(config, roleSearchPathAttr+"=")
			parts := strings.Split(val, ",")
			for i := range parts {
				parts[i] = strings.Trim(strings.TrimSpace(parts[i]), `"`)
			}
			return parts
		}
	}
	return nil
}

func readIdleInTransactionSessionTimeout(roleConfig []string) (int, error) {
	for _, config := range roleConfig {
		if strings.HasPrefix(config, roleIdleInTransactionSessionTimeoutAttr+"=") {
			valStr := strings.TrimPrefix(config, roleIdleInTransactionSessionTimeoutAttr+"=")
			res, err := parseCRDBDurationMs(valStr)
			if err != nil {
				return -1, fmt.Errorf("error reading idle_in_transaction_session_timeout: %w", err)
			}
			return res, nil
		}
	}
	return 0, nil
}

func readStatementTimeout(roleConfig []string) (int, error) {
	for _, config := range roleConfig {
		if strings.HasPrefix(config, roleStatementTimeoutAttr+"=") {
			valStr := strings.TrimPrefix(config, roleStatementTimeoutAttr+"=")
			res, err := parseCRDBDurationMs(valStr)
			if err != nil {
				return -1, fmt.Errorf("error reading statement_timeout: %w", err)
			}
			return res, nil
		}
	}
	return 0, nil
}

func readRolePassword(db *DBConnection, d *schema.ResourceData, roleCanLogin bool) (string, error) {
	statePassword := d.Get(rolePasswordAttr).(string)
	if !roleCanLogin || !db.client.config.Superuser {
		return statePassword, nil
	}

	superuser, err := db.isSuperuser()
	if err != nil {
		return "", err
	}
	if !superuser {
		return "", fmt.Errorf("could not read role password from CockroachSQL as connected user is not a SUPERUSER")
	}

	var rolePassword string
	err = db.QueryRow("SELECT COALESCE(passwd, '') FROM pg_catalog.pg_shadow AS s WHERE s.usename = $1", d.Id()).Scan(&rolePassword)
	switch {
	case err == sql.ErrNoRows:
		return "", nil
	case err != nil:
		return "", fmt.Errorf("error reading role password: %w", err)
	}

	if statePassword != "" && !strings.HasPrefix(statePassword, "md5") && !strings.HasPrefix(statePassword, "SCRAM-SHA-256") {
		if strings.HasPrefix(rolePassword, "md5") {
			hasher := md5.New()
			if _, err := hasher.Write([]byte(statePassword + d.Id())); err != nil {
				return "", err
			}
			hashedPassword := "md5" + hex.EncodeToString(hasher.Sum(nil))
			if hashedPassword == rolePassword {
				return statePassword, nil
			}
		}
		if strings.HasPrefix(rolePassword, "SCRAM-SHA-256") {
			return statePassword, nil
		}
	}
	return rolePassword, nil
}

func resourceCockroachSQLRoleUpdate(db *DBConnection, d *schema.ResourceData) error {
	if err := setRolePassword(db, d); err != nil {
		return err
	}
	if err := setRoleCreateRole(db, d); err != nil {
		return err
	}
	if err := setRoleLogin(db, d); err != nil {
		return err
	}
	if err := setRoleValidUntil(db, d); err != nil {
		return err
	}
	if err := revokeRoles(db, d); err != nil {
		return err
	}
	if err := grantRoles(db, d); err != nil {
		return err
	}
	if err := alterSearchPath(db, d); err != nil {
		return err
	}
	if err := setStatementTimeout(db, d); err != nil {
		return err
	}
	if err := setIdleInTransactionSessionTimeout(db, d); err != nil {
		return err
	}

	return resourceCockroachSQLRoleReadImpl(db, d)
}

func setRolePassword(db QueryAble, d *schema.ResourceData) error {
	if _, ok := getWO(d, rolePasswordWOAttr); ok {
		if !d.HasChange(rolePasswordWOVersionAttr) {
			return nil
		}
	} else {
		if !d.HasChange(rolePasswordAttr) {
			return nil
		}
	}

	roleName := d.Get(roleNameAttr).(string)
	var password string
	if v, ok := getWO(d, rolePasswordWOAttr); ok {
		password = v
		d.Set(rolePasswordAttr, "")
	} else if v, ok := d.GetOk(rolePasswordAttr); ok && v.(string) != "" {
		password = v.(string)
	} else {
		return nil
	}

	stmt := fmt.Sprintf("ALTER ROLE %s PASSWORD '%s'", pq.QuoteIdentifier(roleName), pqQuoteLiteral(password))
	if _, err := db.Exec(stmt); err != nil {
		return fmt.Errorf("error updating role password: %w", err)
	}
	return nil
}

func setRoleCreateRole(db QueryAble, d *schema.ResourceData) error {
	if !d.HasChange(roleCreateRoleAttr) {
		return nil
	}
	createRole := d.Get(roleCreateRoleAttr).(bool)
	tok := "NOCREATEROLE"
	if createRole {
		tok = "CREATEROLE"
	}
	roleName := d.Get(roleNameAttr).(string)
	stmt := fmt.Sprintf("ALTER ROLE %s WITH %s", pq.QuoteIdentifier(roleName), tok)
	if _, err := db.Exec(stmt); err != nil {
		return fmt.Errorf("error updating role CREATEROLE: %w", err)
	}
	return nil
}

func setRoleLogin(db QueryAble, d *schema.ResourceData) error {
	if !d.HasChange(roleLoginAttr) {
		return nil
	}
	login := d.Get(roleLoginAttr).(bool)
	tok := "NOLOGIN"
	if login {
		tok = "LOGIN"
	}
	roleName := d.Get(roleNameAttr).(string)
	stmt := fmt.Sprintf("ALTER ROLE %s WITH %s", pq.QuoteIdentifier(roleName), tok)
	if _, err := db.Exec(stmt); err != nil {
		return fmt.Errorf("error updating role LOGIN: %w", err)
	}
	return nil
}

func setRoleValidUntil(db QueryAble, d *schema.ResourceData) error {
	if !d.HasChange(roleValidUntilAttr) {
		return nil
	}
	validUntil := d.Get(roleValidUntilAttr).(string)
	if validUntil == "" || strings.ToLower(validUntil) == "infinity" {
		validUntil = "infinity"
	}
	roleName := d.Get(roleNameAttr).(string)
	stmt := fmt.Sprintf("ALTER ROLE %s VALID UNTIL '%s'", pq.QuoteIdentifier(roleName), pqQuoteLiteral(validUntil))
	if _, err := db.Exec(stmt); err != nil {
		return fmt.Errorf("error updating role VALID UNTIL: %w", err)
	}
	return nil
}

func revokeRoles(db QueryAble, d *schema.ResourceData) error {
	role := d.Get(roleNameAttr).(string)
	query := `SELECT pg_get_userbyid(roleid) FROM pg_catalog.pg_auth_members members JOIN pg_catalog.pg_roles ON members.member = pg_roles.oid WHERE rolname = $1`
	rows, err := db.Query(query, role)
	if err != nil {
		return fmt.Errorf("could not get roles list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var grantedRoles []string
	for rows.Next() {
		var grantedRole string
		if err = rows.Scan(&grantedRole); err != nil {
			return err
		}
		grantedRoles = append(grantedRoles, grantedRole)
	}

	for _, grantedRole := range grantedRoles {
		query = fmt.Sprintf("REVOKE %s FROM %s", pq.QuoteIdentifier(grantedRole), pq.QuoteIdentifier(role))
		if _, err := db.Exec(query); err != nil {
			return err
		}
	}
	return nil
}

func grantRoles(db QueryAble, d *schema.ResourceData) error {
	role := d.Get(roleNameAttr).(string)
	for _, grantingRole := range d.Get("roles").(*schema.Set).List() {
		query := fmt.Sprintf("GRANT %s TO %s", pq.QuoteIdentifier(grantingRole.(string)), pq.QuoteIdentifier(role))
		if _, err := db.Exec(query); err != nil {
			return err
		}
	}
	return nil
}

func alterSearchPath(db QueryAble, d *schema.ResourceData) error {
	role := d.Get(roleNameAttr).(string)
	searchPathInterface := d.Get(roleSearchPathAttr).([]any)
	var searchPath string
	if len(searchPathInterface) > 0 {
		var parts []string
		for _, part := range searchPathInterface {
			parts = append(parts, pq.QuoteIdentifier(part.(string)))
		}
		searchPath = strings.Join(parts, ", ")
	} else {
		searchPath = "DEFAULT"
	}
	query := fmt.Sprintf("ALTER ROLE %s SET search_path TO %s", pq.QuoteIdentifier(role), searchPath)
	if _, err := db.Exec(query); err != nil {
		return err
	}
	return nil
}

func setStatementTimeout(db QueryAble, d *schema.ResourceData) error {
	roleName := d.Get(roleNameAttr).(string)
	statementTimeout := d.Get(roleStatementTimeoutAttr).(int)
	if statementTimeout != 0 {
		stmt := fmt.Sprintf("ALTER ROLE %s SET statement_timeout TO %d", pq.QuoteIdentifier(roleName), statementTimeout)
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	} else if d.HasChange(roleStatementTimeoutAttr) {
		stmt := fmt.Sprintf("ALTER ROLE %s RESET statement_timeout", pq.QuoteIdentifier(roleName))
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func setIdleInTransactionSessionTimeout(db QueryAble, d *schema.ResourceData) error {
	roleName := d.Get(roleNameAttr).(string)
	idleTimeout := d.Get(roleIdleInTransactionSessionTimeoutAttr).(int)
	if idleTimeout != 0 {
		stmt := fmt.Sprintf("ALTER ROLE %s SET idle_in_transaction_session_timeout TO %d", pq.QuoteIdentifier(roleName), idleTimeout)
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	} else if d.HasChange(roleIdleInTransactionSessionTimeoutAttr) {
		stmt := fmt.Sprintf("ALTER ROLE %s RESET idle_in_transaction_session_timeout", pq.QuoteIdentifier(roleName))
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func getWO(d *schema.ResourceData, attribute string) (string, bool) {
	raw, diags := d.GetRawConfigAt(cty.GetAttrPath(attribute))
	if diags.HasError() || raw.IsNull() || !raw.Type().Equals(cty.String) {
		return "", false
	}
	if raw.AsString() == "" {
		return "", false
	}
	return raw.AsString(), true
}
