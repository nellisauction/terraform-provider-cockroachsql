package cockroachsql

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/blang/semver"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
)

const (
	defaultProviderMaxOpenConnections  = 20
	defaultExpectedCockroachSQLVersion = "23.2.0"
)

func multiEnvDefaultFunc(keys []string, defaultValue any) schema.SchemaDefaultFunc {
	return func() (any, error) {
		for _, key := range keys {
			if v := os.Getenv(key); v != "" {
				if _, ok := defaultValue.(int); ok {
					return strconv.Atoi(v)
				}
				return v, nil
			}
		}
		return defaultValue, nil
	}
}

// Provider returns a terraform.ResourceProvider.
func Provider() *schema.Provider {
	return &schema.Provider{
		Schema: map[string]*schema.Schema{
			"host": {
				Type:     schema.TypeString,
				Optional: true,
				DefaultFunc: multiEnvDefaultFunc([]string{
					"COCKROACH_HOST",
				}, nil),
				Description: "Name of CockroachDB server address to connect to",
			},
			"port": {
				Type:     schema.TypeInt,
				Optional: true,
				DefaultFunc: multiEnvDefaultFunc([]string{
					"COCKROACH_PORT",
				}, 26257),
				Description: "The CockroachDB port number to connect to at the server host",
			},
			"database": {
				Type:     schema.TypeString,
				Optional: true,
				DefaultFunc: multiEnvDefaultFunc([]string{
					"COCKROACH_DATABASE",
				}, "defaultdb"),
				Description: "The name of the database to connect to (defaults to `defaultdb`).",
			},
			"username": {
				Type:     schema.TypeString,
				Optional: true,
				DefaultFunc: multiEnvDefaultFunc([]string{
					"COCKROACH_USER",
				}, "root"),
				Description: "CockroachDB user name to connect as",
			},
			"password": {
				Type:     schema.TypeString,
				Optional: true,
				DefaultFunc: multiEnvDefaultFunc([]string{
					"COCKROACH_PASSWORD",
				}, nil),
				Description: "Password to be used if the CockroachDB server demands password authentication",
				Sensitive:   true,
			},
			"database_username": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Database username associated to the connected user (for user name maps)",
			},
			"superuser": {
				Type:     schema.TypeBool,
				Optional: true,
				DefaultFunc: multiEnvDefaultFunc([]string{
					"COCKROACH_SUPERUSER",
				}, true),
				Description: "Specify if the user to connect as is a CockroachDB superuser or not.",
			},
			"sslmode": {
				Type:     schema.TypeString,
				Optional: true,
				DefaultFunc: func() (any, error) {
					if v := os.Getenv("COCKROACH_INSECURE"); strings.ToLower(v) == "true" {
						return "disable", nil
					}
					if v := os.Getenv("COCKROACH_SSLMODE"); v != "" {
						return v, nil
					}
					return "require", nil
				},
				Description: "This option determines whether or with what priority a secure SSL TCP/IP connection will be negotiated with the CockroachDB server",
			},
			"ssl_mode": {
				Type:       schema.TypeString,
				Optional:   true,
				Deprecated: "Rename CockroachDB provider `ssl_mode` attribute to `sslmode`",
			},
			"clientcert": {
				Type:        schema.TypeList,
				Optional:    true,
				Description: "SSL client certificate if required by the database.",
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"cert": {
							Type:        schema.TypeString,
							Description: "The SSL client certificate file path. The file must contain PEM encoded data.",
							Required:    true,
						},
						"key": {
							Type:        schema.TypeString,
							Description: "The SSL client certificate private key file path. The file must contain PEM encoded data.",
							Required:    true,
						},
						"sslinline": {
							Type:        schema.TypeBool,
							Description: "Must be set to true if you are inlining the cert/key instead of using a file path.",
							Optional:    true,
						},
					},
				},
				MaxItems: 1,
			},
			"sslrootcert": {
				Type:        schema.TypeString,
				Description: "The SSL server root certificate file path. The file must contain PEM encoded data.",
				Optional:    true,
			},
			"connect_timeout": {
				Type:     schema.TypeInt,
				Optional: true,
				DefaultFunc: multiEnvDefaultFunc([]string{
					"COCKROACH_CONNECT_TIMEOUT",
				}, 180),
				Description:  "Maximum wait for connection, in seconds. Zero or not specified means wait indefinitely.",
				ValidateFunc: validation.IntAtLeast(-1),
			},
			"max_connections": {
				Type:         schema.TypeInt,
				Optional:     true,
				Default:      defaultProviderMaxOpenConnections,
				Description:  "Maximum number of connections to establish to the database. Zero means unlimited.",
				ValidateFunc: validation.IntAtLeast(-1),
			},
			"expected_version": {
				Type:     schema.TypeString,
				Optional: true,
				DefaultFunc: multiEnvDefaultFunc([]string{
					"COCKROACH_EXPECTED_VERSION",
				}, defaultExpectedCockroachSQLVersion),
				Description:  "Specify the expected version of CockroachDB.",
				ValidateFunc: validateExpectedVersion,
			},
			"url": {
				Type:     schema.TypeString,
				Optional: true,
				DefaultFunc: multiEnvDefaultFunc([]string{
					"COCKROACH_URL",
				}, nil),
				Description: "Connection URL for CockroachDB. If set, this overrides other connection parameters.",
				Sensitive:   true,
			},
		},

		ResourcesMap: map[string]*schema.Resource{
			"cockroachsql_database":           resourceCockroachSQLDatabase(),
			"cockroachsql_default_privileges": resourceCockroachSQLDefaultPrivileges(),
			"cockroachsql_grant":              resourceCockroachSQLGrant(),
			"cockroachsql_grant_role":         resourceCockroachSQLGrantRole(),
			"cockroachsql_schema":             resourceCockroachSQLSchema(),
			"cockroachsql_role":               resourceCockroachSQLRole(),
		},

		DataSourcesMap: map[string]*schema.Resource{
			"cockroachsql_schemas":   dataSourceCockroachSQLDatabaseSchemas(),
			"cockroachsql_tables":    dataSourceCockroachSQLDatabaseTables(),
			"cockroachsql_sequences": dataSourceCockroachSQLDatabaseSequences(),
		},

		ConfigureFunc: providerConfigure,
	}
}

