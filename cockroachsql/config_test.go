package cockroachsql

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/blang/semver"
)

func TestConfigConnParams(t *testing.T) {
	var tests = []struct {
		input *Config
		want  []string
	}{
		{&Config{SSLMode: "require", ConnectTimeoutSec: 10}, []string{"connect_timeout=10", "sslmode=require"}},
		{&Config{SSLMode: "disable"}, []string{"connect_timeout=0", "sslmode=disable"}},
		{&Config{ExpectedVersion: semver.MustParse("23.1.0"), ApplicationName: "Terraform provider"}, []string{"connect_timeout=0", "fallback_application_name=Terraform+provider"}},
		{&Config{SSLClientCert: &ClientCertificateConfig{CertificatePath: "/path/to/public-certificate.pem", KeyPath: "/path/to/private-key.pem"}}, []string{"connect_timeout=0", "sslcert=%2Fpath%2Fto%2Fpublic-certificate.pem", "sslkey=%2Fpath%2Fto%2Fprivate-key.pem"}},
		{&Config{SSLRootCertPath: "/path/to/root.pem"}, []string{"connect_timeout=0", "sslrootcert=%2Fpath%2Fto%2Froot.pem"}},
	}

	for _, test := range tests {

		connParams := test.input.connParams()

		sort.Strings(connParams)
		sort.Strings(test.want)

		if !reflect.DeepEqual(connParams, test.want) {
			t.Errorf("Config.connParams(%+v) returned %#v, want %#v", test.input, connParams, test.want)
		}

	}
}

func TestConfigConnStr(t *testing.T) {
	var tests = []struct {
		input        *Config
		wantDbURL    string
		wantDbParams []string
	}{
		{&Config{Host: "localhost", Port: 26257, Username: "cockroach_user", Password: "cockroach_password", SSLMode: "disable"}, "postgresql://cockroach_user:cockroach_password@localhost:26257/defaultdb", []string{"connect_timeout=0", "sslmode=disable"}},
		{&Config{Host: "localhost", Port: 26257, Username: "spaced user", Password: "spaced password", SSLMode: "disable"}, "postgresql://spaced%20user:spaced%20password@localhost:26257/defaultdb", []string{"connect_timeout=0", "sslmode=disable"}},
	}

	for _, test := range tests {

		connStr := test.input.connStr("defaultdb")

		splitConnStr := strings.Split(connStr, "?")

		if splitConnStr[0] != test.wantDbURL {
			t.Errorf("Config.connStr(%+v) returned %#v, want %#v", test.input, splitConnStr[0], test.wantDbURL)
		}

		connParams := strings.Split(splitConnStr[1], "&")

		sort.Strings(connParams)
		sort.Strings(test.wantDbParams)

		if !reflect.DeepEqual(connParams, test.wantDbParams) {
			t.Errorf("Config.connStr(%+v) returned %#v, want %#v", test.input, connParams, test.wantDbParams)
		}

	}
}
