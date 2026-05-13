package cockroachsql

import (
	"database/sql"
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/lib/pq"
)

func ResourceFunc(fn func(*DBConnection, *schema.ResourceData) error) func(*schema.ResourceData, any) error {
	return func(d *schema.ResourceData, meta any) error {
		client := meta.(*Client)

		db, err := client.Connect()
		if err != nil {
			return err
		}

		return fn(db, d)
	}
}

func ResourceExistsFunc(fn func(*DBConnection, *schema.ResourceData) (bool, error)) func(*schema.ResourceData, any) (bool, error) {
	return func(d *schema.ResourceData, meta any) (bool, error) {
		client := meta.(*Client)

		db, err := client.Connect()
		if err != nil {
			return false, err
		}

		return fn(db, d)
	}
}

// QueryAble is a DB connection (sql.DB/Tx)
type QueryAble interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// pqQuoteLiteral returns a string literal safe for inclusion in a CockroachSQL
// query as a parameter.  The resulting string still needs to be wrapped in
// single quotes in SQL (i.e. fmt.Sprintf(`'%s'`, pqQuoteLiteral("str"))).  See
// quote_literal_internal() in cockroachsql/backend/utils/adt/quote.c:77.
func pqQuoteLiteral(in string) string {
	in = strings.ReplaceAll(in, `\`, `\\`)
	in = strings.ReplaceAll(in, `'`, `''`)
	return in
}

func isMemberOfRole(db QueryAble, role, member string) (bool, error) {
	var _rez int
	setOption := true

	err := db.QueryRow(
		"SELECT 1 FROM information_schema.columns WHERE table_name='pg_auth_members' AND column_name = 'set_option'",
	).Scan(&_rez)

	switch {
	case err == sql.ErrNoRows:
		setOption = false
	case err != nil:
		return false, fmt.Errorf("could not read setOption column: %w", err)
	}

	query := "SELECT 1 FROM pg_auth_members WHERE pg_get_userbyid(roleid) = $1 AND pg_get_userbyid(member) = $2"
	if setOption {
		query += " AND set_option"
	}

	err = db.QueryRow(query, role, member).Scan(&_rez)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("could not read role membership: %w", err)
	}

	return true, nil
}

// grantRoleMembership grants the role *role* to the user *member*.
// It returns false if the grant is not needed because the user is already
// a member of this role.
func grantRoleMembership(db QueryAble, role, member string) (bool, error) {
	if member == role {
		return false, nil
	}

	isMember, err := isMemberOfRole(db, role, member)
	if err != nil {
		return false, err
	}

	if isMember {
		log.Printf("grantRoleMembership: %s is already a member of %s, nothing to do", member, role)
		return false, nil
	}

	log.Printf("grantRoleMembership: granting %s to %s", role, member)

	stmt := fmt.Sprintf("GRANT %s TO %s", pq.QuoteIdentifier(role), pq.QuoteIdentifier(member))
	if _, err := db.Exec(stmt); err != nil {
		return false, fmt.Errorf("error granting role %s to %s: %w", role, member, err)
	}
	return true, nil
}

// revokeRoleMembership revokes the role *role* from the user *member*.
// It returns false if the revoke is not needed because the user is not a member of this role.
func revokeRoleMembership(db QueryAble, role, member string) (bool, error) {
	if member == role {
		return false, nil
	}

	isMember, err := isMemberOfRole(db, role, member)
	if err != nil {
		return false, err
	}
	if !isMember {
		return false, nil
	}

	log.Printf("revokeRoleMembership: Revoke %s from %s", role, member)

	stmt := fmt.Sprintf("REVOKE %s FROM %s", pq.QuoteIdentifier(role), pq.QuoteIdentifier(member))
	if _, err := db.Exec(stmt); err != nil {
		return false, fmt.Errorf("error revoking role %s from %s: %w", role, member, err)
	}
	return true, nil
}

// withRolesGranted temporarily grants, if needed, the roles specified to connected user
// (i.e.: the admin configure in the provider) and revoke them as soon as the
// callback func has finished.
func withRolesGranted(db QueryAble, roles []string, fn func() error) error {
	currentUser, err := getCurrentUser(db)
	if err != nil {
		return err
	}

	grantedRoles := []string{}
	for _, role := range roles {
		if role == "" || role == currentUser {
			continue
		}

		if _, err := db.Exec(fmt.Sprintf("GRANT %s TO %s", pq.QuoteIdentifier(role), pq.QuoteIdentifier(currentUser))); err != nil {
			return fmt.Errorf("could not grant role %s to %s: %w", role, currentUser, err)
		}
		grantedRoles = append(grantedRoles, role)
	}

	if len(grantedRoles) > 0 {
		defer func() {
			for _, role := range grantedRoles {
				if _, err := db.Exec(fmt.Sprintf("REVOKE %s FROM %s", pq.QuoteIdentifier(role), pq.QuoteIdentifier(currentUser))); err != nil {
					log.Printf("[ERR] could not revoke role %s from %s: %v", role, currentUser, err)
				}
			}
		}()
	}

	return fn()
}

