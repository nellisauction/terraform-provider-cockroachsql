package cockroachsql

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func TestRedactDSN(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"postgresql://u:s3cret@host:26257/db?sslmode=require", "postgresql://u:xxxxx@host:26257/db"},
		{"postgresql://u@host:26257/db?sslinline=true&sslkey=-----BEGIN", "postgresql://u@host:26257/db"},
		{"host=h user=u password=s3cret", "<redacted connection string>"},
	}
	for _, tt := range tests {
		if got := redactDSN(tt.in); got != tt.want {
			t.Errorf("redactDSN(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestConfigRedactSecrets(t *testing.T) {
	c := &Config{
		Password:      "p@ss word",
		ConnectionURL: "postgresql://u:url-secret@host/db",
		SSLClientCert: &ClientCertificateConfig{KeyPath: "-----BEGIN KEY-----", SSLInline: true},
	}
	in := "failed: p@ss word p%40ss%20word p%40ss+word url-secret -----BEGIN KEY-----"
	got := c.redactSecrets(in)
	for _, secret := range []string{"p@ss", "url-secret", "BEGIN KEY"} {
		if strings.Contains(got, secret) {
			t.Errorf("redactSecrets left %q in %q", secret, got)
		}
	}

	if got := (&Config{}).redactSecrets("unchanged"); got != "unchanged" {
		t.Errorf("empty password must not alter the message, got %q", got)
	}
}

func TestConfigRedactSecretsMalformedURL(t *testing.T) {
	for _, password := range []string{"p@ss:word", "p/ss", "p?ss", "p#ss", "a/b?c#d@e"} {
		c := &Config{ConnectionURL: "postgresql://u:" + password + "@host:abc/db"}
		got := c.redactSecrets("parse " + c.ConnectionURL + ": invalid port")
		if strings.Contains(got, password) {
			t.Errorf("redactSecrets left password %q in %q", password, got)
		}
	}
}

func TestConnectRejectsUnparseableURLWithoutEchoingIt(t *testing.T) {
	const password = "p/ss?w#rd"
	c := &Config{ConnectionURL: "postgresql://u:" + password + "@host:abc/db"}
	_, err := c.NewClient("db").Connect(context.Background())
	if err == nil {
		t.Fatal("expected an error for an unparseable URL")
	}
	for _, leaked := range []string{"p/ss", "w#rd", "host:abc"} {
		if strings.Contains(err.Error(), leaked) {
			t.Fatalf("error echoes the URL: %v", err)
		}
	}
}

func TestClientCertSensitive(t *testing.T) {
	elem := Provider().Schema["clientcert"].Elem.(*schema.Resource)
	for _, k := range []string{"cert", "key"} {
		if !elem.Schema[k].Sensitive {
			t.Errorf("clientcert.%s must be Sensitive", k)
		}
	}
}

func TestAccCockroachSQLRole_CreateErrorRedactsPassword(t *testing.T) {
	skipIfNotAcc(t)
	testAccPreCheck(t)

	const roleName = "tf_tests_redact_role"
	const password = `s3cr3t'pa\55`

	config := getTestConfig(t)
	dbExecute(t, config.connStr(getTestDatabaseName()), fmt.Sprintf("CREATE ROLE IF NOT EXISTS %s", roleName))
	defer dbExecute(t, config.connStr(getTestDatabaseName()), fmt.Sprintf("DROP ROLE IF EXISTS %s", roleName))

	db, err := testAccProvider.Meta().(*Client).Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	d := schema.TestResourceDataRaw(t, resourceCockroachSQLRole().Schema, map[string]any{
		"name":     roleName,
		"password": password,
	})
	err = resourceCockroachSQLRoleCreate(context.Background(), db, d)
	if err == nil {
		t.Fatal("expected CREATE ROLE to fail for an existing role")
	}
	for _, leaked := range []string{password, pqQuoteLiteral(password), "s3cr3t"} {
		if strings.Contains(err.Error(), leaked) {
			t.Fatalf("error leaks the password: %v", err)
		}
	}
}
