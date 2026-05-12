package cockroachsql

import (
	"database/sql"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"unicode"

	"github.com/blang/semver"
	_ "github.com/lib/pq" // CockroachDB db
)

type featureName uint

const (
	featureCreateRoleWith featureName = iota
	featureDatabaseOwnerRole
	featureDBAllowConnections
	featureDBIsTemplate
	featureFallbackApplicationName
	featureRLS
	featureSchemaCreateIfNotExist
	featureReplication
	featureExtension
	featurePrivileges
	featureProcedure
	featureRoutine
	featurePrivilegesOnSchemas
	featureForceDropDatabase
	featurePid
	featurePublishViaRoot
	featurePubTruncate
	featurePublication
	featurePubWithoutTruncate
	featureFunction
	featureServer
	featureCreateRoleSelfGrant
	featureSecurityLabel
)

var (
	dbRegistryLock sync.Mutex
	dbRegistry     map[string]*DBConnection = make(map[string]*DBConnection, 1)

	// Mapping of feature flags to versions
	featureSupported = map[featureName]semver.Range{
		// CREATE ROLE WITH
		featureCreateRoleWith: semver.MustParseRange(">=0.0.0"),

		// https://www.cockroachlabs.com/docs/9.0/static/libpq-connect.html
		featureFallbackApplicationName: semver.MustParseRange(">=0.0.0"),

		// CREATE SCHEMA IF NOT EXISTS
		featureSchemaCreateIfNotExist: semver.MustParseRange(">=0.0.0"),

		// We do not support cockroachsql_grant and cockroachsql_default_privileges
		featurePrivileges: semver.MustParseRange(">=0.0.0"),

		// ALTER DEFAULT PRIVILEGES has ON SCHEMAS support
		featurePrivilegesOnSchemas: semver.MustParseRange(">=22.1.0"),

		// DROP DATABASE WITH FORCE
		featureForceDropDatabase: semver.MustParseRange(">=23.2.0"),

		// pid in pg_stat_activity
		featurePid: semver.MustParseRange(">=0.0.0"),

		featureDatabaseOwnerRole: semver.MustParseRange(">=24.1.0"),

		// New privileges rules in version 16
		featureCreateRoleSelfGrant: semver.MustParseRange(">=24.1.0"),

		// Unsupported features in CockroachDB
		featureDBAllowConnections: semver.MustParseRange(">99.0.0"),
		featureDBIsTemplate:       semver.MustParseRange(">99.0.0"),
		featureRLS:                semver.MustParseRange(">99.0.0"),
		featureReplication:        semver.MustParseRange(">99.0.0"),
		featureExtension:          semver.MustParseRange(">99.0.0"),
		featureProcedure:          semver.MustParseRange(">99.0.0"),
		featureRoutine:            semver.MustParseRange(">99.0.0"),
		featurePublishViaRoot:     semver.MustParseRange(">99.0.0"),
		featurePubTruncate:        semver.MustParseRange(">99.0.0"),
		featurePubWithoutTruncate: semver.MustParseRange(">99.0.0"),
		featurePublication:        semver.MustParseRange(">99.0.0"),
		featureFunction:           semver.MustParseRange(">99.0.0"),
		featureServer:             semver.MustParseRange(">99.0.0"),
		featureSecurityLabel:      semver.MustParseRange(">99.0.0"),
	}
)

type DBConnection struct {
	*sql.DB

	client *Client

	// version is the version number of the database as determined by parsing the
	// output of `SELECT VERSION()`.x
	version semver.Version
}

// featureSupported returns true if a given feature is supported or not. This is
// slightly different from Config's featureSupported in that here we're
// evaluating against the fingerprinted version, not the expected version.
func (db *DBConnection) featureSupported(name featureName) bool {
	fn, found := featureSupported[name]
	if !found {
		// panic'ing because this is a provider-only bug
		panic(fmt.Sprintf("unknown feature flag %v", name))
	}

	return fn(db.version)
}

// isSuperuser returns true if connected user is a CockroachDB SUPERUSER
func (db *DBConnection) isSuperuser() (bool, error) {
	var superuser bool

	if err := db.QueryRow("SELECT rolsuper FROM pg_roles WHERE rolname = CURRENT_USER").Scan(&superuser); err != nil {
		return false, fmt.Errorf("could not check if current user is superuser: %w", err)
	}

	return superuser, nil
}

type ClientCertificateConfig struct {
	CertificatePath string
	KeyPath         string
	SSLInline       bool
}

// Config - provider config
type Config struct {
	ConnectionURL     string
	Host              string
	Port              int
	Username          string
	Password          string
	DatabaseUsername  string
	Superuser         bool
	SSLMode           string
	ApplicationName   string
	Timeout           int
	ConnectTimeoutSec int
	MaxConns          int
	ExpectedVersion   semver.Version
	SSLClientCert     *ClientCertificateConfig
	SSLRootCertPath   string
}

// Client struct holding connection string
type Client struct {
	// Configuration for the client
	config Config

	databaseName string
}

// NewClient returns client config for the specified database.
func (c *Config) NewClient(database string) *Client {
	return &Client{
		config:       *c,
		databaseName: database,
	}
}