func sliceContainsStr(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// allowedPrivileges is the list of privileges allowed per object types in CockroachSQL.
// see: https://www.cockroachsql.org/docs/current/sql-grant.html
var allowedPrivileges = map[string][]string{
	"database":             {"ALL", "CREATE", "CONNECT", "TEMPORARY"},
	"table":                {"ALL", "SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE", "REFERENCES", "TRIGGER"},
	"sequence":             {"ALL", "USAGE", "SELECT", "UPDATE"},
	"schema":               {"ALL", "CREATE", "USAGE"},
	"function":             {"ALL", "EXECUTE"},
	"procedure":            {"ALL", "EXECUTE"},
	"routine":              {"ALL", "EXECUTE"},
	"type":                 {"ALL", "USAGE"},
	"foreign_data_wrapper": {"ALL", "USAGE"},
	"foreign_server":       {"ALL", "USAGE"},
	"column":               {"ALL", "SELECT", "INSERT", "UPDATE", "REFERENCES"},
}

// validatePrivileges checks that privileges to apply are allowed for this object type.
func validatePrivileges(d *schema.ResourceData) error {
	objectType := d.Get("object_type").(string)
	privileges := d.Get("privileges").(*schema.Set).List()

	allowed, ok := allowedPrivileges[objectType]
	if !ok {
		return fmt.Errorf("unknown object type %s", objectType)
	}

	for _, priv := range privileges {
		if !sliceContainsStr(allowed, priv.(string)) {
			return fmt.Errorf("%s is not an allowed privilege for object type %s", priv, objectType)
		}
	}
	return nil
}

func resourcePrivilegesEqual(granted *schema.Set, d *schema.ResourceData) bool {
	objectType := d.Get("object_type").(string)
	wanted := d.Get("privileges").(*schema.Set)

	log.Printf("[DEBUG] cockroachsql: comparing granted=%v wanted=%v", granted.List(), wanted.List())
	if granted.Equal(wanted) {
		return true
	}

	if !wanted.Contains("ALL") {
		return false
	}

	// implicit check: e.g. for object_type schema -> ALL == ["CREATE", "USAGE"]
	log.Printf("The wanted privilege is 'ALL'. therefore, we will check if the current privileges are ALL implicitly")
	implicits := []any{}
	for _, p := range allowedPrivileges[objectType] {
		if p != "ALL" {
			implicits = append(implicits, p)
		}
	}
	wantedSet := schema.NewSet(schema.HashString, implicits)
	return granted.Equal(wantedSet)
}

func pgArrayToSet(arr pq.StringArray) *schema.Set {
	s := schema.NewSet(schema.HashString, []any{})
	for _, v := range arr {
		s.Add(v)
	}
	return s
}

func stringSliceToSet(slice []string) *schema.Set {
	s := make([]any, len(slice))
	for i, v := range slice {
		s[i] = v
	}
	return schema.NewSet(schema.HashString, s)
}

func quoteIdentifyIdent(ident string) string {
	// When passing a function with arguments like "test(text, char)" this will correctly parse it to "test"(text, char).
	// If we were to add quotes around the whole ident CockroachDB would not be able to find the function.
	// Usually specifying parameters of a function is not necessary, but CockroachDB allows function overloading where it
	// identifies the function by its parameters allowing the developer to have multiple functions with the same name.
	// Information:
	// https://en.wikipedia.org/wiki/Function_overloading
	// https://stackoverflow.com/a/48640797

	s := strings.Split(ident, "(")

	functionArgTypes := ""

	if len(s) > 1 {
		functionArgTypes = "(" + s[1]
	}

	return fmt.Sprintf("%s%s", pq.QuoteIdentifier(s[0]), functionArgTypes)
}

func setToPgIdentList(schema string, idents *schema.Set) string {
	quotedIdents := make([]string, idents.Len())
	for i, ident := range idents.List() {
		quotedIdents[i] = fmt.Sprintf(
			"%s.%s",
			pq.QuoteIdentifier(schema), quoteIdentifyIdent(ident.(string)),
		)
	}
	return strings.Join(quotedIdents, ",")
}

func setToPgIdentListWithoutSchema(idents *schema.Set) string {
	quotedIdents := make([]string, idents.Len())
	for i, ident := range idents.List() {
		quotedIdents[i] = pq.QuoteIdentifier(ident.(string))
	}
	return strings.Join(quotedIdents, ",")
}

func setToPgIdentSimpleList(idents *schema.Set) string {
	quotedIdents := make([]string, idents.Len())
	for i, ident := range idents.List() {
		quotedIdents[i] = ident.(string)
	}
	return strings.Join(quotedIdents, ",")
}

func dbExists(db QueryAble, dbname string) (bool, error) {
	err := db.QueryRow("SELECT datname FROM pg_database WHERE datname=$1", dbname).Scan(&dbname)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("could not check if database exists: %w", err)
	}

	return true, nil
}

