//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestAuthRoleCannotReachAnotherServiceSchema(t *testing.T) {
	statements := map[string]string{
		"create a table":   `CREATE TABLE chat.smuggled (id int)`,
		"read":             `SELECT 1 FROM chat.anything`,
		"create a schema":  `CREATE SCHEMA sneaky`,
		"write to public":  `CREATE TABLE public.parked_here_for_now (id int)`,
		"change own grant": `GRANT USAGE ON SCHEMA chat TO auth_service`,
	}

	for name, statement := range statements {
		t.Run(name, func(t *testing.T) {
			_, err := pool.Exec(t.Context(), statement)
			if err == nil {
				t.Fatalf("the auth role was allowed to %s", name)
			}
			if !isPermissionDenied(err) {
				t.Fatalf("refused, but not by the permission model: %v", err)
			}
		})
	}
}

func TestUnqualifiedNamesResolveInTheAuthSchema(t *testing.T) {
	var schema string
	if err := pool.QueryRow(t.Context(),
		`SELECT schemaname FROM pg_tables WHERE tablename = 'users'`).Scan(&schema); err != nil {
		t.Fatalf("look up the users table: %v", err)
	}

	if schema != "auth" {
		t.Errorf("users resolves to schema %q, want auth", schema)
	}
}

func isPermissionDenied(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "permission denied") ||
		strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "must be owner")
}
