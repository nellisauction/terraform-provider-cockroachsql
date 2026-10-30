package cockroachsql

import (
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
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

// parseCRDBDurationMs parses a session timeout as reported in role settings
// (e.g. "1m30s", "1.5s", "250ms"). Bare integers are milliseconds.
func parseCRDBDurationMs(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}

	if ms, err := strconv.Atoi(s); err == nil {
		return ms, nil
	}

	duration, err := time.ParseDuration(strings.ToLower(s))
	if err != nil {
		return 0, fmt.Errorf("invalid duration format: %s", s)
	}
	return int(duration.Milliseconds()), nil
}

func resourceCockroachSQLRole() *schema.Resource {
	return &schema.Resource{
		CreateContext: ResourceFunc(resourceCockroachSQLRoleCreate),
		ReadContext:   ResourceFunc(resourceCockroachSQLRoleRead),
		UpdateContext: ResourceFunc(resourceCockroachSQLRoleUpdate),
		DeleteContext: ResourceFunc(resourceCockroachSQLRoleDelete),
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
				Description: "Role(s) to grant to this role. Only the memberships listed here are managed: memberships granted outside this attribute (for example with cockroachsql_grant_role) are neither reported as drift nor revoked.",
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

func resourceCockroachSQLRoleCreate(ctx context.Context, db *DBConnection, d *schema.ResourceData) error {
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

	if _, err := db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("error creating role %s: %s", roleName, redactRolePasswords(d, err.Error()))
	}

	if err := grantRoles(ctx, db, d); err != nil {
		return err
	}

	if err := alterSearchPath(ctx, db, d); err != nil {
		return err
	}

	// Small delay to ensure CRDB propagates settings to pg_roles
	time.Sleep(100 * time.Millisecond)

	if err := setStatementTimeout(ctx, db, d); err != nil {
		return err
	}

	time.Sleep(100 * time.Millisecond)

	if err := setIdleInTransactionSessionTimeout(ctx, db, d); err != nil {
		return err
	}

	d.SetId(roleName)

	return resourceCockroachSQLRoleReadImpl(ctx, db, d)
}

func resourceCockroachSQLRoleDelete(ctx context.Context, db *DBConnection, d *schema.ResourceData) error {
	roleName := d.Get(roleNameAttr).(string)

	if !d.Get(roleSkipReassignOwnedAttr).(bool) {
		if err := withRolesGranted(ctx, db, []string{roleName}, func() error {
			return reassignAndDropOwned(ctx, db, roleName, db.client.config.getDatabaseUsername())
		}); err != nil {
			return err
		}
	}
	if !d.Get(roleSkipDropRoleAttr).(bool) {
		if _, err := db.ExecContext(ctx, fmt.Sprintf("DROP ROLE %s", pq.QuoteIdentifier(roleName))); err != nil {
			return fmt.Errorf("could not delete role %s: %w", roleName, err)
		}
	}

	d.SetId("")

	return nil
}

// reassignAndDropOwned runs REASSIGN OWNED and DROP OWNED in every database,
// because both statements only affect objects in the current database.
func reassignAndDropOwned(ctx context.Context, db *DBConnection, roleName, newOwner string) error {
	databases, err := listDatabases(ctx, db)
	if err != nil {
		return err
	}

	for _, database := range databases {
		conn := db
		if database != db.client.databaseName {
			conn, err = db.client.config.NewClient(database).Connect(ctx)
			if err != nil {
				if exists, existsErr := dbExists(ctx, db, database); existsErr == nil && !exists {
					continue
				}
				return err
			}
		}

		if _, err := conn.ExecContext(ctx, fmt.Sprintf("REASSIGN OWNED BY %s TO %s", pq.QuoteIdentifier(roleName), pq.QuoteIdentifier(newOwner))); err != nil {
			if isUndefinedDatabaseError(err) {
				continue
			}
			return fmt.Errorf("could not reassign owned by role %s to %s in database %s: %w", roleName, newOwner, database, err)
		}

		if _, err := conn.ExecContext(ctx, fmt.Sprintf("DROP OWNED BY %s", pq.QuoteIdentifier(roleName))); err != nil {
			if isUndefinedDatabaseError(err) {
				continue
			}
			return fmt.Errorf("could not drop owned by role %s in database %s: %w", roleName, database, err)
		}
	}

	return nil
}

func listDatabases(ctx context.Context, db QueryAble) ([]string, error) {
	rows, err := db.QueryContext(ctx, "SELECT datname FROM pg_catalog.pg_database WHERE datname <> 'system' ORDER BY datname")
	if err != nil {
		return nil, fmt.Errorf("could not list databases: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var databases []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("could not scan database name: %w", err)
		}
		databases = append(databases, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("could not list databases: %w", err)
	}
	return databases, nil
}

func isUndefinedDatabaseError(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "3D000"
}

func resourceCockroachSQLRoleExists(ctx context.Context, db *DBConnection, d *schema.ResourceData) (bool, error) {
	var roleName string
	err := db.QueryRowContext(ctx, "SELECT rolname FROM pg_catalog.pg_roles WHERE rolname=$1", d.Id()).Scan(&roleName)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, err
	}

	return true, nil
}

func resourceCockroachSQLRoleRead(ctx context.Context, db *DBConnection, d *schema.ResourceData) error {
	exists, err := resourceCockroachSQLRoleExists(ctx, db, d)
	if err != nil {
		return err
	}
	if !exists {
		d.SetId("")
		return nil
	}

	return resourceCockroachSQLRoleReadImpl(ctx, db, d)
}

func resourceCockroachSQLRoleReadImpl(ctx context.Context, db *DBConnection, d *schema.ResourceData) error {
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
	err := db.QueryRowContext(ctx, roleSQL, roleID).Scan(values...)

	switch {
	case err == sql.ErrNoRows:
		log.Printf("[WARN] CockroachSQL ROLE (%s) not found", roleID)
		d.SetId("")
		return nil
	case err != nil:
		return fmt.Errorf("error reading ROLE: %w", err)
	}

	roleCreateRole, roleCanLogin, err = readRoleOptions(ctx, db, roleName)
	if err != nil {
		return err
	}

	d.Set(roleNameAttr, roleName)
	d.Set(roleCreateRoleAttr, roleCreateRole)
	d.Set(roleLoginAttr, roleCanLogin)
	d.Set(roleSkipDropRoleAttr, d.Get(roleSkipDropRoleAttr).(bool))
	d.Set(roleSkipReassignOwnedAttr, d.Get(roleSkipReassignOwnedAttr).(bool))
	d.Set(roleValidUntilAttr, roleValidUntil)
	d.Set(roleRolesAttr, pgArrayToSet(roleRoles).Intersection(d.Get(roleRolesAttr).(*schema.Set)))
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
		password, err := readRolePassword(ctx, db, d, roleCanLogin)
		if err != nil {
			return err
		}
		d.Set(rolePasswordAttr, password)
	}
	return nil
}