func roleExists(db QueryAble, rolname string) (bool, error) {
	err := db.QueryRow("SELECT 1 FROM pg_roles WHERE rolname=$1", rolname).Scan(&rolname)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("could not check if role exists: %w", err)
	}

	return true, nil
}

func schemaExists(db QueryAble, schemaname string) (bool, error) {
	err := db.QueryRow("SELECT 1 FROM pg_namespace WHERE nspname=$1", schemaname).Scan(&schemaname)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("could not check if schema exists: %w", err)
	}

	return true, nil
}

func getCurrentUser(db QueryAble) (string, error) {
	var currentUser string
	err := db.QueryRow("SELECT CURRENT_USER").Scan(&currentUser)
	switch {
	case err == sql.ErrNoRows:
		return "", fmt.Errorf("SELECT CURRENT_USER returns now row, this is quite disturbing")
	case err != nil:
		return "", fmt.Errorf("error while looking for the current user: %w", err)
	}
	return currentUser, nil
}

func getDatabase(d *schema.ResourceData, databaseName string) string {
	if v, ok := d.GetOk("database"); ok {
		databaseName = v.(string)
	}

	return databaseName
}

func getDatabaseOwner(db QueryAble, database string) (string, error) {
	dbQueryString := "$1"
	dbQueryValues := []any{database}

	// Empty means current DB
	if database == "" {
		dbQueryString = "current_database()"
		dbQueryValues = []any{}

	}
	query := fmt.Sprintf(`
SELECT rolname
  FROM pg_database
  JOIN pg_roles ON datdba = pg_roles.oid
  WHERE datname = %s
`, dbQueryString)
	var owner string

	err := db.QueryRow(query, dbQueryValues...).Scan(&owner)
	switch {
	case err == sql.ErrNoRows:
		return "", fmt.Errorf("could not find database '%s' while looking for owner", database)
	case err != nil:
		return "", fmt.Errorf("error while looking for the owner of database '%s': %w", database, err)
	}
	return owner, nil
}

func getSchemaOwner(db QueryAble, schemaName string) (string, error) {
	query := `
SELECT rolname
  FROM pg_namespace
  JOIN pg_roles ON nspowner = pg_roles.oid
  WHERE nspname = $1
`
	var owner string

	err := db.QueryRow(query, schemaName).Scan(&owner)
	switch {
	case err == sql.ErrNoRows:
		return "", fmt.Errorf("could not find schema '%s' while looking for owner", schemaName)
	case err != nil:
		return "", fmt.Errorf("error while looking for the owner of schema '%s': %w", schemaName, err)
	}
	return owner, nil
}

// getTablesOwner retrieves all the owners for all the tables in the specified schema.
func getTablesOwner(db QueryAble, schemaName string) ([]string, error) {
	rows, err := db.Query(
		"SELECT DISTINCT tableowner FROM pg_tables WHERE schemaname = $1",
		schemaName,
	)
	if err != nil {
		return nil, fmt.Errorf("error while looking for owners of tables in schema '%s': %w", schemaName, err)
	}
	defer func() { _ = rows.Close() }()

	var owners []string
	for rows.Next() {
		var owner string
		if err := rows.Scan(&owner); err != nil {
			return nil, fmt.Errorf("could not scan tables owner: %w", err)
		}
		owners = append(owners, owner)
	}

	return owners, nil
}

func resolveOwners(db QueryAble, owners []string) ([]string, error) {
	resolvedOwners := []string{}
	for _, owner := range owners {
		if owner == "pg_database_owner" {
			var err error
			owner, err = getDatabaseOwner(db, "")
			if err != nil {
				return nil, err
			}
		}
		resolvedOwners = append(resolvedOwners, owner)
	}

	return resolvedOwners, nil
}

const publicRole = "public"

func findStringSubmatchMap(expression string, text string) map[string]string {

	r := regexp.MustCompile(expression)

	parts := r.FindStringSubmatch(text)

	paramsMap := make(map[string]string)
	for i, name := range r.SubexpNames() {
		if i > 0 && i <= len(parts) {
			paramsMap[name] = parts[i]
		}
	}

	return paramsMap
}

// quoteTable can quote a table name with or without a schema prefix
// Example:
//
//	my_table -> "my_table"
//	public.my_table -> "public"."my_table"
func quoteTableName(tableName string) string {
	parts := strings.Split(tableName, ".")
	for i := range parts {
		parts[i] = pq.QuoteIdentifier(parts[i])
	}
	return strings.Join(parts, ".")
}
