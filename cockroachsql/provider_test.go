package cockroachsql

import (
	"context"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

var testAccProviders map[string]*schema.Provider
var testAccProvider *schema.Provider

func init() {
	testAccProvider = Provider()
	testAccProviders = map[string]*schema.Provider{
		"cockroachsql": testAccProvider,
	}
}

func TestProvider(t *testing.T) {
	if err := Provider().InternalValidate(); err != nil {
		t.Fatalf("err: %s", err)
	}
}

func TestProvider_impl(t *testing.T) {
	var _ = Provider()
}

func testAccPreCheck(t *testing.T) {
	if os.Getenv("COCKROACH_HOST") == "" {
		t.Fatal("COCKROACH_HOST must be set for acceptance tests")
	}
	if os.Getenv("COCKROACH_USER") == "" {
		t.Fatal("COCKROACH_USER must be set for acceptance tests")
	}

	config := map[string]any{}
	if v := os.Getenv("COCKROACH_DATABASE"); v != "" {
		config["database"] = v
	}

	diags := testAccProvider.Configure(context.Background(), terraform.NewResourceConfigRaw(config))
	if diags.HasError() {
		t.Fatal(diags)
	}
}