// readRoleOptions reads LOGIN/CREATEROLE from SHOW ROLES, because
// pg_roles.rolcanlogin and rolcreaterole can lag behind ALTER ROLE for
// several seconds in CockroachDB.
func readRoleOptions(ctx context.Context, db QueryAble, roleName string) (createRole bool, canLogin bool, err error) {
	var options string
	err = db.QueryRowContext(ctx, "SELECT COALESCE(options::STRING, '') FROM [SHOW ROLES] WHERE username = $1", roleName).Scan(&options)
	if err != nil {
		return false, false, fmt.Errorf("could not read options of role %s: %w", roleName, err)
	}
	opts := parseRoleOptions(options)
	return opts["CREATEROLE"], !opts["NOLOGIN"], nil
}

func parseRoleOptions(options string) map[string]bool {
	opts := map[string]bool{}
	for _, opt := range strings.Split(strings.Trim(options, "{}"), ",") {
		opt = strings.ToUpper(strings.Trim(strings.TrimSpace(opt), `"`))
		if name, _, _ := strings.Cut(opt, "="); name != "" {
			opts[name] = true
		}
	}
	return opts
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

const crdbMaskedPassword = "********"

func readRolePassword(ctx context.Context, db *DBConnection, d *schema.ResourceData, roleCanLogin bool) (string, error) {
	statePassword := d.Get(rolePasswordAttr).(string)
	if !roleCanLogin || !db.client.config.Superuser {
		return statePassword, nil
	}

	superuser, err := db.isSuperuser(ctx)
	if err != nil {
		return "", err
	}
	if !superuser {
		return "", fmt.Errorf("could not read role password from CockroachSQL as connected user is not a SUPERUSER")
	}

	var rolePassword string
	err = db.QueryRowContext(ctx, "SELECT COALESCE(passwd, '') FROM pg_catalog.pg_shadow AS s WHERE s.usename = $1", d.Id()).Scan(&rolePassword)
	switch {
	case err == sql.ErrNoRows:
		return "", nil
	case err != nil:
		return "", fmt.Errorf("error reading role password: %w", err)
	}

	// CockroachDB masks every stored password, so drift cannot be detected.
	if rolePassword == crdbMaskedPassword {
		return statePassword, nil
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

func resourceCockroachSQLRoleUpdate(ctx context.Context, db *DBConnection, d *schema.ResourceData) error {
	if err := setRolePassword(ctx, db, d); err != nil {
		return err
	}
	if err := setRoleCreateRole(ctx, db, d); err != nil {
		return err
	}
	if err := setRoleLogin(ctx, db, d); err != nil {
		return err
	}
	if err := setRoleValidUntil(ctx, db, d); err != nil {
		return err
	}
	if err := updateRoleMemberships(ctx, db, d); err != nil {
		return err
	}
	if err := alterSearchPath(ctx, db, d); err != nil {
		return err
	}
	if err := setStatementTimeout(ctx, db, d); err != nil {
		return err
	}
	if err := setIdleInTransactionSessionTimeout(ctx, db, d); err != nil {
		return err
	}

	return resourceCockroachSQLRoleReadImpl(ctx, db, d)
}

func setRolePassword(ctx context.Context, db QueryAble, d *schema.ResourceData) error {
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
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("error updating role password: %s", redactRolePasswords(d, err.Error()))
	}
	return nil
}

func setRoleCreateRole(ctx context.Context, db QueryAble, d *schema.ResourceData) error {
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
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("error updating role CREATEROLE: %w", err)
	}
	return nil
}

func setRoleLogin(ctx context.Context, db QueryAble, d *schema.ResourceData) error {
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
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("error updating role LOGIN: %w", err)
	}
	return nil
}

