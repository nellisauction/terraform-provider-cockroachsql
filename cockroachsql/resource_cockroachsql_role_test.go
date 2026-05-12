package cockroachsql

import (
	"database/sql"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/lib/pq"
)

func TestAccCockroachSQLRole_Basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePrivileges)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckCockroachSQLRoleDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccCockroachSQLRoleConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckCockroachSQLRoleExists("myrole2", nil, nil),
					resource.TestCheckResourceAttr("cockroachsql_role.myrole2", "name", "myrole2"),
					resource.TestCheckResourceAttr("cockroachsql_role.myrole2", "login", "true"),
					resource.TestCheckResourceAttr("cockroachsql_role.myrole2", "roles.#", "0"),

					testAccCheckCockroachSQLRoleExists("role_default", nil, nil),
					resource.TestCheckResourceAttr("cockroachsql_role.role_with_defaults", "name", "role_default"),
					resource.TestCheckResourceAttr("cockroachsql_role.role_with_defaults", "create_role", "false"),
					resource.TestCheckResourceAttr("cockroachsql_role.role_with_defaults", "valid_until", ""),
					resource.TestCheckResourceAttr("cockroachsql_role.role_with_defaults", "skip_drop_role", "false"),
					resource.TestCheckResourceAttr("cockroachsql_role.role_with_defaults", "skip_reassign_owned", "false"),
					resource.TestCheckResourceAttr("cockroachsql_role.role_with_defaults", "statement_timeout", "0"),
					resource.TestCheckResourceAttr("cockroachsql_role.role_with_defaults", "idle_in_transaction_session_timeout", "0"),
				),
			},
		},
	})
}

func TestAccCockroachSQLRole_Update(t *testing.T) {
	var configCreate = `
resource "cockroachsql_role" "update_role" {
  name = "update_role"
  login = true
  valid_until = "2099-05-04 12:00:00+00"
}
`

	var configUpdate = `
resource "cockroachsql_role" "group_role" {
  name = "group_role"
}

resource "cockroachsql_role" "update_role" {
  name = "update_role2"
  login = true
  roles = ["${cockroachsql_role.group_role.name}"]
  search_path = ["mysearchpath"]
}
`

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testCheckCompatibleVersion(t, featurePrivileges)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckCockroachSQLRoleDestroy,
		Steps: []resource.TestStep{
			{
				Config: configCreate,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckCockroachSQLRoleExists("update_role", nil, nil),
					resource.TestCheckResourceAttr("cockroachsql_role.update_role", "name", "update_role"),
					resource.TestCheckResourceAttr("cockroachsql_role.update_role", "login", "true"),
					resource.TestCheckResourceAttr("cockroachsql_role.update_role", "valid_until", "2099-05-04 12:00:00+00"),
					resource.TestCheckResourceAttr("cockroachsql_role.update_role", "roles.#", "0"),
					resource.TestCheckResourceAttr("cockroachsql_role.update_role", "search_path.#", "0"),
					resource.TestCheckResourceAttr("cockroachsql_role.update_role", "statement_timeout", "0"),
					resource.TestCheckResourceAttr("cockroachsql_role.update_role", "idle_in_transaction_session_timeout", "0"),
				),
			},
			{
				Config: configUpdate,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckCockroachSQLRoleExists("update_role2", []string{"group_role"}, []string{"mysearchpath"}),
					resource.TestCheckResourceAttr(
						"cockroachsql_role.update_role", "name", "update_role2",
					),
					resource.TestCheckResourceAttr("cockroachsql_role.update_role", "login", "true"),
					resource.TestCheckResourceAttr("cockroachsql_role.update_role", "valid_until", ""),
					resource.TestCheckResourceAttr("cockroachsql_role.update_role", "roles.#", "1"),
					resource.TestCheckResourceAttr("cockroachsql_role.update_role", "roles.0", "group_role"),
					resource.TestCheckResourceAttr("cockroachsql_role.update_role", "search_path.#", "1"),
					resource.TestCheckResourceAttr("cockroachsql_role.update_role", "search_path.0", "mysearchpath"),
				),
			},
			{
				Config: configCreate,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckCockroachSQLRoleExists("update_role", nil, nil),
					resource.TestCheckResourceAttr("cockroachsql_role.update_role", "name", "update_role"),
					resource.TestCheckResourceAttr("cockroachsql_role.update_role", "login", "true"),
					resource.TestCheckResourceAttr("cockroachsql_role.update_role", "roles.#", "0"),
					resource.TestCheckResourceAttr("cockroachsql_role.update_role", "search_path.#", "0"),
					resource.TestCheckResourceAttr("cockroachsql_role.update_role", "statement_timeout", "0"),
					resource.TestCheckResourceAttr("cockroachsql_role.update_role", "idle_in_transaction_session_timeout", "0"),
				),
			},
		},
	})
}

func testAccCheckCockroachSQLRoleDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*Client)

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "cockroachsql_role" {
			continue
		}

		db, err := client.Connect()
		if err != nil {
			return err
		}

		exists, err := checkRoleExists(db, rs.Primary.ID)

		if err != nil {
			return fmt.Errorf("error checking role %s", err)
		}

		if exists {
			return fmt.Errorf("Role still exists after destroy")
		}
	}

	return nil
}

func testAccCheckCockroachSQLRoleExists(roleName string, expectedRoles []string, expectedSearchPath []string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		client := testAccProvider.Meta().(*Client)

		db, err := client.Connect()
		if err != nil {
			return err
		}

		exists, err := checkRoleExists(db, roleName)

		if err != nil {
			return fmt.Errorf("error checking role %s", err)
		}

		if !exists {
			return fmt.Errorf("Role not found")
		}

		if expectedRoles != nil {
			if err := checkGrantedRoles(client, roleName, expectedRoles); err != nil {
				return err
			}
		}

		if expectedSearchPath != nil {
			if err := checkSearchPath(client, roleName, expectedSearchPath); err != nil {
				return err
			}
		}

		return nil
	}
}

func checkRoleExists(db QueryAble, roleName string) (bool, error) {
	var _rez bool
	err := db.QueryRow("SELECT TRUE FROM pg_catalog.pg_roles WHERE rolname=$1", roleName).Scan(&_rez)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("error reading info about role: %s", err)
	}

	return true, nil
}

func checkGrantedRoles(client *Client, roleName string, expectedRoles []string) error {
	db, err := client.Connect()
	if err != nil {
		return err
	}

	rows, err := db.Query(
		"SELECT pg_get_userbyid(roleid) as rolname from pg_auth_members WHERE pg_get_userbyid(member) = $1 ORDER BY rolname",
		roleName,
	)
	if err != nil {
		return fmt.Errorf("error reading granted roles: %v", err)
	}
	defer func() { _ = rows.Close() }()

	grantedRoles := []string{}
	for rows.Next() {
		var grantedRole string
		if err := rows.Scan(&grantedRole); err != nil {
			return fmt.Errorf("error scanning granted role: %v", err)
		}
		grantedRoles = append(grantedRoles, grantedRole)
	}

	sort.Strings(expectedRoles)
	sort.Strings(grantedRoles)

	if !reflect.DeepEqual(expectedRoles, grantedRoles) {
		return fmt.Errorf("expected roles %v; got %v", expectedRoles, grantedRoles)
	}

	return nil
}

func checkSearchPath(client *Client, roleName string, expectedSearchPath []string) error {
	db, err := client.Connect()
	if err != nil {
		return err
	}

	var roleConfig pq.StringArray
	err = db.QueryRow("SELECT rolconfig FROM pg_catalog.pg_roles WHERE rolname=$1", roleName).Scan(&roleConfig)
	if err != nil {
		return fmt.Errorf("error reading role config: %v", err)
	}

	searchPath := readSearchPath(roleConfig)

	if !reflect.DeepEqual(expectedSearchPath, searchPath) {
		return fmt.Errorf("expected search_path %v; got %v", expectedSearchPath, searchPath)
	}

	return nil
}

var testAccCockroachSQLRoleConfig = `
resource "cockroachsql_role" "myrole2" {
  name  = "myrole2"
  login = true
}

resource "cockroachsql_role" "role_simple" {
  name = "role_simple"
}

resource "cockroachsql_role" "role_with_defaults" {
  name = "role_default"
}

resource "cockroachsql_role" "sub_role" {
  name  = "sub_role"
  roles = [
		"${cockroachsql_role.myrole2.name}",
		"${cockroachsql_role.role_simple.name}",
  ]
}

resource "cockroachsql_role" "role_with_search_path" {
  name        = "role_with_search_path"
  search_path = ["bar", "foo-with-hyphen"]
}
`
