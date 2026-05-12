package main

import (
	"log"

	"github.com/hashicorp/terraform-plugin-sdk/v2/plugin"
	"github.com/nellisauction/terraform-provider-cockroachsql/cockroachsql"
)

// These variables are set by the build process
var (
	version = "dev"
	commit  = "none"
)

func main() {
	log.Printf("[INFO] CockroachSQL Provider version: %s, commit: %s", version, commit)
	plugin.Serve(&plugin.ServeOpts{
		ProviderFunc: cockroachsql.Provider})
}