func setRoleValidUntil(ctx context.Context, db QueryAble, d *schema.ResourceData) error {
	if !d.HasChange(roleValidUntilAttr) {
		return nil
	}
	validUntil := d.Get(roleValidUntilAttr).(string)
	if validUntil == "" || strings.ToLower(validUntil) == "infinity" {
		validUntil = "infinity"
	}
	roleName := d.Get(roleNameAttr).(string)
	stmt := fmt.Sprintf("ALTER ROLE %s VALID UNTIL '%s'", pq.QuoteIdentifier(roleName), pqQuoteLiteral(validUntil))
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("error updating role VALID UNTIL: %w", err)
	}
	return nil
}

// updateRoleMemberships only touches memberships added to or removed from
// `roles`, so memberships managed elsewhere (e.g. cockroachsql_grant_role)
// are left alone.
func updateRoleMemberships(ctx context.Context, db QueryAble, d *schema.ResourceData) error {
	if !d.HasChange(roleRolesAttr) {
		return nil
	}
	role := d.Get(roleNameAttr).(string)
	oldRaw, newRaw := d.GetChange(roleRolesAttr)
	oldRoles, newRoles := oldRaw.(*schema.Set), newRaw.(*schema.Set)

	for _, r := range oldRoles.Difference(newRoles).List() {
		query := fmt.Sprintf("REVOKE %s FROM %s", pq.QuoteIdentifier(r.(string)), pq.QuoteIdentifier(role))
		if _, err := db.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("could not revoke role %s from %s: %w", r, role, err)
		}
	}
	for _, r := range newRoles.Difference(oldRoles).List() {
		query := fmt.Sprintf("GRANT %s TO %s", pq.QuoteIdentifier(r.(string)), pq.QuoteIdentifier(role))
		if _, err := db.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("could not grant role %s to %s: %w", r, role, err)
		}
	}
	return nil
}

func grantRoles(ctx context.Context, db QueryAble, d *schema.ResourceData) error {
	role := d.Get(roleNameAttr).(string)
	for _, grantingRole := range d.Get("roles").(*schema.Set).List() {
		query := fmt.Sprintf("GRANT %s TO %s", pq.QuoteIdentifier(grantingRole.(string)), pq.QuoteIdentifier(role))
		if _, err := db.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	return nil
}

func alterSearchPath(ctx context.Context, db QueryAble, d *schema.ResourceData) error {
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
	if _, err := db.ExecContext(ctx, query); err != nil {
		return err
	}
	return nil
}

func setStatementTimeout(ctx context.Context, db QueryAble, d *schema.ResourceData) error {
	roleName := d.Get(roleNameAttr).(string)
	statementTimeout := d.Get(roleStatementTimeoutAttr).(int)
	if statementTimeout != 0 {
		stmt := fmt.Sprintf("ALTER ROLE %s SET statement_timeout TO %d", pq.QuoteIdentifier(roleName), statementTimeout)
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	} else if d.HasChange(roleStatementTimeoutAttr) {
		stmt := fmt.Sprintf("ALTER ROLE %s RESET statement_timeout", pq.QuoteIdentifier(roleName))
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func setIdleInTransactionSessionTimeout(ctx context.Context, db QueryAble, d *schema.ResourceData) error {
	roleName := d.Get(roleNameAttr).(string)
	idleTimeout := d.Get(roleIdleInTransactionSessionTimeoutAttr).(int)
	if idleTimeout != 0 {
		stmt := fmt.Sprintf("ALTER ROLE %s SET idle_in_transaction_session_timeout TO %d", pq.QuoteIdentifier(roleName), idleTimeout)
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	} else if d.HasChange(roleIdleInTransactionSessionTimeoutAttr) {
		stmt := fmt.Sprintf("ALTER ROLE %s RESET idle_in_transaction_session_timeout", pq.QuoteIdentifier(roleName))
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func redactRolePasswords(d *schema.ResourceData, s string) string {
	wo, _ := getWO(d, rolePasswordWOAttr)
	password := d.Get(rolePasswordAttr).(string)
	return redactStrings(s, password, pqQuoteLiteral(password), wo, pqQuoteLiteral(wo))
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