// featureSupported returns true if a given feature is supported or not.  This
// is slightly different from Client's featureSupported in that here we're
// evaluating against the expected version, not the fingerprinted version.
func (c *Config) featureSupported(name featureName) bool {
	fn, found := featureSupported[name]
	if !found {
		// panic'ing because this is a provider-only bug
		panic(fmt.Sprintf("unknown feature flag %v", name))
	}

	return fn(c.ExpectedVersion)
}

func (c *Config) connParams() []string {
	params := map[string]string{}

	if c.SSLMode != "" {
		params["sslmode"] = c.SSLMode
	}
	params["connect_timeout"] = fmt.Sprintf("%d", c.ConnectTimeoutSec)

	if c.featureSupported(featureFallbackApplicationName) && c.ApplicationName != "" {
		params["fallback_application_name"] = c.ApplicationName
	}
	if c.SSLClientCert != nil {
		params["sslcert"] = c.SSLClientCert.CertificatePath
		params["sslkey"] = c.SSLClientCert.KeyPath
		if c.SSLClientCert.SSLInline {
			params["sslinline"] = fmt.Sprintf("%t", c.SSLClientCert.SSLInline)
		}
	}

	if c.SSLRootCertPath != "" {
		params["sslrootcert"] = c.SSLRootCertPath
	}

	paramsArray := []string{}
	for key, value := range params {
		paramsArray = append(paramsArray, fmt.Sprintf("%s=%s", key, url.QueryEscape(value)))
	}

	return paramsArray
}

func (c *Config) connStr(database string) string {
	connStr := fmt.Sprintf(
		"postgresql://%s:%s@%s:%d/%s?%s",
		url.PathEscape(c.Username),
		url.PathEscape(c.Password),
		c.Host,
		c.Port,
		database,
		strings.Join(c.connParams(), "&"),
	)

	return connStr
}

func (c *Config) getDatabaseUsername() string {
	if c.DatabaseUsername != "" {
		return c.DatabaseUsername
	}
	return c.Username
}

// Connect returns a copy to an sql.Open()'ed database connection wrapped in a DBConnection struct.
// Callers must return their database resources. Use of QueryRow() or Exec() is encouraged.
// Query() must have their rows.Close()'ed.
func (c *Client) Connect() (*DBConnection, error) {
	dbRegistryLock.Lock()
	defer dbRegistryLock.Unlock()

	dsn := c.config.ConnectionURL
	if dsn == "" {
		dsn = c.config.connStr(c.databaseName)
	}
	conn, found := dbRegistry[dsn]
	if found {
		if err := conn.Ping(); err != nil {
			log.Printf("[DEBUG] cockroachsql: cached connection for %s is dead: %v. Re-opening.", dsn, err)
			_ = conn.Close()
			delete(dbRegistry, dsn)
			found = false
		}
	}
	if !found {
		db, err := sql.Open(proxyDriverName, dsn)
		if err != nil {
			errString := strings.Replace(err.Error(), c.config.Password, "XXXX", 2)
			return nil, fmt.Errorf("error connecting to CockroachDB server %s: %s", c.config.Host, errString)
		}

		if err == nil {
			err = db.Ping()
		}
		if err != nil {
			errString := strings.Replace(err.Error(), c.config.Password, "XXXX", 2)
			return nil, fmt.Errorf("error connecting to CockroachDB server %s: %s", c.config.Host, errString)
		}

		// We don't want to retain connection
		// So when we connect on a specific database which might be managed by terraform,
		// we don't keep opened connection in case of the db has to be dropped in the plan.
		db.SetMaxIdleConns(0)
		db.SetMaxOpenConns(c.config.MaxConns)

		defaultVersion, _ := semver.Parse(defaultExpectedCockroachSQLVersion)
		version := &c.config.ExpectedVersion
		if defaultVersion.Equals(c.config.ExpectedVersion) {
			// Version hint not set by user, need to fingerprint
			v, err := fingerprintCapabilities(db)
			if err != nil {
				_ = db.Close()
				return nil, fmt.Errorf("error detecting capabilities: %w", err)
			}
			version = v
		}

		conn = &DBConnection{
			db,
			c,
			*version,
		}
		dbRegistry[dsn] = conn
	}

	return conn, nil
}

// fingerprintCapabilities queries CockroachDB to populate a local catalog of
// capabilities.  This is only run once per Client.
func fingerprintCapabilities(db *sql.DB) (*semver.Version, error) {
	var crdbVersion string
	err := db.QueryRow(`SELECT VERSION()`).Scan(&crdbVersion)
	if err != nil {
		return nil, fmt.Errorf("error CockroachDB version: %w", err)
	}

	// CockroachDB CCL v22.1.2 ...
	fields := strings.FieldsFunc(crdbVersion, func(c rune) bool {
		return unicode.IsSpace(c) || c == ','
	})
	if len(fields) < 2 {
		return nil, fmt.Errorf("error determining the server version: %q", crdbVersion)
	}

	var versionStr string
	// Find the version string (e.g. v22.1.2)
	for _, field := range fields {
		if strings.HasPrefix(field, "v") || (len(field) > 0 && unicode.IsDigit(rune(field[0]))) {
			versionStr = field
			break
		}
	}

	if versionStr == "" {
		return nil, fmt.Errorf("error determining the server version: %q", crdbVersion)
	}

	version, err := semver.ParseTolerant(versionStr)
	if err != nil {
		return nil, fmt.Errorf("error parsing version: %w", err)
	}

	return &version, nil
}