func validateExpectedVersion(v any, key string) (warnings []string, errors []error) {
	if _, err := semver.ParseTolerant(v.(string)); err != nil {
		errors = append(errors, fmt.Errorf("invalid version (%q): %w", v.(string), err))
	}
	return
}

func providerConfigure(d *schema.ResourceData) (any, error) {
	var sslMode string
	if sslModeRaw, ok := d.GetOk("sslmode"); ok {
		sslMode = sslModeRaw.(string)
	} else {
		sslModeDeprecated := d.Get("ssl_mode").(string)
		if sslModeDeprecated != "" {
			sslMode = sslModeDeprecated
		}
	}
	versionStr := d.Get("expected_version").(string)
	version, _ := semver.ParseTolerant(versionStr)

	host := d.Get("host").(string)
	port := d.Get("port").(int)
	username := d.Get("username").(string)
	password := d.Get("password").(string)

	config := Config{
		ConnectionURL:     d.Get("url").(string),
		Host:              host,
		Port:              port,
		Username:          username,
		Password:          password,
		DatabaseUsername:  d.Get("database_username").(string),
		Superuser:         d.Get("superuser").(bool),
		SSLMode:           sslMode,
		ApplicationName:   "Terraform provider",
		ConnectTimeoutSec: d.Get("connect_timeout").(int),
		MaxConns:          d.Get("max_connections").(int),
		ExpectedVersion:   version,
		SSLRootCertPath:   d.Get("sslrootcert").(string),
	}

	if value, ok := d.GetOk("clientcert"); ok {
		if spec, ok := value.([]any)[0].(map[string]interface{}); ok {
			config.SSLClientCert = &ClientCertificateConfig{
				CertificatePath: spec["cert"].(string),
				KeyPath:         spec["key"].(string),
				SSLInline:       spec["sslinline"].(bool),
			}
		}
	}

	client := config.NewClient(d.Get("database").(string))
	return client, nil
}
