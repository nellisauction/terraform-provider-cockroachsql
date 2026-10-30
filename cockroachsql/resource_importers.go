package cockroachsql

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

const importIDSeparator = "|"

func splitImportID(id string, minParts, maxParts int, format string) ([]string, error) {
	parts := strings.Split(id, importIDSeparator)
	if len(parts) < minParts || len(parts) > maxParts {
		return nil, fmt.Errorf("unexpected import ID %q, expected %s", id, format)
	}
	for len(parts) < maxParts {
		parts = append(parts, "")
	}
	return parts, nil
}

// splitImportList splits a comma separated list, ignoring commas inside
// parentheses so function signatures such as f(int, text) stay intact.
func splitImportList(v string) []any {
	if v == "" {
		return []any{}
	}
	var items []any
	depth, start := 0, 0
	for i, r := range v {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				items = append(items, strings.TrimSpace(v[start:i]))
				start = i + 1
			}
		}
	}
	return append(items, strings.TrimSpace(v[start:]))
}

func parseImportBool(v, attr string) (bool, error) {
	if v == "" {
		return false, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("invalid %s %q in import ID: %w", attr, v, err)
	}
	return b, nil
}

const grantRoleImportFormat = "role|grant_role[|with_admin_option]"

func importCockroachSQLGrantRole(_ context.Context, d *schema.ResourceData, _ any) ([]*schema.ResourceData, error) {
	parts, err := splitImportID(d.Id(), 2, 3, grantRoleImportFormat)
	if err != nil {
		return nil, err
	}
	withAdmin, err := parseImportBool(parts[2], "with_admin_option")
	if err != nil {
		return nil, err
	}
	d.Set("role", parts[0])
	d.Set("grant_role", parts[1])
	d.Set("with_admin_option", withAdmin)
	d.SetId(generateGrantRoleID(d))
	return []*schema.ResourceData{d}, nil
}

const grantImportFormat = "role|database|object_type|schema|objects|columns (objects and columns comma separated, schema/objects/columns may be empty)"

func importCockroachSQLGrant(_ context.Context, d *schema.ResourceData, _ any) ([]*schema.ResourceData, error) {
	parts, err := splitImportID(d.Id(), 3, 6, grantImportFormat)
	if err != nil {
		return nil, err
	}
	if !sliceContainsStr(allowedObjectTypes, parts[2]) {
		return nil, fmt.Errorf("unknown object_type %q in import ID, expected one of %s", parts[2], strings.Join(allowedObjectTypes, ", "))
	}
	d.Set("role", parts[0])
	d.Set("database", parts[1])
	d.Set("object_type", parts[2])
	d.Set("schema", parts[3])
	d.Set("objects", schema.NewSet(schema.HashString, splitImportList(parts[4])))
	d.Set("columns", schema.NewSet(schema.HashString, splitImportList(parts[5])))
	d.SetId(generateGrantID(d))
	return []*schema.ResourceData{d}, nil
}

const defaultPrivilegesImportFormat = "role|database|object_type|owner|schema (owner * means for_all_roles, schema may be empty)"

func importCockroachSQLDefaultPrivileges(_ context.Context, d *schema.ResourceData, _ any) ([]*schema.ResourceData, error) {
	parts, err := splitImportID(d.Id(), 4, 5, defaultPrivilegesImportFormat)
	if err != nil {
		return nil, err
	}
	d.Set("role", parts[0])
	d.Set("database", parts[1])
	d.Set("object_type", parts[2])
	d.Set("for_all_roles", parts[3] == "*")
	if parts[3] != "*" {
		d.Set("owner", parts[3])
	}
	d.Set("schema", parts[4])
	d.SetId(generateDefaultPrivilegesID(d))
	return []*schema.ResourceData{d}, nil
}
