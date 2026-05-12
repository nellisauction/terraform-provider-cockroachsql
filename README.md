Terraform Provider for CockroachSQL
==================================

This is a specialized Terraform provider for managing [CockroachDB](https://www.cockroachlabs.com/) SQL objects (databases, roles, grants, etc.).

Features:
---------
- **Native CockroachDB Support**: Tailored to align with the CockroachDB SQL dialect and behavior.
- **Strict `CREATE ROLE` Syntax**: Supports CockroachDB-specific role options and ignores unsupported role options.
- **DDL Stability**: DDL operations are executed directly against the database connection to ensure reliability in CockroachDB's distributed environment.
- **Native Default Privileges**: Uses CockroachDB's native `SHOW DEFAULT PRIVILEGES` for accurate state management.

Requirements
------------
- [Terraform](https://www.terraform.io/downloads.html) 1.0+
- [Go](https://golang.org/doc/install) 1.25+ (to build the provider plugin)
- **CockroachDB**: v23.2.0+ (LTS) is the minimum supported version.

Building The Provider
---------------------
1. Clone the repository:
   ```sh
   git clone git@github.com:nellisauction/terraform-provider-cockroachsql.git
   ```
2. Build the binary:
   ```sh
   make build
   ```

Using the Provider
------------------
To use this provider locally, you can use Terraform's `dev_overrides` feature. Create or edit your `~/.terraformrc` file:

```hcl
provider_installation {
  dev_overrides {
    "nellisauction/cockroachsql" = "/path/to/your/compiled/binary/directory"
  }
  direct {}
}
```

In your Terraform configuration:
```hcl
terraform {
  required_providers {
    cockroachsql = {
      source = "nellisauction/cockroachsql"
    }
  }
}

provider "cockroachsql" {
  host     = "localhost"
  port     = 26257
  username = "root"
  database = "defaultdb"
  sslmode  = "disable"
}
```

Pulumi Usage
------------
To use this provider with Pulumi, build the binary and then add it to your Pulumi project:
```bash
pulumi package add terraform-provider /path/to/terraform-provider-cockroachsql
```

Developing the Provider
-----------------------
To run the full suite of Acceptance tests, run `make testacc`.
*Note:* Acceptance tests create real resources and require a running CockroachDB cluster.
